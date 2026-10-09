package hertwill

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
)

func TestSearchUsesBearerTokenAndQuery(t *testing.T) {
	stats := &httpstats.Stats{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ProductSearchPath {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "boots" {
			t.Fatalf("q = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "" {
			t.Fatalf("unexpected limit = %q", got)
		}
		w.Header().Set("RateLimit", "299")
		w.Header().Set("RateLimit-Policy", "300;w=60")
		w.Write([]byte(`[{"id":"p1","title":"Pilot Boots","sku":"ABC123","price":"59.00","availability":"in_stock","status":"synced","image_urls":["a","b"]}]`))
	}))
	defer server.Close()

	products, err := (Client{BaseURL: server.URL, AccessToken: "token", Timeout: time.Second, Stats: stats}).Search(context.Background(), "boots", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 || products[0].Name != "Pilot Boots" || products[0].ImageCount == nil || *products[0].ImageCount != 2 {
		t.Fatalf("unexpected products: %+v", products)
	}
	if stats.Snapshot().HertwillAttempts != 1 || stats.Snapshot().HertwillFailures != 0 {
		t.Fatalf("unexpected stats: %+v", stats.Snapshot())
	}
	if stats.Snapshot().RateLimitInfo["RateLimit"] != "299" {
		t.Fatalf("missing rate-limit info: %+v", stats.Snapshot().RateLimitInfo)
	}
}

func TestEmailPasswordAuthUnverified(t *testing.T) {
	_, err := (Client{Email: "a@example.com", Password: "secret"}).Product(context.Background(), "123")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != UnverifiedEmailPasswordMessage {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncStatusUnverifiedDoesNotCallEndpoint(t *testing.T) {
	stats := &httpstats.Stats{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %q; sync-status endpoint is unverified and must not be called", r.URL.Path)
	}))
	defer server.Close()

	_, err := (Client{BaseURL: server.URL, AccessToken: "token", Timeout: time.Second, Stats: stats}).SyncStatus(context.Background(), "3912")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != UnverifiedSyncStatusMessage {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Snapshot().HertwillAttempts != 0 {
		t.Fatalf("expected no Hertwill API attempts, got %+v", stats.Snapshot())
	}
}

func TestListProductsParsesShapesAndLimits(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "array", body: `[{"id":"p1","title":"One","sku":"SKU1"},{"id":"p2","title":"Two","sku":"SKU2"}]`},
		{name: "data", body: `{"data":[{"id":"p1","title":"One","sku":"SKU1"},{"id":"p2","title":"Two","sku":"SKU2"}]}`},
		{name: "products", body: `{"products":[{"id":"p1","title":"One","sku":"SKU1"},{"id":"p2","title":"Two","sku":"SKU2"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &httpstats.Stats{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != ListProductsPath {
					t.Fatalf("path = %q", r.URL.Path)
				}
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			products, err := (Client{BaseURL: server.URL, AccessToken: "token", Timeout: time.Second, Stats: stats}).ListProducts(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(products) != 1 || products[0].ID != "p1" {
				t.Fatalf("products = %+v", products)
			}
			if stats.Snapshot().HertwillAttempts != 1 {
				t.Fatalf("stats = %+v", stats.Snapshot())
			}
		})
	}
}

func TestListProductsWithMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"page":2,"per_page":25,"total":50,"total_pages":2,"next_cursor":"abc"}`))
	}))
	defer server.Close()

	products, metadata, err := (Client{BaseURL: server.URL, AccessToken: "token", Timeout: time.Second}).ListProductsWithMetadata(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 0 {
		t.Fatalf("products = %+v", products)
	}
	if metadata["page"] != float64(2) || metadata["next_cursor"] != "abc" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestProductParsesWrappedRealShapeImagesAndVariations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/products/3912" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Write([]byte(realProductResponse()))
	}))
	defer server.Close()

	product, err := (Client{BaseURL: server.URL, AccessToken: "token", Timeout: time.Second}).Product(context.Background(), "3912")
	if err != nil {
		t.Fatal(err)
	}
	if product.ID != "3912" || product.SKU != "MOOMIN42B" || product.Name != "Moomin Adventure Rain Jacket - Yellow" {
		t.Fatalf("unexpected product: %+v", product)
	}
	if product.ImageCount == nil || *product.ImageCount != 18 {
		t.Fatalf("image count = %+v", product.ImageCount)
	}
	if product.Brand != "Moomin by NordicBuddies" || product.Category != "Outerwear" || product.Slug != "moomin-adventure-rain-jacket-yellow" {
		t.Fatalf("metadata not parsed: %+v", product)
	}
	if len(product.Categories) != 2 || len(product.Variations) != 5 {
		t.Fatalf("categories/variations not parsed: %+v", product)
	}
	if product.Variations[0].SKU != "MOOMIN42B-L" || product.Variations[0].Attributes["size"] != "L" {
		t.Fatalf("variation not parsed: %+v", product.Variations[0])
	}
}

func TestImagesFeaturedDuplicateNotDoubleCounted(t *testing.T) {
	product, err := decodeProduct([]byte(`{"id":1,"images":{"featured":"a","gallery":["a","b"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	modelProduct := product.toModel()
	if modelProduct.ImageCount == nil || *modelProduct.ImageCount != 2 {
		t.Fatalf("image count = %+v", modelProduct.ImageCount)
	}
}

func realProductResponse() string {
	return `{"data":{"id":3912,"sku":"MOOMIN42B","name":"Moomin Adventure Rain Jacket - Yellow","description":"...","price":59.24,"stock":20,"stock_status":"instock","sale_price":null,"slug":"moomin-adventure-rain-jacket-yellow","brand":{"id":42,"name":"Moomin by NordicBuddies"},"category":{"id":21,"name":"Outerwear"},"categories":[{"id":1,"name":"Apparel"},{"id":21,"name":"Outerwear"}],"images":{"featured":"https://assets.hertwill.com/1.jpg","gallery":["https://assets.hertwill.com/1.jpg","https://assets.hertwill.com/2.jpg","https://assets.hertwill.com/3.jpg","https://assets.hertwill.com/4.jpg","https://assets.hertwill.com/5.jpg","https://assets.hertwill.com/6.jpg","https://assets.hertwill.com/7.jpg","https://assets.hertwill.com/8.jpg","https://assets.hertwill.com/9.jpg","https://assets.hertwill.com/10.jpg","https://assets.hertwill.com/11.jpg","https://assets.hertwill.com/12.jpg","https://assets.hertwill.com/13.jpg","https://assets.hertwill.com/14.jpg","https://assets.hertwill.com/15.jpg","https://assets.hertwill.com/16.jpg","https://assets.hertwill.com/17.jpg","https://assets.hertwill.com/18.jpg"]},"variations":[{"id":8188,"sku":"MOOMIN42B-L","name":"Moomin Adventure Rain Jacket - Yellow - L","price":59.24,"stock":20,"stock_status":"instock","image":null,"attributes":[{"name":"size","value":"L"}]},{"id":8189,"sku":"MOOMIN42B-M","name":"Moomin Adventure Rain Jacket - Yellow - M","price":59.24,"stock":20,"stock_status":"instock","image":null,"attributes":[{"name":"size","value":"M"}]},{"id":8190,"sku":"MOOMIN42B-S","name":"Moomin Adventure Rain Jacket - Yellow - S","price":59.24,"stock":20,"stock_status":"instock","image":null,"attributes":[{"name":"size","value":"S"}]},{"id":8191,"sku":"MOOMIN42B-XL","name":"Moomin Adventure Rain Jacket - Yellow - XL","price":59.24,"stock":20,"stock_status":"instock","image":null,"attributes":[{"name":"size","value":"XL"}]},{"id":8192,"sku":"MOOMIN42B-XS","name":"Moomin Adventure Rain Jacket - Yellow - XS","price":59.24,"stock":20,"stock_status":"instock","image":null,"attributes":[{"name":"size","value":"XS"}]}]},"meta":{"request_id":"req"}}`
}

func TestRateLimitIsRetried(t *testing.T) {
	retrySleep = func(time.Duration) {}
	defer func() { retrySleep = time.Sleep }()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"data":{"id":1,"name":"Hat","sku":"h"}}`))
	}))
	defer server.Close()
	product, err := (Client{BaseURL: server.URL, AccessToken: "token", Timeout: time.Second}).Product(context.Background(), "1")
	if err != nil || product.Name != "Hat" || calls != 3 {
		t.Fatalf("product=%+v err=%v calls=%d", product, err, calls)
	}
}
