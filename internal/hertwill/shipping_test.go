package hertwill

import (
	"encoding/json"
	"testing"
)

func TestShippingCostAcceptsNumberObjectAndNull(t *testing.T) {
	body := []byte(`{"data":[{"id":1,"name":"EU","per_item":false,"shipping_prices":[
		{"origin_iso_code":"EE","dest_iso_code":"EE","price":2.99},
		{"origin_iso_code":"EE","dest_iso_code":"DE","price":{"amount":15.99,"currency":"EUR"}},
		{"origin_iso_code":"EE","dest_iso_code":"AU","price":null}]}]}`)
	var envelope struct {
		Data []ShippingPriceList `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	prices := envelope.Data[0].Prices
	if prices[0].Price == nil || prices[0].Price.Amount != 2.99 {
		t.Fatalf("number price: %+v", prices[0].Price)
	}
	if prices[1].Price == nil || prices[1].Price.Amount != 15.99 {
		t.Fatalf("object price: %+v", prices[1].Price)
	}
	if prices[2].Price != nil {
		t.Fatalf("null price should stay nil: %+v", prices[2].Price)
	}
}
