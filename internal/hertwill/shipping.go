package hertwill

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ShippingPriceList is one of a brand's shipping price lists: per-order or
// per-item rates from the brand's origin country to each destination.
type ShippingPriceList struct {
	ID      int             `json:"id"`
	Name    string          `json:"name"`
	PerItem bool            `json:"per_item"`
	Prices  []ShippingPrice `json:"shipping_prices"`
}

type ShippingPrice struct {
	Origin      string        `json:"origin_iso_code"`
	Destination string        `json:"dest_iso_code"`
	Price       *ShippingCost `json:"price"`
}

// ShippingCost is a price in EUR. Authenticated responses send a plain
// number; public ones an {"amount","currency"} object (or null).
type ShippingCost struct {
	Amount float64
}

func (c *ShippingCost) UnmarshalJSON(data []byte) error {
	var amount float64
	if err := json.Unmarshal(data, &amount); err == nil {
		c.Amount = amount
		return nil
	}
	var object struct {
		Amount float64 `json:"amount"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	c.Amount = object.Amount
	return nil
}

func (c ShippingCost) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Amount)
}

// RawBrandShipping returns the raw JSON of a brand's shipping price lists.
// Prices are only included for authenticated requests.
func (c Client) RawBrandShipping(ctx context.Context, brandID int) ([]byte, error) {
	body, _, err := c.getBytes(ctx, strings.ReplaceAll(BrandShippingPath, "{id}", strconv.Itoa(brandID)), nil)
	return body, err
}

// BrandShipping returns a brand's shipping price lists.
func (c Client) BrandShipping(ctx context.Context, brandID int) ([]ShippingPriceList, error) {
	body, err := c.RawBrandShipping(ctx, brandID)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data []ShippingPriceList `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return nil, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	return envelope.Data, nil
}
