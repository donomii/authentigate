package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty"
)

var shopService string = "http://localhost:98/shoppr/"

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
	rest(c, shopService, "basket", &basket)

	body := ""
	for k, v := range basket {
		body = body + fmt.Sprintf("<p>%v ... %v</p>\n", k, v)
	}

	c.Writer.Write([]byte(wrapPage("Basket", body)))
}

func shop(c *gin.Context, id string, token string) {
	var prices map[string]float64
	resp := rest(c, shopService, "listItems", &prices)

	body := "<a href=checkout>Go to checkout</a>"
	for k, v := range prices {
		body = body + fmt.Sprintf("<p><a href=\"addItem?item="+k+"\">%v</a> ... $%v</p>\n", k, v)
	}

	resp = rest(c, shopService, "basket", nil)

	var basket map[string]int
	json.Unmarshal(resp.Body(), &basket)

	body = body + "<h2>Basket</h2>"
	for k, v := range basket {
		body = body + fmt.Sprintf("<p>%v ... %v</p>\n", k, v)
	}

	c.Writer.Write([]byte(wrapPage("Shop", body)))
}

func coupon(c *gin.Context, id string, token string) {
	var coupon Coupon
	rest(c, shopService, "createCoupon?duration=1000&discount=0.7&target=orange", &coupon)

	body := "<h2>Coupon</h2>"
	body = body + "<p>Use code " + coupon.Name + " for a " + fmt.Sprintf("%v", coupon.Discount) + " on " + coupon.Target + "</p>"
	c.Writer.Write([]byte(wrapPage("Shop", body)))
}

func rest(c *gin.Context, base, endpoint string, data interface{}) *resty.Response {
	client := resty.New()
	r := client.R()
	for _, header := range []string{"authentigate-id", "authentigate-token", "authentigate-base-url", "authentigate-top-url"} {
		r.SetHeader(header, c.Request.Header.Get(header))
	}

	resp, _ := r.Get(base + endpoint)
	if data != nil {
		json.Unmarshal(resp.Body(), data)
	}
	return resp
}

func addItem(c *gin.Context, id string, token string) {
	item, _ := c.GetQuery("item")
	item = strings.Replace(item, " ", "%20", -1)
	rest(c, shopService, "addItem?item="+item+"&amount=1", nil)
	shop(c, id, token)
}

func checkout(c *gin.Context, id string, token string) {

	var basket map[string]int
	rest(c, shopService, "basket", &basket)

	body := "<h2>Basket</h2>"

	for k, v := range basket {
		body = body + fmt.Sprintf("<p>%v ... %v</p>\n", k, v)
	}
	total := 0.0
	order := map[string]float64{}
	rest(c, shopService, "checkout", &order)

	body = body + "<h2>Order</h2>"

	for k, v := range order {
		body = body + fmt.Sprintf("<p>%v ... $%v</p>\n", k, v)
		total = total + v
	}
	body = body + "<p>Total: " + fmt.Sprintf("%v", total) + "</p>"

	body = body + `<h2>Coupon</h2>
	<p>Get a discount <a href=coupon>coupon</a></p>
	<form action="purchase">
  <label for="fname">Coupon</label>
  <input type="text" id="coupon" name="coupon"><br><br>
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
	resp := rest(c, shopService, "purchase?coupon="+coupon, &order)
	fmt.Printf("%+v\n", resp)

	body := "<h2>Order</h2>"

	for k, v := range order {
		body = body + fmt.Sprintf("<p>%v ... $%v</p>\n", k, v)
		total = total + v
	}
	body = body + "<p>Total: " + fmt.Sprintf("%v", total) + "</p>"
	body = body + "<h2>Purchase confirmed</h2>"
	c.Writer.Write([]byte(wrapPage("Purchase", body)))
}

func main() {
	router := gin.Default()
	serveDemo(router, "/fe/")
	router.Run("127.0.0.1:99")
}

func serveDemo(router *gin.Engine, prefix string) {
	router.GET(prefix+"basket", makeAuthed(basket))
	router.GET(prefix+"shop", makeAuthed(shop))
	router.GET(prefix+"checkout", makeAuthed(checkout))
	router.GET(prefix+"purchase", makeAuthed(purchase))
	router.GET(prefix+"addItem", makeAuthed(addItem))
	router.GET(prefix+"coupon", makeAuthed(coupon))
}
