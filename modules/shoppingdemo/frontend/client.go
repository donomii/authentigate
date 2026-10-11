package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/resty.v1"
)

var shopService string = "http://localhost:8098/shoppr/api/v1/"

type Coupon struct {
	Name     string
	Expiry   time.Time
	Target   string
	Discount float64
}

func dumpResponse(resp *resty.Response) {
	fmt.Printf("\nResponse Status Code: %v", resp.StatusCode())
	fmt.Printf("\nResponse Status: %v", resp.Status())
	fmt.Printf("\nResponse Body: %v", resp)
	fmt.Printf("\nResponse Time: %v", resp.Time())
	fmt.Printf("\nResponse Received At: %v", resp.ReceivedAt())

	var basket map[string]int
	json.Unmarshal(resp.Body(), &basket)
	fmt.Printf("Basket: %v\n", basket)
}

type User struct {
	Name        string
	Basket      map[string]int
	UsedCoupons map[string]int
	Orders      []map[string]float64
}

type Shop struct {
	Prices  map[string]float64
	Coupons map[string]Coupon
}

func makeAuthed(handlerFunc func(*gin.Context, string, string)) func(c *gin.Context) {
	return func(c *gin.Context) {
		id := c.Request.Header.Get("authentigate-id")
		if id == "" {
			id = "demoUser"
		}
		baseUrl := c.Request.Header.Get("authentigate-base-url")
		log.Printf("Got real user id: '%v'", id)
		handlerFunc(c, id, baseUrl)
	}
}
func wrapPage(title, body string) string {
	return "<HTML><HEAD><TITLE>" + title + "</TITLE></HEAD><BODY>" + body + "</BODY></HTML>"
}

func basket(c *gin.Context, id string, token string) {
	var basket map[string]int
	if rest(c, shopService, "basket", &basket) == nil {
		return
	}

	body := ""
	for k, v := range basket {
		body = body + fmt.Sprintf("<p>%v ... %v</p>\n", html.EscapeString(k), v)
	}

	c.Writer.Write([]byte(wrapPage("Basket", body)))
}

type price struct {
	Price float64
	Name  string
}

type BasketItem struct {
	Name  string
	Count int
}

type wat2 []BasketItem

type wat []price

func SortWat(p map[string]float64) wat {
	out := make([]price, len(p))
	names := []string{}
	for i, _ := range p {
		names = append(names, i)
	}
	sort.Strings(names)
	for i, v := range names {
		out[i] = price{p[v], v}
	}
	return out
}

func SortWat2(w map[string]int) wat2 {
	out := make([]BasketItem, len(w))
	names := []string{}
	for i, _ := range w {
		names = append(names, i)
	}
	sort.Strings(names)
	for i, v := range names {
		out[i] = BasketItem{v, w[v]}
	}
	return out
}
func map2wat(m map[string]float64) wat {
	var list wat
	for k, v := range m {
		list = append(list, price{v, k})
	}
	return list
}

func map2wat2(m map[string]int) wat2 {
	var list wat2
	for k, v := range m {
		list = append(list, BasketItem{k, v})
	}
	return list
}

func shop(c *gin.Context, id string, token string) {
	var pricemap map[string]float64
	resp := rest(c, shopService, "listItems", &pricemap)
	if resp == nil {
		return
	}
	body := ""
	prices := SortWat(pricemap)
	for _, v := range prices {
		body = body + fmt.Sprintf("<p><a href=\"addItem?item=%v\">%v</a> ... $%v</p>\n", url.QueryEscape(v.Name), html.EscapeString(v.Name), v.Price)
	}

	var basketmap map[string]int
	if rest(c, shopService, "basket", &basketmap) == nil {
		return
	}

	body = body + "<h2>Basket</h2>"

	basket := SortWat2(basketmap)
	for _, v := range basket {
		body = body + fmt.Sprintf("<p>%v ... %v</p>\n", html.EscapeString(v.Name), v.Count)
	}

	body = body + "<h2><a href=checkout>Go to checkout</a></h2>"
	c.Writer.Write([]byte(wrapPage("Shop", body)))
}

func coupon(c *gin.Context, id string, token string) {
	var coupon Coupon
	if rest(c, shopService, "createCoupon?duration=1000&discount=0.7&target=orange", &coupon) == nil {
		return
	}

	body := "<h2>Coupon</h2>"
	body = body + "<p>Use code " + html.EscapeString(coupon.Name) + " for a " + fmt.Sprintf("%v", coupon.Discount) + " on " + html.EscapeString(coupon.Target) + "</p>"
	c.Writer.Write([]byte(wrapPage("Shop", body)))
}

func rest(c *gin.Context, base, endpoint string, data interface{}) *resty.Response {
	client := resty.New()
	client.SetTimeout(10 * time.Second)
	r := client.R()
	for _, header := range []string{"authentigate-id", "authentigate-token", "authentigate-base-url", "authentigate-top-url"} {
		r.SetHeader(header, c.Request.Header.Get(header))
	}

	resp, err := r.Get(base + endpoint)
	if err != nil {
		c.String(http.StatusBadGateway, "Shop service request failed: %v", err)
		return nil
	} else if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		c.Data(resp.StatusCode(), "application/json", resp.Body())
		return nil
	}
	if data != nil {
		if err := json.Unmarshal(resp.Body(), data); err != nil {
			c.String(http.StatusBadGateway, "Shop service returned invalid JSON for %s: %v", endpoint, err)
			return nil
		}
	}
	return resp
}

func addItem(c *gin.Context, id string, token string) {
	item, _ := c.GetQuery("item")
	if rest(c, shopService, "addItem?item="+url.QueryEscape(item)+"&amount=1", nil) == nil {
		return
	}
	shop(c, id, token)
}

func checkout(c *gin.Context, id string, token string) {

	var basket map[string]int
	if rest(c, shopService, "basket", &basket) == nil {
		return
	}

	body := "<h2>Basket</h2>"

	for k, v := range basket {
		body = body + fmt.Sprintf("<p>%v ... %v</p>\n", html.EscapeString(k), v)
	}
	total := 0.0
	order := map[string]float64{}
	if rest(c, shopService, "checkout", &order) == nil {
		return
	}

	body = body + "<h2>Order</h2>"

	for k, v := range order {
		body = body + fmt.Sprintf("<p>%v ... $%v</p>\n", html.EscapeString(k), v)
		total = total + v
	}
	body = body + "<p>Total: " + fmt.Sprintf("%v", total) + "</p>"

	body = body + `<h2>Coupon</h2>
	<p>Get a discount <a href=coupon>coupon</a></p>
	<form action="purchase">
  <label for="fname">Coupon</label>
  <input type="text" id="coupon" name="coupon"><br><br>

<label for="fname">Credit Card</label>
  <input type="text" id="ccnumber" name="ccnumber"><br><br>

<label for="fname">Name</label>
  <input type="text" id="ccname" name="ccname"><br><br>

<label for="fname">Expiry</label>
  <input type="text" id="ccexpiry" name="ccexpiry"><br><br>

  <input type="submit" value="Submit">
</form>
`

	c.Writer.Write([]byte(wrapPage("Checkout", body)))

}

func purchase(c *gin.Context, id string, token string) {
	coupon, _ := c.GetQuery("coupon")
	fmt.Printf("Coupon id: %v\n", coupon)

	total := 0.0
	order := map[string]float64{}
	resp := rest(c, shopService, "purchase?coupon="+url.QueryEscape(coupon), &order)
	if resp == nil {
		return
	}
	fmt.Printf("%+v\n", resp)

	body := "<h2>Order</h2>"

	for k, v := range order {
		body = body + fmt.Sprintf("<p>%v ... $%v</p>\n", html.EscapeString(k), v)
		total = total + v
	}
	body = body + "<p>Total: " + fmt.Sprintf("%v", total) + "</p>"
	body = body + "<h2>Purchase confirmed</h2>"
	body = body + "<a href=shop>Back to start</a>"
	c.Writer.Write([]byte(wrapPage("Purchase", body)))
}

func main() {
	router := gin.Default()
	serveDemo(router, "/fe/api/v1/")
	router.Run("127.0.0.1:8099")
}

///fe/api/v1/shop
func serveDemo(router *gin.Engine, prefix string) {
	router.GET(prefix+"basket", makeAuthed(basket))
	router.GET(prefix+"shop", makeAuthed(shop))
	router.GET(prefix+"checkout", makeAuthed(checkout))
	router.GET(prefix+"purchase", makeAuthed(purchase))
	router.GET(prefix+"addItem", makeAuthed(addItem))
	router.GET(prefix+"coupon", makeAuthed(coupon))
}
