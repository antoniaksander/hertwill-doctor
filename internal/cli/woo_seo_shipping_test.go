package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A variation ID must be priced through its parent's variations batch
// endpoint; PUT /products/{variation id} 404s on WooCommerce.
func TestWooSetPriceVariationUsesParentBatch(t *testing.T) {
	var mu sync.Mutex
	var batchBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products/36533":
			w.Write([]byte(`{"id":36533,"name":"Harness - XL","type":"variation","parent_id":36528,"regular_price":"42.95","status":"publish"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/wp-json/wc/v3/products/36528/variations/batch":
			body, _ := io.ReadAll(r.Body)
			batchBody = string(body)
			w.Write([]byte(`{"update":[{"id":36533,"regular_price":"39.95"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)

	code, out, errOut := runCLI(t, "woo", "set-price", "--id", "36533", "--price", "39.95", "--confirm")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if batchBody != `{"update":[{"id":36533,"regular_price":"39.95"}]}` {
		t.Fatalf("unexpected batch body %s", batchBody)
	}
	if !strings.Contains(out, "now 39.95") {
		t.Fatalf("expected result 'now 39.95', got:\n%s", out)
	}
}

func newMetaServer(t *testing.T, puts *[]string) {
	t.Helper()
	var mu sync.Mutex
	meta := map[string]string{"rank_math_title": "Old title"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/wp-json/wc/v3/products/36400" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			*puts = append(*puts, string(body))
			var req struct {
				MetaData []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"meta_data"`
			}
			json.Unmarshal(body, &req)
			for _, m := range req.MetaData {
				meta[m.Key] = m.Value
			}
		}
		var items []string
		for k, v := range meta {
			kb, _ := json.Marshal(k)
			vb, _ := json.Marshal(v)
			items = append(items, `{"id":1,"key":`+string(kb)+`,"value":`+string(vb)+`}`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":36400,"name":"Space Wall Decoration","meta_data":[` + strings.Join(items, ",") + `]}`))
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
}

func TestWooSetSEODryRunSendsNothing(t *testing.T) {
	var puts []string
	newMetaServer(t, &puts)
	code, out, errOut := runCLI(t, "woo", "set-seo", "--id", "36400", "--title", "New title | Roxder", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if len(puts) != 0 {
		t.Fatalf("dry run sent %v", puts)
	}
	if !strings.Contains(out, "before: Old title") || !strings.Contains(out, "Dry run only") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestWooSetSEOConfirmSendsOnlyMeta(t *testing.T) {
	var puts []string
	newMetaServer(t, &puts)
	code, out, errOut := runCLI(t, "woo", "set-seo", "--id", "36400", "--title", "New title | Roxder", "--description", "Short description.", "--confirm")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if len(puts) != 1 || !strings.HasPrefix(puts[0], `{"meta_data":[`) || strings.Contains(puts[0], `"name"`) {
		t.Fatalf("expected one meta_data-only PUT, got %v", puts)
	}
	if !strings.Contains(out, "after:  New title | Roxder") || !strings.Contains(out, "after:  Short description.") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func newZonesServer(t *testing.T, puts *[]string) {
	t.Helper()
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products/shipping_classes":
			w.Write([]byte(`[{"id":950,"name":"Matteo","slug":"brand-matteo","count":0}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/shipping/zones":
			w.Write([]byte(`[{"id":0,"name":"Locations not covered"},{"id":1,"name":"Estonia"},{"id":2,"name":"United Kingdom"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/shipping/zones/0/locations":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/shipping/zones/1/locations":
			w.Write([]byte(`[{"code":"EE","type":"country"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/shipping/zones/2/locations":
			w.Write([]byte(`[{"code":"GB","type":"country"}]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/methods"):
			w.Write([]byte(`[{"instance_id":7,"method_id":"flat_rate","title":"DPD delivery","settings":{"class_cost_950":{"value":""}}},{"instance_id":8,"method_id":"free_shipping","title":"Free","settings":{}}]`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/wp-json/wc/v3/shipping/zones/"):
			body, _ := io.ReadAll(r.Body)
			*puts = append(*puts, r.URL.Path+" "+string(body))
			var req struct {
				Settings map[string]string `json:"settings"`
			}
			json.Unmarshal(body, &req)
			w.Write([]byte(`{"instance_id":7,"settings":{"class_cost_950":{"value":"` + req.Settings["class_cost_950"] + `"}}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
}

func TestWooSetClassCostsDryRunSendsNothing(t *testing.T) {
	var puts []string
	newZonesServer(t, &puts)
	code, out, errOut := runCLI(t, "woo", "set-class-costs", "--class", "brand-matteo", "--costs", "EE=5.23", "--others", "-1", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if len(puts) != 0 {
		t.Fatalf("dry run sent %v", puts)
	}
	if !strings.Contains(out, "skipped: zone has no country locations") {
		t.Fatalf("expected the catch-all zone to be skipped:\n%s", out)
	}
}

func TestWooSetClassCostsConfirmSetsOnlyFlatRates(t *testing.T) {
	var puts []string
	newZonesServer(t, &puts)
	code, out, errOut := runCLI(t, "woo", "set-class-costs", "--class", "brand-matteo", "--costs", "EE=5.23", "--others", "-1", "--confirm")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	want := []string{
		`/wp-json/wc/v3/shipping/zones/1/methods/7 {"settings":{"class_cost_950":"5.23"}}`,
		`/wp-json/wc/v3/shipping/zones/2/methods/7 {"settings":{"class_cost_950":"-1"}}`,
	}
	if strings.Join(puts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected writes:\n%s", strings.Join(puts, "\n"))
	}
	if !strings.Contains(out, "Changed: 2, failed: 0") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestWooSetClassCostsUnknownClassFails(t *testing.T) {
	var puts []string
	newZonesServer(t, &puts)
	code, _, errOut := runCLI(t, "woo", "set-class-costs", "--class", "brand-typo", "--costs", "EE=5", "--dry-run")
	if code == 0 || !strings.Contains(errOut, "not found") {
		t.Fatalf("expected not-found error, got code %d: %s", code, errOut)
	}
}
