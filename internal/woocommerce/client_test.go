package woocommerce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
)

func TestProductBySKUSuccess(t *testing.T) {
	stats := &httpstats.Stats{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sku"); got != "ABC123" {
			t.Fatalf("sku query = %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "1" {
			t.Fatalf("per_page = %q", got)
		}
		w.Write([]byte(`[{"id":123,"name":"Pilot Boots","sku":"ABC123","price":"59.00","stock_status":"instock","status":"publish","images":[{},{}]}]`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, ConsumerKey: "ck", ConsumerSecret: "cs", Timeout: time.Second, Stats: stats}
	product, err := client.ProductBySKU(context.Background(), "ABC123")
	if err != nil {
		t.Fatal(err)
	}
	if !product.Found || product.ID != "123" || product.SKU != "ABC123" {
		t.Fatalf("unexpected product: %+v", product)
	}
	if stats.Snapshot().WooAttempts != 1 || stats.Snapshot().WooFailures != 0 {
		t.Fatalf("unexpected stats: %+v", stats.Snapshot())
	}
}

func TestTrashedProductBySKUQueriesTrash(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`[{"id":7,"name":"Blue Widgets","sku":"5113661-sinine","status":"trash","images":[]}]`))
	}))
	defer server.Close()
	product, err := (Client{BaseURL: server.URL, ConsumerKey: "ck", ConsumerSecret: "cs", Timeout: time.Second}).TrashedProductBySKU(context.Background(), "5113661-sinine")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("status") != "trash" || gotQuery.Get("sku") != "5113661-sinine" {
		t.Fatalf("query = %v", gotQuery)
	}
	if !product.Found || product.Status != "trash" {
		t.Fatalf("product = %+v", product)
	}
}

func TestProductBySKUEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	product, err := (Client{BaseURL: server.URL, ConsumerKey: "ck", ConsumerSecret: "cs", Timeout: time.Second}).ProductBySKU(context.Background(), "MISSING")
	if err != nil {
		t.Fatal(err)
	}
	if product.Found || product.SKU != "MISSING" {
		t.Fatalf("unexpected product: %+v", product)
	}
}

func TestAuthFailureCountsFailure(t *testing.T) {
	stats := &httpstats.Stats{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := (Client{BaseURL: server.URL, ConsumerKey: "bad", ConsumerSecret: "bad", Timeout: time.Second, Stats: stats}).ProductBySKU(context.Background(), "ABC123")
	if err == nil {
		t.Fatal("expected error")
	}
	snap := stats.Snapshot()
	if snap.WooAttempts != 1 || snap.WooFailures != 1 {
		t.Fatalf("unexpected stats: %+v", snap)
	}
}

func TestInvalidJSONCountsFailure(t *testing.T) {
	stats := &httpstats.Stats{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	_, err := (Client{BaseURL: server.URL, ConsumerKey: "ck", ConsumerSecret: "cs", Timeout: time.Second, Stats: stats}).ProductBySKU(context.Background(), "ABC123")
	if err == nil {
		t.Fatal("expected error")
	}
	snap := stats.Snapshot()
	if snap.WooAttempts != 1 || snap.WooFailures != 1 {
		t.Fatalf("unexpected stats: %+v", snap)
	}
}

func TestTimeoutCountsFailure(t *testing.T) {
	stats := &httpstats.Stats{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	_, err := (Client{BaseURL: server.URL, ConsumerKey: "ck", ConsumerSecret: "cs", Timeout: time.Millisecond, Stats: stats}).ProductBySKU(context.Background(), "ABC123")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	snap := stats.Snapshot()
	if snap.WooAttempts != 1 || snap.WooFailures != 1 {
		t.Fatalf("unexpected stats: %+v", snap)
	}
}

func TestUpdateProductImagesSendsImagesOnlyPayload(t *testing.T) {
	stats := &httpstats.Stats{}
	var gotMethod, gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Write([]byte(`{"id":20832,"sku":"MOOMIN42B","name":"Moomin","price":"94.95","stock_status":"instock","status":"private","images":[{},{}]}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, ConsumerKey: "ck", ConsumerSecret: "cs", Timeout: time.Second, Stats: stats}
	product, err := client.UpdateProductImages(context.Background(), "20832", []string{"https://a/1.jpg", "https://a/2.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotPath != "/wp-json/wc/v3/products/20832" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(gotBody) != 1 {
		t.Fatalf("payload should only contain images field, got keys: %+v", gotBody)
	}
	images, ok := gotBody["images"].([]any)
	if !ok || len(images) != 2 {
		t.Fatalf("images payload = %+v", gotBody["images"])
	}
	first, ok := images[0].(map[string]any)
	if !ok || first["src"] != "https://a/1.jpg" {
		t.Fatalf("first image = %+v", images[0])
	}
	if product.ID != "20832" || product.ImageCount == nil || *product.ImageCount != 2 {
		t.Fatalf("unexpected product: %+v", product)
	}
	if stats.Snapshot().WooAttempts != 1 || stats.Snapshot().WooFailures != 0 {
		t.Fatalf("unexpected stats: %+v", stats.Snapshot())
	}
}

func TestUpdateProductImagesDebugMasksCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":1,"images":[]}`))
	}))
	defer server.Close()

	var debugLines []string
	client := Client{
		BaseURL:        server.URL,
		ConsumerKey:    "ck_real_secret",
		ConsumerSecret: "cs_real_secret",
		Timeout:        time.Second,
		Debug:          func(line string) { debugLines = append(debugLines, line) },
	}
	if _, err := client.UpdateProductImages(context.Background(), "1", []string{"https://a/1.jpg"}); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(debugLines, "\n")
	if strings.Contains(all, "ck_real_secret") || strings.Contains(all, "cs_real_secret") {
		t.Fatalf("debug output leaked credentials: %s", all)
	}
	if !strings.Contains(all, "%2A%2A%2A%2A") {
		t.Fatalf("debug output missing masked credential marker: %s", all)
	}
	if !strings.Contains(all, "WooCommerce PUT") {
		t.Fatalf("debug output missing PUT log line: %s", all)
	}
}

func TestAPIErrorDoesNotLeakRequestURLSecrets(t *testing.T) {
	err := APIError{
		Kind: ErrorKindNetwork,
		Err:  errors.New(`Get "https://example.com/wp-json/wc/v3/products?consumer_key=ck_real&consumer_secret=cs_real": dial tcp: no such host`),
	}
	message := err.Error()
	if strings.Contains(message, "ck_real") || strings.Contains(message, "cs_real") || strings.Contains(message, "consumer_secret") {
		t.Fatalf("error leaked secret material: %s", message)
	}
}
