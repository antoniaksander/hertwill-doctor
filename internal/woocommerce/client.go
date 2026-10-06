package woocommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
	"github.com/antoniaksander/hertwill-doctor/internal/model"
)

type Client struct {
	BaseURL        string
	ConsumerKey    string
	ConsumerSecret string
	HTTPClient     *http.Client
	Timeout        time.Duration
	Stats          *httpstats.Stats
	Debug          func(string)
}

type ErrorKind string

const (
	ErrorKindHTTP        ErrorKind = "http"
	ErrorKindTimeout     ErrorKind = "timeout"
	ErrorKindInvalidJSON ErrorKind = "invalid_json"
	ErrorKindNetwork     ErrorKind = "network"
)

type APIError struct {
	Kind       ErrorKind
	StatusCode int
	Err        error
}

func (e APIError) Error() string {
	switch e.Kind {
	case ErrorKindTimeout:
		return "WooCommerce request timed out"
	case ErrorKindInvalidJSON:
		return "WooCommerce returned invalid JSON"
	case ErrorKindHTTP:
		return fmt.Sprintf("WooCommerce request failed with HTTP %d", e.StatusCode)
	default:
		return "WooCommerce request failed"
	}
}

func (e APIError) Unwrap() error {
	return e.Err
}

func ClassifyError(err error) (APIError, bool) {
	var apiErr APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return APIError{}, false
}

func (c Client) HealthCheck(ctx context.Context) error {
	var products []json.RawMessage
	return c.get(ctx, "/wp-json/wc/v3/products", url.Values{"per_page": {"1"}}, &products)
}

func (c Client) ProductBySKU(ctx context.Context, sku string) (model.Product, error) {
	var products []wooProduct
	err := c.get(ctx, "/wp-json/wc/v3/products", url.Values{"sku": {sku}, "per_page": {"1"}}, &products)
	if err != nil {
		return model.Product{}, err
	}
	if len(products) == 0 {
		return model.Product{Found: false, SKU: sku}, nil
	}
	return products[0].toModel(), nil
}

func (c Client) ProductByID(ctx context.Context, id string) (model.Product, error) {
	var product wooProduct
	err := c.get(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), nil, &product)
	if err != nil {
		return model.Product{}, err
	}
	return product.toModel(), nil
}

func (c Client) Search(ctx context.Context, query string, limit int) ([]model.Product, error) {
	var products []wooProduct
	err := c.get(ctx, "/wp-json/wc/v3/products", url.Values{"search": {query}, "per_page": {strconv.Itoa(limit)}}, &products)
	if err != nil {
		return nil, err
	}
	out := make([]model.Product, 0, len(products))
	for _, product := range products {
		out = append(out, product.toModel())
	}
	return out, nil
}

// UpdateProductImages replaces a WooCommerce product's image gallery with the
// given URLs via PUT /products/{id}. The request body contains only an
// "images" field; no other product fields (price, stock, status,
// description, variations, ...) are sent or changed.
func (c Client) UpdateProductImages(ctx context.Context, id string, imageURLs []string) (model.Product, error) {
	type imagePayload struct {
		Src string `json:"src"`
	}
	images := make([]imagePayload, 0, len(imageURLs))
	for _, imageURL := range imageURLs {
		images = append(images, imagePayload{Src: imageURL})
	}
	body, err := json.Marshal(struct {
		Images []imagePayload `json:"images"`
	}{Images: images})
	if err != nil {
		return model.Product{}, err
	}
	debugBodySummary := fmt.Sprintf("WooCommerce PUT image count: %d", len(imageURLs))
	var product wooProduct
	if err := c.put(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), body, debugBodySummary, &product); err != nil {
		return model.Product{}, err
	}
	return product.toModel(), nil
}

// UpdateProductTerms replaces a product's categories and/or brands via
// PUT /products/{id}. A nil slice leaves that field out of the request, so
// only the fields being changed are sent.
func (c Client) UpdateProductTerms(ctx context.Context, id string, categoryIDs, brandIDs []int) (model.Product, error) {
	type termRef struct {
		ID int `json:"id"`
	}
	refs := func(ids []int) []termRef {
		out := make([]termRef, 0, len(ids))
		for _, id := range ids {
			out = append(out, termRef{ID: id})
		}
		return out
	}
	payload := map[string]any{}
	if categoryIDs != nil {
		payload["categories"] = refs(categoryIDs)
	}
	if brandIDs != nil {
		payload["brands"] = refs(brandIDs)
	}
	if len(payload) == 0 {
		return model.Product{}, fmt.Errorf("nothing to update")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return model.Product{}, err
	}
	debugBodySummary := fmt.Sprintf("WooCommerce PUT categories: %v brands: %v", categoryIDs, brandIDs)
	var product wooProduct
	if err := c.put(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), body, debugBodySummary, &product); err != nil {
		return model.Product{}, err
	}
	return product.toModel(), nil
}

func (c Client) get(ctx context.Context, path string, query url.Values, target any) error {
	base, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil {
		return fmt.Errorf("invalid WooCommerce base URL: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	q := base.Query()
	for key, values := range query {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	q.Set("consumer_key", c.ConsumerKey)
	q.Set("consumer_secret", c.ConsumerSecret)
	base.RawQuery = q.Encode()

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base.String(), nil)
	if err != nil {
		return err
	}
	if c.Debug != nil {
		safeURL := *base
		safeQuery := safeURL.Query()
		safeQuery.Set("consumer_key", "****")
		safeQuery.Set("consumer_secret", "****")
		safeURL.RawQuery = safeQuery.Encode()
		c.Debug("WooCommerce GET " + safeURL.String())
	}
	if c.Stats != nil {
		c.Stats.WooAttempt()
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if c.Stats != nil {
			c.Stats.WooFailure()
		}
		if reqCtx.Err() == context.DeadlineExceeded {
			return APIError{Kind: ErrorKindTimeout, Err: err}
		}
		return APIError{Kind: ErrorKindNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if c.Stats != nil {
			c.Stats.WooFailure()
		}
		return APIError{Kind: ErrorKindHTTP, StatusCode: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		if c.Stats != nil {
			c.Stats.WooFailure()
		}
		return APIError{Kind: ErrorKindInvalidJSON, Err: err}
	}
	return nil
}

// put sends a PUT request. debugBodySummary is what gets logged for the
// request body when Debug is set, instead of the raw body bytes, so large
// payloads (e.g. many image URLs) don't spam debug output.
func (c Client) put(ctx context.Context, path string, body []byte, debugBodySummary string, target any) error {
	base, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil {
		return fmt.Errorf("invalid WooCommerce base URL: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	q := base.Query()
	q.Set("consumer_key", c.ConsumerKey)
	q.Set("consumer_secret", c.ConsumerSecret)
	base.RawQuery = q.Encode()

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPut, base.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Debug != nil {
		safeURL := *base
		safeQuery := safeURL.Query()
		safeQuery.Set("consumer_key", "****")
		safeQuery.Set("consumer_secret", "****")
		safeURL.RawQuery = safeQuery.Encode()
		c.Debug("WooCommerce PUT " + safeURL.String())
		if debugBodySummary != "" {
			c.Debug(debugBodySummary)
		}
	}
	if c.Stats != nil {
		c.Stats.WooAttempt()
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if c.Stats != nil {
			c.Stats.WooFailure()
		}
		if reqCtx.Err() == context.DeadlineExceeded {
			return APIError{Kind: ErrorKindTimeout, Err: err}
		}
		return APIError{Kind: ErrorKindNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if c.Stats != nil {
			c.Stats.WooFailure()
		}
		return APIError{Kind: ErrorKindHTTP, StatusCode: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		if c.Stats != nil {
			c.Stats.WooFailure()
		}
		return APIError{Kind: ErrorKindInvalidJSON, Err: err}
	}
	return nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

type wooProduct struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	SKU           string    `json:"sku"`
	Price         string    `json:"price"`
	StockStatus   string    `json:"stock_status"`
	Status        string    `json:"status"`
	Images        []any     `json:"images"`
	StockQuantity *int      `json:"stock_quantity"`
	Categories    []wooTerm `json:"categories"`
	Brands        []wooTerm `json:"brands"`
}

type wooTerm struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func termLabels(terms []wooTerm) []string {
	labels := make([]string, 0, len(terms))
	for _, term := range terms {
		labels = append(labels, fmt.Sprintf("%s (%d)", term.Name, term.ID))
	}
	return labels
}

func (p wooProduct) toModel() model.Product {
	imageCount := len(p.Images)
	stock := p.StockStatus
	if p.StockQuantity != nil {
		stock = strconv.Itoa(*p.StockQuantity)
	}
	return model.Product{
		Found:      true,
		ID:         strconv.Itoa(p.ID),
		Name:       p.Name,
		SKU:        p.SKU,
		Price:      p.Price,
		Stock:      stock,
		ImageCount: &imageCount,
		Status:     p.Status,
		Brand:      strings.Join(termLabels(p.Brands), ", "),
		Categories: termLabels(p.Categories),
	}
}
