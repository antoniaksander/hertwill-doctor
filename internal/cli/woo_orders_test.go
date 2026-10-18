package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWooOrdersListsOrdersReadOnly(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/wp-json/wc/v3/orders" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":35395,"status":"completed","date_created":"2026-10-13T10:00:00","currency":"EUR","total":"61.05","total_tax":"11.82","shipping_total":"3.35","discount_total":"0.00","billing":{"country":"EE"},"shipping":{"country":""},"coupon_lines":[{"code":"welcome10"}],"line_items":[{"product_id":29955,"variation_id":30027,"name":"Merino Balaclava","sku":"BREM-1","quantity":1,"total":"34.64"}]}]`))
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)

	code, out, errOut := runCLI(t, "--json", "woo", "orders", "--after", "2026-10-01")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if !strings.Contains(query, "after=2026-10-01T00%3A00%3A00") || !strings.Contains(query, "status=completed%2Cprocessing%2Con-hold") {
		t.Fatalf("unexpected query %s", query)
	}
	var report ordersReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if report.Count != 1 || report.Total != 61.05 {
		t.Fatalf("unexpected totals %+v", report)
	}
	o := report.Orders[0]
	if o.Country != "EE" || len(o.Coupons) != 1 || o.Coupons[0] != "welcome10" {
		t.Fatalf("unexpected order %+v", o)
	}
	if len(o.Items) != 1 || o.Items[0].VariationID != 30027 || o.Items[0].Total != "34.64" {
		t.Fatalf("unexpected items %+v", o.Items)
	}
}

func TestWooOrdersRejectsBadDate(t *testing.T) {
	clearConfigEnv(t)
	setWooEnv(t, "http://127.0.0.1:1")
	code, _, errOut := runCLI(t, "woo", "orders", "--after", "1.10.2026")
	if code == 0 || !strings.Contains(errOut, "--after must be a date") {
		t.Fatalf("expected a date error, got code %d: %s", code, errOut)
	}
}
