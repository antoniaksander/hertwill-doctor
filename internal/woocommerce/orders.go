package woocommerce

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Order is the read-only subset of a WooCommerce order used for sales reports.
type Order struct {
	ID            int         `json:"id"`
	Status        string      `json:"status"`
	DateCreated   string      `json:"date_created"`
	Currency      string      `json:"currency"`
	Total         string      `json:"total"`
	TotalTax      string      `json:"total_tax"`
	ShippingTotal string      `json:"shipping_total"`
	DiscountTotal string      `json:"discount_total"`
	Country       string      `json:"country"`
	Coupons       []string    `json:"coupons"`
	Items         []OrderItem `json:"items"`
}

// OrderItem is one line of an order. Total is the line total excluding tax.
type OrderItem struct {
	ProductID   int    `json:"product_id"`
	VariationID int    `json:"variation_id"`
	Name        string `json:"name"`
	SKU         string `json:"sku"`
	Quantity    int    `json:"quantity"`
	Total       string `json:"total"`
}

type wooOrder struct {
	ID            int    `json:"id"`
	Status        string `json:"status"`
	DateCreated   string `json:"date_created"`
	Currency      string `json:"currency"`
	Total         string `json:"total"`
	TotalTax      string `json:"total_tax"`
	ShippingTotal string `json:"shipping_total"`
	DiscountTotal string `json:"discount_total"`
	Billing       struct {
		Country string `json:"country"`
	} `json:"billing"`
	Shipping struct {
		Country string `json:"country"`
	} `json:"shipping"`
	CouponLines []struct {
		Code string `json:"code"`
	} `json:"coupon_lines"`
	LineItems []struct {
		ProductID   int    `json:"product_id"`
		VariationID int    `json:"variation_id"`
		Name        string `json:"name"`
		SKU         string `json:"sku"`
		Quantity    int    `json:"quantity"`
		Total       string `json:"total"`
	} `json:"line_items"`
}

// ListOrders returns orders created on or after `after` (YYYY-MM-DD, may be
// empty) with one of the given statuses, newest first, following pages.
// Read-only: GET /orders.
func (c Client) ListOrders(ctx context.Context, after string, statuses []string) ([]Order, error) {
	var all []Order
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		query.Set("orderby", "date")
		query.Set("order", "desc")
		if after != "" {
			query.Set("after", after+"T00:00:00")
		}
		if len(statuses) > 0 {
			query.Set("status", strings.Join(statuses, ","))
		}
		var batch []wooOrder
		if err := c.get(ctx, "/wp-json/wc/v3/orders", query, &batch); err != nil {
			return nil, err
		}
		for _, o := range batch {
			all = append(all, o.toOrder())
		}
		if len(batch) < 100 {
			return all, nil
		}
	}
}

func (o wooOrder) toOrder() Order {
	country := o.Shipping.Country
	if country == "" {
		country = o.Billing.Country
	}
	out := Order{
		ID: o.ID, Status: o.Status, DateCreated: o.DateCreated, Currency: o.Currency,
		Total: o.Total, TotalTax: o.TotalTax, ShippingTotal: o.ShippingTotal,
		DiscountTotal: o.DiscountTotal, Country: country, Coupons: []string{}, Items: []OrderItem{},
	}
	for _, c := range o.CouponLines {
		out.Coupons = append(out.Coupons, c.Code)
	}
	for _, li := range o.LineItems {
		out.Items = append(out.Items, OrderItem{
			ProductID: li.ProductID, VariationID: li.VariationID, Name: li.Name,
			SKU: li.SKU, Quantity: li.Quantity, Total: li.Total,
		})
	}
	return out
}
