package woocommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	return c.productBySKU(ctx, sku, url.Values{})
}

// TrashedProductBySKU looks up a product in the WooCommerce trash. The default
// lookup (status "any") skips trashed products, but their SKUs still block a
// new product with the same SKU.
func (c Client) TrashedProductBySKU(ctx context.Context, sku string) (model.Product, error) {
	return c.productBySKU(ctx, sku, url.Values{"status": {"trash"}})
}

func (c Client) productBySKU(ctx context.Context, sku string, query url.Values) (model.Product, error) {
	query.Set("sku", sku)
	query.Set("per_page", "1")
	var products []wooProduct
	err := c.get(ctx, "/wp-json/wc/v3/products", query, &products)
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

// UpdateProductPrice sets only a simple product's regular price via
// PUT /products/{id}.
func (c Client) UpdateProductPrice(ctx context.Context, id, regularPrice string) (model.Product, error) {
	body, err := json.Marshal(map[string]string{"regular_price": regularPrice})
	if err != nil {
		return model.Product{}, err
	}
	var product wooProduct
	if err := c.put(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), body, "WooCommerce PUT regular_price: "+regularPrice, &product); err != nil {
		return model.Product{}, err
	}
	return product.toModel(), nil
}

// Variation is the price data of one variation of a variable product.
type Variation struct {
	ID           int    `json:"id"`
	SKU          string `json:"sku"`
	RegularPrice string `json:"regular_price"`
	SalePrice    string `json:"sale_price"`
}

// variationPageSize is both the page size for listing variations and the
// batch size for updating them; WooCommerce caps both at 100.
const variationPageSize = 100

// ProductVariations lists all variations of a variable product via
// GET /products/{id}/variations, following pages.
func (c Client) ProductVariations(ctx context.Context, id string) ([]Variation, error) {
	var all []Variation
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("per_page", strconv.Itoa(variationPageSize))
		query.Set("page", strconv.Itoa(page))
		var batch []Variation
		if err := c.get(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id)+"/variations", query, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < variationPageSize {
			return all, nil
		}
	}
}

// UpdateVariationPrices sets only regular_price on the given variations via
// POST /products/{id}/variations/batch, in chunks of 100.
func (c Client) UpdateVariationPrices(ctx context.Context, id string, variationIDs []int, regularPrice string) ([]Variation, error) {
	type update struct {
		ID           int    `json:"id"`
		RegularPrice string `json:"regular_price"`
	}
	type result struct {
		Variation
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	var updated []Variation
	for start := 0; start < len(variationIDs); start += variationPageSize {
		end := min(start+variationPageSize, len(variationIDs))
		updates := make([]update, 0, end-start)
		for _, vid := range variationIDs[start:end] {
			updates = append(updates, update{ID: vid, RegularPrice: regularPrice})
		}
		body, err := json.Marshal(map[string][]update{"update": updates})
		if err != nil {
			return nil, err
		}
		var resp struct {
			Update []result `json:"update"`
		}
		summary := fmt.Sprintf("WooCommerce POST variations batch: %d variations regular_price: %s", len(updates), regularPrice)
		if err := c.send(ctx, http.MethodPost, "/wp-json/wc/v3/products/"+url.PathEscape(id)+"/variations/batch", body, summary, &resp); err != nil {
			return updated, err
		}
		for _, r := range resp.Update {
			if r.Error != nil {
				return updated, fmt.Errorf("variation %d: %s", r.ID, r.Error.Message)
			}
			updated = append(updated, r.Variation)
		}
	}
	return updated, nil
}

// ProductFields returns the raw text fields that ReplaceInField can edit.
func (c Client) ProductFields(ctx context.Context, id string) (map[string]string, error) {
	var product wooProduct
	if err := c.get(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), nil, &product); err != nil {
		return nil, err
	}
	return map[string]string{
		"name":              product.Name,
		"slug":              product.Slug,
		"short_description": product.ShortDesc,
		"description":       product.Description,
	}, nil
}

// UpdateProductField sets one text field (name, slug, short_description or
// description) via PUT /products/{id}, sending only that field.
func (c Client) UpdateProductField(ctx context.Context, id, field, value string) (map[string]string, error) {
	body, err := json.Marshal(map[string]string{field: value})
	if err != nil {
		return nil, err
	}
	var product wooProduct
	if err := c.put(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), body, "WooCommerce PUT "+field, &product); err != nil {
		return nil, err
	}
	return map[string]string{
		"name":              product.Name,
		"slug":              product.Slug,
		"short_description": product.ShortDesc,
		"description":       product.Description,
	}, nil
}

// Category is a WooCommerce product category.
type Category struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Parent int    `json:"parent"`
}

// FindCategories returns categories whose name matches search.
func (c Client) FindCategories(ctx context.Context, search string) ([]Category, error) {
	var cats []Category
	err := c.get(ctx, "/wp-json/wc/v3/products/categories", url.Values{"search": {search}, "per_page": {"100"}}, &cats)
	return cats, err
}

// CreateCategory creates a product category under parent (0 for top level).
func (c Client) CreateCategory(ctx context.Context, name string, parent int) (Category, error) {
	body, err := json.Marshal(map[string]any{"name": name, "parent": parent})
	if err != nil {
		return Category{}, err
	}
	var cat Category
	err = c.send(ctx, http.MethodPost, "/wp-json/wc/v3/products/categories", body, "WooCommerce POST category "+name, &cat)
	return cat, err
}

// UpdateProductName changes only a product's name via PUT /products/{id}.
// The slug (URL) is left as it is.
func (c Client) UpdateProductName(ctx context.Context, id, name string) (model.Product, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return model.Product{}, err
	}
	var product wooProduct
	if err := c.put(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), body, "WooCommerce PUT name", &product); err != nil {
		return model.Product{}, err
	}
	return product.toModel(), nil
}

// UpdateProductStatus changes only a product's post status (publish,
// private, draft or pending) via PUT /products/{id}.
func (c Client) UpdateProductStatus(ctx context.Context, id, status string) (model.Product, error) {
	body, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return model.Product{}, err
	}
	var product wooProduct
	if err := c.put(ctx, "/wp-json/wc/v3/products/"+url.PathEscape(id), body, "WooCommerce PUT status: "+status, &product); err != nil {
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
	return c.send(ctx, http.MethodPut, path, body, debugBodySummary, target)
}

// TrashProduct moves a product to the WooCommerce trash (bin) via
// DELETE /products/{id} without force, so it can be restored from wp-admin.
// It never deletes permanently.
func (c Client) TrashProduct(ctx context.Context, id string) (model.Product, error) {
	var product wooProduct
	if err := c.send(ctx, http.MethodDelete, "/wp-json/wc/v3/products/"+url.PathEscape(id), nil, "WooCommerce DELETE (trash, no force)", &product); err != nil {
		return model.Product{}, err
	}
	return product.toModel(), nil
}

func (c Client) send(ctx context.Context, method, path string, body []byte, debugBodySummary string, target any) error {
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

	q.Del("force")
	base.RawQuery = q.Encode()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, base.String(), reader)
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
		c.Debug("WooCommerce " + method + " " + safeURL.String())
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
	RegularPrice  string    `json:"regular_price"`
	SalePrice     string    `json:"sale_price"`
	Type          string    `json:"type"`
	Slug          string    `json:"slug"`
	ShortDesc     string    `json:"short_description"`
	Description   string    `json:"description"`
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
		Regular:    p.RegularPrice,
		Sale:       p.SalePrice,
		Type:       p.Type,
		Stock:      stock,
		ImageCount: &imageCount,
		Status:     p.Status,
		Brand:      strings.Join(termLabels(p.Brands), ", "),
		Categories: termLabels(p.Categories),
	}
}
