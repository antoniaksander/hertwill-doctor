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
)

// ImportListItem is one product in the store's Hertwill import list.
// ID is the Hertwill catalog product ID; ProductID is the store-side dropship
// ID. Variations carry the dropship IDs that sync requests need.
type ImportListItem struct {
	ID                 int                   `json:"id"`
	ProductID          int                   `json:"product_id"`
	Name               string                `json:"name"`
	SKU                string                `json:"sku"`
	Status             string                `json:"status"`
	Price              float64               `json:"price"`
	Currency           string                `json:"currency"`
	DefaultStoreMarkup float64               `json:"default_store_markup"`
	StockStatus        string                `json:"stock_status"`
	Variations         []ImportListVariation `json:"variations"`
}

type ImportListVariation struct {
	ID         int    `json:"id"`
	DropshipID int    `json:"dropship_id"`
	SKU        string `json:"sku"`
}

// ImportResult is one entry of the POST /v1/import-list/products response.
type ImportResult struct {
	ProductID  int                   `json:"product_id"`
	Status     string                `json:"status"`
	Variations []ImportListVariation `json:"variations,omitempty"`
}

// SyncRequest is the POST /v1/sync/products body. DefaultStoreMarkup is the
// absolute selling price, not a multiplier: the live API rejects prices below
// wholesale cost with PRICE_BELOW_COST.
type SyncRequest struct {
	ProductID          int             `json:"product_id"`
	DefaultStoreMarkup float64         `json:"default_store_markup"`
	Currency           string          `json:"currency,omitempty"`
	Lang               string          `json:"lang,omitempty"`
	Variations         []SyncVariation `json:"variations,omitempty"`
}

type SyncVariation struct {
	ID                 int     `json:"id"`
	DropshipID         int     `json:"dropship_id"`
	DefaultStoreMarkup float64 `json:"default_store_markup"`
}

type SyncResult struct {
	ProductID int    `json:"product_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// APIError is a non-2xx Hertwill response with its error code and message.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" || e.Message != "" {
		return fmt.Sprintf("Hertwill HTTP %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("Hertwill request failed with HTTP %d", e.StatusCode)
}

// BuildSyncRequest builds a sync body that sets price on the product and on
// every variation of the import-list item.
func BuildSyncRequest(item ImportListItem, price float64, currency string) SyncRequest {
	req := SyncRequest{ProductID: item.ID, DefaultStoreMarkup: price, Currency: currency}
	for _, v := range item.Variations {
		req.Variations = append(req.Variations, SyncVariation{ID: v.ID, DropshipID: v.DropshipID, DefaultStoreMarkup: price})
	}
	return req
}

// ImportList returns every page of the store's import list.
func (c Client) ImportList(ctx context.Context, status string) ([]ImportListItem, error) {
	var all []ImportListItem
	for page, pages := 1, 1; page <= pages; page++ {
		query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"20"}}
		if status != "" {
			query.Set("status", status)
		}
		body, _, err := c.getBytes(ctx, ImportListPath, query)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Data []ImportListItem `json:"data"`
			Meta struct {
				Pagination struct {
					PageCount int `json:"page_count"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			if c.Stats != nil {
				c.Stats.HertwillFailure()
			}
			return nil, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
		}
		all = append(all, payload.Data...)
		pages = payload.Meta.Pagination.PageCount
	}
	return all, nil
}

// AddToImportList stages up to 50 catalog product IDs in the import list.
func (c Client) AddToImportList(ctx context.Context, productIDs []int) ([]ImportResult, error) {
	if len(productIDs) == 0 || len(productIDs) > 50 {
		return nil, fmt.Errorf("provide between 1 and 50 product IDs")
	}
	body, err := c.postJSON(ctx, ImportListProductsPath, map[string]any{"product_ids": productIDs})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []ImportResult `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	return payload.Data, nil
}

// SyncProduct starts a sync of one import-list product to the connected store.
func (c Client) SyncProduct(ctx context.Context, req SyncRequest) (SyncResult, error) {
	body, err := c.postJSON(ctx, SyncProductsPath, req)
	if err != nil {
		return SyncResult{}, err
	}
	var payload struct {
		Data SyncResult `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return SyncResult{}, fmt.Errorf("Hertwill returned invalid JSON: %w", err)
	}
	return payload.Data, nil
}

func (c Client) postJSON(ctx context.Context, path string, value any) ([]byte, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := strings.TrimRight(c.baseURL(), "/") + path
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if c.Debug != nil {
		c.Debug("Hertwill POST " + endpoint + " Authorization=Bearer **** Body bytes: " + strconv.Itoa(len(payload)))
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
			return nil, fmt.Errorf("Hertwill request timed out after %s", timeout)
		}
		return nil, fmt.Errorf("Hertwill request failed: %w", err)
	}
	defer resp.Body.Close()
	if c.Stats != nil {
		c.Stats.RecordHertwillRateLimit(resp.Header)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		return nil, fmt.Errorf("Hertwill response read failed: %w", err)
	}
	if c.Debug != nil {
		c.Debug(fmt.Sprintf("Hertwill response: Status %d, Body bytes: %d", resp.StatusCode, len(body)))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if c.Stats != nil {
			c.Stats.HertwillFailure()
		}
		apiErr := &APIError{StatusCode: resp.StatusCode}
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil {
			apiErr.Code = envelope.Error.Code
			apiErr.Message = envelope.Error.Message
		}
		return nil, apiErr
	}
	return body, nil
}

// RawSyncJob returns the raw GET /v1/sync/jobs/{productId} response for one
// catalog product, including any sync error details Hertwill recorded.
func (c Client) RawSyncJob(ctx context.Context, productID int) ([]byte, error) {
	body, _, err := c.getBytes(ctx, strings.ReplaceAll(SyncJobPath, "{productId}", strconv.Itoa(productID)), nil)
	return body, err
}
