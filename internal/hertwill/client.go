package hertwill

import (
	"bytes"
	"context"
	"encoding/json"
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

const UnverifiedEmailPasswordMessage = "Hertwill email/password authentication endpoint is not verified yet. Provide HERTWILL_ACCESS_TOKEN or verify the auth endpoint in docs/hertwill-endpoints.md."

const UnverifiedSyncStatusMessage = "Hertwill sync-status endpoint is not verified yet."

type Client struct {
	BaseURL     string
	AccessToken string
	Email       string
	Password    string
	HTTPClient  *http.Client
	Timeout     time.Duration
	Stats       *httpstats.Stats
	Debug       func(string)
}

type RawResponse struct {
	Body     []byte
	Metadata map[string]any
}

func (c Client) Product(ctx context.Context, id string) (model.Product, error) {
	response, err := c.RawProduct(ctx, id)
	if err != nil {
		return model.Product{}, err
	}
	product, err := decodeProduct(response.Body)
	if err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return model.Product{}, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	return product.toModel(), nil
}

func (c Client) RawProduct(ctx context.Context, id string) (RawResponse, error) {
	body, metadata, err := c.getBytes(ctx, strings.ReplaceAll(ProductPath, "{id}", url.PathEscape(id)), nil)
	return RawResponse{Body: body, Metadata: metadata}, err
}

func (c Client) Search(ctx context.Context, query string, limit int) ([]model.Product, error) {
	response, err := c.RawSearch(ctx, query)
	if err != nil {
		return nil, err
	}
	products, err := decodeProducts(response.Body)
	if err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return nil, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	if limit > 0 && len(products) > limit {
		products = products[:limit]
	}
	out := make([]model.Product, 0, len(products))
	for _, product := range products {
		out = append(out, product.toModel())
	}
	return out, nil
}

func (c Client) ListProducts(ctx context.Context, limit int) ([]model.Product, error) {
	response, err := c.RawListProducts(ctx)
	if err != nil {
		return nil, err
	}
	products, err := decodeProducts(response.Body)
	if err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return nil, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	if limit > 0 && len(products) > limit {
		products = products[:limit]
	}
	out := make([]model.Product, 0, len(products))
	for _, product := range products {
		out = append(out, product.toModel())
	}
	return out, nil
}

func (c Client) ListProductsWithMetadata(ctx context.Context, limit int) ([]model.Product, map[string]any, error) {
	response, err := c.RawListProducts(ctx)
	if err != nil {
		return nil, nil, err
	}
	products, err := decodeProducts(response.Body)
	if err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return nil, nil, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	if limit > 0 && len(products) > limit {
		products = products[:limit]
	}
	out := make([]model.Product, 0, len(products))
	for _, product := range products {
		out = append(out, product.toModel())
	}
	return out, response.Metadata, nil
}

func (c Client) RawSearch(ctx context.Context, query string) (RawResponse, error) {
	body, metadata, err := c.getBytes(ctx, ProductSearchPath, url.Values{"q": {query}})
	return RawResponse{Body: body, Metadata: metadata}, err
}

func (c Client) RawListProducts(ctx context.Context) (RawResponse, error) {
	body, metadata, err := c.getBytes(ctx, ListProductsPath, nil)
	return RawResponse{Body: body, Metadata: metadata}, err
}

func (c Client) SyncStatus(ctx context.Context, id string) (map[string]any, error) {
	if !SyncStatusEndpointVerified() {
		return nil, fmt.Errorf(UnverifiedSyncStatusMessage)
	}
	var status map[string]any
	err := c.get(ctx, strings.ReplaceAll(SyncStatusPath, "{id}", url.PathEscape(id)), nil, &status)
	return status, err
}

func (c Client) get(ctx context.Context, path string, query url.Values, target any) error {
	body, _, err := c.getBytes(ctx, path, query)
	if err != nil {
		return err
	}
	if products, ok := target.(*[]hertwillProduct); ok {
		decoded, err := decodeProducts(body)
		if err != nil {
			if c.Stats != nil {
				c.Stats.HertwillFailure()
			}
			return fmt.Errorf("Hertwill returned invalid JSON: %w", err)
		}
		*products = decoded
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	return nil
}

func (c Client) getBytes(ctx context.Context, path string, query url.Values) ([]byte, map[string]any, error) {
	var body []byte
	var metadata map[string]any
	err := withRateLimitRetry(c.Debug, func() error {
		var err error
		body, metadata, err = c.getBytesOnce(ctx, path, query)
		return err
	})
	return body, metadata, err
}

func (c Client) getBytesOnce(ctx context.Context, path string, query url.Values) ([]byte, map[string]any, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, nil, err
	}
	base, err := url.Parse(strings.TrimRight(c.baseURL(), "/"))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Hertwill base URL: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	q := base.Query()
	for key, values := range query {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	base.RawQuery = q.Encode()

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if c.Debug != nil {
		c.Debug("Hertwill GET " + base.String() + " Authorization=Bearer ****")
	}
	if c.Stats != nil {
		c.Stats.HertwillAttempt()
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		if reqCtx.Err() == context.DeadlineExceeded {
			return nil, nil, fmt.Errorf("Hertwill request timed out after %s", timeout)
		}
		return nil, nil, fmt.Errorf("Hertwill request failed: %w", err)
	}
	defer resp.Body.Close()
	if c.Stats != nil {
		c.Stats.RecordHertwillRateLimit(resp.Header)
	}
	body, readErr := io.ReadAll(resp.Body)
	if c.Debug != nil {
		c.Debug("Hertwill response:")
		c.Debug(fmt.Sprintf("Status: %d", resp.StatusCode))
		c.Debug("Content-Type: " + resp.Header.Get("Content-Type"))
		c.Debug(fmt.Sprintf("Body bytes: %d", len(body)))
	}
	if readErr != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return nil, nil, fmt.Errorf("Hertwill response read failed: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return nil, nil, &APIError{StatusCode: resp.StatusCode, RetryAfter: retryAfter(resp.Header)}
	}
	return body, extractMetadata(body), nil
}

func decodeProducts(body []byte) ([]hertwillProduct, error) {
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var direct []hertwillProduct
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	for _, key := range []string{"products", "items", "data", "results"} {
		if value, ok := wrapped[key]; ok {
			if err := json.Unmarshal(value, &direct); err == nil {
				return direct, nil
			}
		}
	}
	return []hertwillProduct{}, nil
}

func decodeProduct(body []byte) (hertwillProduct, error) {
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return hertwillProduct{}, err
	}
	var product hertwillProduct
	if err := json.Unmarshal(raw, &product); err == nil && product.hasIdentity() {
		return product, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return hertwillProduct{}, err
	}
	if value, ok := wrapped["data"]; ok {
		if err := json.Unmarshal(value, &product); err != nil {
			return hertwillProduct{}, err
		}
		return product, nil
	}
	return product, nil
}

func extractMetadata(body []byte) map[string]any {
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil
	}
	metadata := map[string]any{}
	for _, key := range []string{"page", "per_page", "limit", "total", "total_pages", "next", "next_page", "cursor", "next_cursor"} {
		if value, ok := wrapped[key]; ok {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err == nil {
				metadata[key] = decoded
			}
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func (c Client) token(ctx context.Context) (string, error) {
	if strings.TrimSpace(c.AccessToken) != "" {
		return strings.TrimSpace(c.AccessToken), nil
	}
	if c.Email != "" && c.Password != "" {
		if !EmailPasswordAuthVerified() {
			return "", fmt.Errorf(UnverifiedEmailPasswordMessage)
		}
		return c.login(ctx)
	}
	return "", fmt.Errorf("missing Hertwill authentication; provide HERTWILL_ACCESS_TOKEN")
}

func (c Client) login(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{"email": c.Email, "password": c.Password})
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	base, _ := url.Parse(strings.TrimRight(c.baseURL(), "/") + LoginPath)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Stats != nil {
		c.Stats.HertwillAttempt()
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return "", fmt.Errorf("Hertwill login failed with HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Token       string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return "", err
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	if c.Stats != nil {
		c.Stats.HertwillFailure()
	}
	return "", fmt.Errorf("Hertwill login response did not include an access token")
}

func (c Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

type hertwillProduct struct {
	ID           any                 `json:"id"`
	Name         string              `json:"name"`
	Title        string              `json:"title"`
	SKU          string              `json:"sku"`
	ProductCode  string              `json:"product_code"`
	Code         string              `json:"code"`
	Price        any                 `json:"price"`
	Stock        any                 `json:"stock"`
	StockStatus  string              `json:"stock_status"`
	Availability string              `json:"availability"`
	Status       string              `json:"status"`
	Slug         string              `json:"slug"`
	Brand        namedEntity         `json:"brand"`
	Category     namedEntity         `json:"category"`
	Categories   []namedEntity       `json:"categories"`
	Images       flexibleImages      `json:"images"`
	ImageURLs    []string            `json:"image_urls"`
	Variations   []hertwillVariation `json:"variations"`
}

type namedEntity struct {
	ID   any    `json:"id"`
	Name string `json:"name"`
}

type hertwillVariation struct {
	ID          any             `json:"id"`
	SKU         string          `json:"sku"`
	Name        string          `json:"name"`
	Price       any             `json:"price"`
	Stock       any             `json:"stock"`
	StockStatus string          `json:"stock_status"`
	Image       any             `json:"image"`
	Images      flexibleImages  `json:"images"`
	Attributes  []variationAttr `json:"attributes"`
}

type variationAttr struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type flexibleImages struct {
	Array    []string
	Featured string
	Gallery  []string
}

func (i *flexibleImages) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		i.Array = arr
		return nil
	}
	var arrAny []any
	if err := json.Unmarshal(data, &arrAny); err == nil {
		for _, item := range arrAny {
			if s := stringify(item); s != "" {
				i.Array = append(i.Array, s)
			}
		}
		return nil
	}
	var obj struct {
		Featured string   `json:"featured"`
		Gallery  []string `json:"gallery"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		i.Featured = obj.Featured
		i.Gallery = obj.Gallery
		return nil
	}
	return nil
}

func (i flexibleImages) Count(extra ...string) int {
	return len(i.URLs(extra...))
}

// URLs returns the deduplicated, order-preserved image URLs across the
// array/featured/gallery shapes a Hertwill response may use, plus any extra
// URLs (such as a top-level image_urls field) appended by the caller.
func (i flexibleImages) URLs(extra ...string) []string {
	seen := map[string]struct{}{}
	var urls []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		urls = append(urls, value)
	}
	for _, value := range i.Array {
		add(value)
	}
	add(i.Featured)
	for _, value := range i.Gallery {
		add(value)
	}
	for _, value := range extra {
		add(value)
	}
	return urls
}

func (p hertwillProduct) hasIdentity() bool {
	return stringify(p.ID) != "" || p.SKU != "" || p.Name != "" || p.Title != ""
}

func (p hertwillProduct) toModel() model.Product {
	name := p.Name
	if name == "" {
		name = p.Title
	}
	sku := p.SKU
	if sku == "" {
		sku = p.ProductCode
	}
	if sku == "" {
		sku = p.Code
	}
	stock := stringify(p.Stock)
	if p.StockStatus != "" {
		if stock != "" {
			stock = p.StockStatus + " / " + stock
		} else {
			stock = p.StockStatus
		}
	}
	if stock == "" {
		stock = p.Availability
	}
	imageURLs := p.Images.URLs(p.ImageURLs...)
	count := len(imageURLs)
	categories := make([]string, 0, len(p.Categories))
	for _, category := range p.Categories {
		if category.Name != "" {
			categories = append(categories, category.Name)
		}
	}
	variations := make([]model.Variation, 0, len(p.Variations))
	for _, variation := range p.Variations {
		variations = append(variations, variation.toModel())
	}
	return model.Product{
		Found:      true,
		ID:         stringify(p.ID),
		Name:       name,
		SKU:        sku,
		Price:      stringify(p.Price),
		Stock:      stock,
		ImageCount: &count,
		ImageURLs:  imageURLs,
		Status:     p.Status,
		Brand:      p.Brand.Name,
		Category:   p.Category.Name,
		Categories: categories,
		Slug:       p.Slug,
		Variations: variations,
	}
}

func (v hertwillVariation) toModel() model.Variation {
	stock := stringify(v.Stock)
	if v.StockStatus != "" {
		if stock != "" {
			stock = v.StockStatus + " / " + stock
		} else {
			stock = v.StockStatus
		}
	}
	imageURL := ""
	if s, ok := v.Image.(string); ok {
		imageURL = s
	}
	count := v.Images.Count(imageURL)
	attrs := map[string]string{}
	for _, attr := range v.Attributes {
		if attr.Name != "" {
			attrs[attr.Name] = stringify(attr.Value)
		}
	}
	if len(attrs) == 0 {
		attrs = nil
	}
	return model.Variation{
		ID:         stringify(v.ID),
		SKU:        v.SKU,
		Name:       v.Name,
		Price:      stringify(v.Price),
		Stock:      stock,
		ImageCount: &count,
		Attributes: attrs,
	}
}

func stringify(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int:
		return strconv.Itoa(value)
	default:
		return fmt.Sprint(value)
	}
}
