package compare

import (
	"strings"

	"github.com/antoniaksander/hertwill-doctor/internal/model"
)

type Row struct {
	Field       string `json:"field"`
	WooCommerce string `json:"woocommerce"`
	Hertwill    string `json:"hertwill"`
	Match       string `json:"match"`
}

func Products(woo, hertwill model.Product) []Row {
	rows := []Row{
		{Field: "Found", WooCommerce: yesNo(woo.Found), Hertwill: yesNo(hertwill.Found), Match: matchBool(woo.Found, hertwill.Found)},
	}
	add := func(field, left, right string) {
		if left == "" {
			left = "n/a"
		}
		if right == "" {
			right = "n/a"
		}
		rows = append(rows, Row{Field: field, WooCommerce: left, Hertwill: right, Match: matchValue(left, right)})
	}
	add("ID", woo.ID, hertwill.ID)
	add("Name", woo.Name, hertwill.Name)
	add("SKU", woo.SKU, hertwill.SKU)
	add("Price", woo.Price, hertwill.Price)
	rows = append(rows, Row{Field: "Stock", WooCommerce: fallback(woo.Stock), Hertwill: fallback(hertwill.Stock), Match: matchStock(woo.Stock, hertwill.Stock)})
	add("Images", imageCount(woo), imageCount(hertwill))
	add("Status", woo.Status, hertwill.Status)
	add("Variations", variationCount(woo), variationCount(hertwill))
	return rows
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func matchBool(left, right bool) string {
	if left == right {
		return "yes"
	}
	return "no"
}

func matchValue(left, right string) string {
	if left == "n/a" || right == "n/a" {
		return "n/a"
	}
	if strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) {
		return "yes"
	}
	return "no"
}

func matchStock(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return "n/a"
	}
	if strings.EqualFold(left, right) {
		return "yes"
	}
	ll := strings.ToLower(left)
	rr := strings.ToLower(right)
	if strings.Contains(ll, rr) || strings.Contains(rr, ll) {
		return "partial"
	}
	return "no"
}

func fallback(value string) string {
	if strings.TrimSpace(value) == "" {
		return "n/a"
	}
	return value
}

func imageCount(product model.Product) string {
	if product.ImageCount == nil {
		return ""
	}
	return intString(*product.ImageCount)
}

func variationCount(product model.Product) string {
	if len(product.Variations) == 0 {
		return ""
	}
	return intString(len(product.Variations))
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
