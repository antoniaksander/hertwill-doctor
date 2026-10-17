package woocommerce

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type wooMeta struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// ProductMeta returns the product's string meta values for the given keys
// (missing keys map to "").
func (c Client) ProductMeta(ctx context.Context, id string, keys []string) (string, map[string]string, error) {
	var product struct {
		Name     string    `json:"name"`
		MetaData []wooMeta `json:"meta_data"`
	}
	if err := c.get(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), nil, &product); err != nil {
		return "", nil, err
	}
	return product.Name, pickMeta(product.MetaData, keys), nil
}

// UpdateProductMeta sets string meta values via PUT /products/{id} with only
// meta_data in the body. WooCommerce updates an existing key in place and
// adds it otherwise. Returns the values read back from the response.
func (c Client) UpdateProductMeta(ctx context.Context, id string, values map[string]string) (map[string]string, error) {
	meta := make([]wooMeta, 0, len(values))
	keys := make([]string, 0, len(values))
	for key, value := range values {
		meta = append(meta, wooMeta{Key: key, Value: value})
		keys = append(keys, key)
	}
	body, err := json.Marshal(map[string][]wooMeta{"meta_data": meta})
	if err != nil {
		return nil, err
	}
	var product struct {
		MetaData []wooMeta `json:"meta_data"`
	}
	if err := c.put(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), body, "WooCommerce PUT meta_data: "+strings.Join(keys, ", "), &product); err != nil {
		return nil, err
	}
	return pickMeta(product.MetaData, keys), nil
}

func pickMeta(meta []wooMeta, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = ""
	}
	for _, m := range meta {
		if _, ok := out[m.Key]; ok {
			if s, isString := m.Value.(string); isString {
				out[m.Key] = s
			}
		}
	}
	return out
}

// ShippingClass is a WooCommerce product shipping class.
type ShippingClass struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Count       int    `json:"count"`
}

// ListShippingClasses returns every shipping class, following pages.
func (c Client) ListShippingClasses(ctx context.Context) ([]ShippingClass, error) {
	var all []ShippingClass
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		var batch []ShippingClass
		if err := c.get(ctx, "/wp-json/wc/v3/products/shipping_classes", query, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			return all, nil
		}
	}
}

// CreateShippingClass creates a shipping class via POST /products/shipping_classes.
func (c Client) CreateShippingClass(ctx context.Context, name, slug, description string) (ShippingClass, error) {
	body, err := json.Marshal(map[string]string{"name": name, "slug": slug, "description": description})
	if err != nil {
		return ShippingClass{}, err
	}
	var created ShippingClass
	err = c.send(ctx, http.MethodPost, "/wp-json/wc/v3/products/shipping_classes", body, "WooCommerce POST shipping class "+slug, &created)
	return created, err
}

// ShippingZone is a zone with its location codes and its flat rate methods.
type ShippingZone struct {
	ID        int
	Name      string
	Countries []string
	FlatRates []FlatRate
}

// FlatRate is one flat_rate method instance and its settings values.
type FlatRate struct {
	InstanceID int
	Title      string
	Settings   map[string]string
}

// ShippingZones returns every zone with its country/region codes and flat
// rate methods (other methods are left out).
func (c Client) ShippingZones(ctx context.Context) ([]ShippingZone, error) {
	var zones []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := c.get(ctx, "/wp-json/wc/v3/shipping/zones", nil, &zones); err != nil {
		return nil, err
	}
	out := make([]ShippingZone, 0, len(zones))
	for _, z := range zones {
		zone := ShippingZone{ID: z.ID, Name: z.Name}
		var locations []struct {
			Code string `json:"code"`
			Type string `json:"type"`
		}
		if err := c.get(ctx, fmt.Sprintf("/wp-json/wc/v3/shipping/zones/%d/locations", z.ID), nil, &locations); err != nil {
			return nil, err
		}
		for _, l := range locations {
			if l.Type == "country" {
				zone.Countries = append(zone.Countries, l.Code)
			}
		}
		var methods []struct {
			InstanceID int    `json:"instance_id"`
			MethodID   string `json:"method_id"`
			Title      string `json:"title"`
			Settings   map[string]struct {
				Value any `json:"value"`
			} `json:"settings"`
		}
		if err := c.get(ctx, fmt.Sprintf("/wp-json/wc/v3/shipping/zones/%d/methods", z.ID), nil, &methods); err != nil {
			return nil, err
		}
		for _, m := range methods {
			if m.MethodID != "flat_rate" {
				continue
			}
			settings := map[string]string{}
			for key, s := range m.Settings {
				if v, ok := s.Value.(string); ok {
					settings[key] = v
				}
			}
			zone.FlatRates = append(zone.FlatRates, FlatRate{InstanceID: m.InstanceID, Title: m.Title, Settings: settings})
		}
		out = append(out, zone)
	}
	return out, nil
}

// UpdateFlatRateSettings sets the given settings on one flat rate method
// instance via PUT /shipping/zones/{zone}/methods/{instance}.
func (c Client) UpdateFlatRateSettings(ctx context.Context, zoneID, instanceID int, settings map[string]string) (map[string]string, error) {
	body, err := json.Marshal(map[string]map[string]string{"settings": settings})
	if err != nil {
		return nil, err
	}
	var method struct {
		Settings map[string]struct {
			Value any `json:"value"`
		} `json:"settings"`
	}
	path := fmt.Sprintf("/wp-json/wc/v3/shipping/zones/%d/methods/%d", zoneID, instanceID)
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	if err := c.put(ctx, path, body, "WooCommerce PUT flat rate settings: "+strings.Join(keys, ", "), &method); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for key := range settings {
		if v, ok := method.Settings[key].Value.(string); ok {
			out[key] = v
		}
	}
	return out, nil
}
