package main

import (
	"crypto/hmac"
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
		c.Header("Cache-Control", "no-store")
		if c.Request.Method == http.MethodPost {
			if c.GetHeader("authentigate-token") == "" {
				c.String(http.StatusForbidden, "Shop mutations require a trusted authenticated session token")
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
			if err := c.Request.ParseForm(); err != nil {
				c.String(http.StatusBadRequest, "Could not read shop form: %v", err)
				return
			}
			if !hmac.Equal([]byte(c.PostForm("csrf")), []byte(formToken(c))) {
				c.String(http.StatusForbidden, "Shop form authorization is missing or invalid; reload the shop page before submitting")
				return
			}
		}
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
	if rest(c, shopService, "basket", http.MethodGet, &basket) == nil {
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
	resp := rest(c, shopService, "listItems", http.MethodGet, &pricemap)
	if resp == nil {
		return
	}
	body := ""
	prices := SortWat(pricemap)
	for _, v := range prices {
		itemField := `<input type="hidden" name="item" value="` + html.EscapeString(v.Name) + `">`
		button := fmt.Sprintf("<button type=\"submit\">Add %v</button> ... $%v</form>\n", html.EscapeString(v.Name), v.Price)
		body = body + `<form method="post" action="addItem">` + formField(c) + itemField + button
	}

	var basketmap map[string]int
	if rest(c, shopService, "basket", http.MethodGet, &basketmap) == nil {
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
	if rest(c, shopService, "createCoupon?duration=1000&discount=0.7&target=orange", http.MethodPost, &coupon) == nil {
		return
	}

	body := "<h2>Coupon</h2>"
	body = body + "<p>Use code " + html.EscapeString(coupon.Name) + " for a " + fmt.Sprintf("%v", coupon.Discount) + " on " + html.EscapeString(coupon.Target) + "</p>"
	c.Writer.Write([]byte(wrapPage("Shop", body)))
}

func rest(c *gin.Context, base, endpoint, method string, data interface{}) *resty.Response {
	client := resty.New()
	client.SetTimeout(10 * time.Second)
	r := client.R()
	for _, header := range []string{"authentigate-id", "authentigate-token", "authentigate-base-url", "authentigate-top-url"} {
		r.SetHeader(header, c.Request.Header.Get(header))
	}
	if method == http.MethodPost {
		r.SetHeader("X-Requested-With", "authentigate-shop")
	}

	resp, err := r.Execute(method, base + endpoint)
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
	item := c.PostForm("item")
	if rest(c, shopService, "addItem?item="+url.QueryEscape(item)+"&amount=1", http.MethodPost, nil) == nil {
		return
	}
	shop(c, id, token)
}

func checkout(c *gin.Context, id string, token string) {

	var basket map[string]int
	if rest(c, shopService, "basket", http.MethodGet, &basket) == nil {
		return
	}

	body := "<h2>Basket</h2>"

	for k, v := range basket {
		body = body + fmt.Sprintf("<p>%v ... %v</p>\n", html.EscapeString(k), v)
	}
	total := 0.0
	order := map[string]float64{}
	if rest(c, shopService, "checkout", http.MethodGet, &order) == nil {
		return
	}

	body = body + "<h2>Demo order</h2><p>This demonstration records an order without collecting payment.</p>"

	for k, v := range order {
		body = body + fmt.Sprintf("<p>%v ... $%v</p>\n", html.EscapeString(k), v)
		total = total + v
	}
	body = body + "<p>Total: " + fmt.Sprintf("%v", total) + "</p>"

	body = body + `<h2>Coupon</h2>
	<form method="post" action="coupon">` + formField(c) + `<button type="submit">Create a demo discount coupon</button></form>
	<form method="post" action="purchase">` + formField(c) + `
  <label for="coupon">Coupon code (optional)</label>
  <input type="text" id="coupon" name="coupon"><br><br>
  <input type="submit" value="Record demo order">
</form>
`

	c.Writer.Write([]byte(wrapPage("Checkout", body)))

}

func purchase(c *gin.Context, id string, token string) {
	coupon := c.PostForm("coupon")
	fmt.Printf("Coupon id: %v\n", coupon)

	total := 0.0
	order := map[string]float64{}
	resp := rest(c, shopService, "purchase?coupon="+url.QueryEscape(coupon), http.MethodPost, &order)
	if resp == nil {
		return
	}
	fmt.Printf("%+v\n", resp)

	body := "<h2>Demo order</h2>"

	for k, v := range order {
		body = body + fmt.Sprintf("<p>%v ... $%v</p>\n", html.EscapeString(k), v)
		total = total + v
	}
	body = body + "<p>Total: " + fmt.Sprintf("%v", total) + "</p>"
	body = body + "<h2>Demo order recorded</h2><p>No payment was collected.</p>"
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
	router.POST(prefix+"purchase", makeAuthed(purchase))
	router.POST(prefix+"addItem", makeAuthed(addItem))
	router.POST(prefix+"coupon", makeAuthed(coupon))
}
