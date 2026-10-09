package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newTermsWooServer(t *testing.T, puts *[]string) *httptest.Server {
	t.Helper()
	product := `{"id":31968,"name":"CALLA","sku":"calla","price":"18.95","status":"private","images":[],
	  "categories":[{"id":54,"name":"All"}],"brands":[]}`
	updated := `{"id":31968,"name":"CALLA","sku":"calla","price":"18.95","status":"private","images":[],
	  "categories":[{"id":54,"name":"All"},{"id":765,"name":"Scarves"}],"brands":[{"id":780,"name":"Breden"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wp-json/wc/v3/products/31968" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(product))
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			*puts = append(*puts, string(body))
			w.Write([]byte(updated))
		}
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
	return server
}

func TestWooSetTermsDryRunSendsNothing(t *testing.T) {
	var puts []string
	newTermsWooServer(t, &puts)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "set-terms", "--id", "31968", "--categories", "54,765", "--brands", "780", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(puts) != 0 {
		t.Fatalf("dry run sent PUT: %v", puts)
	}
	if !strings.Contains(stdout.String(), "All (54)") || !strings.Contains(stdout.String(), "Dry run only") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestWooSetTermsConfirmSendsOnlyTerms(t *testing.T) {
	var puts []string
	newTermsWooServer(t, &puts)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "set-terms", "--id", "31968", "--categories", "54,765", "--brands", "780", "--confirm", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(puts) != 1 || puts[0] != `{"brands":[{"id":780}],"categories":[{"id":54},{"id":765}]}` {
		t.Fatalf("PUT bodies = %v", puts)
	}
	var report setTermsReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.After == nil || report.After.Brand != "Breden (780)" || len(report.After.Categories) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestWooSetTermsOmitsUnchangedField(t *testing.T) {
	var puts []string
	newTermsWooServer(t, &puts)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "set-terms", "--id", "31968", "--brands", "780", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(puts) != 1 || puts[0] != `{"brands":[{"id":780}]}` {
		t.Fatalf("PUT bodies = %v", puts)
	}
}

func TestWooSetTermsValidation(t *testing.T) {
	var puts []string
	newTermsWooServer(t, &puts)
	for _, args := range [][]string{
		{"woo", "set-terms", "--id", "31968", "--brands", "780"},
		{"woo", "set-terms", "--id", "31968", "--dry-run"},
		{"woo", "set-terms", "--id", "31968", "--categories", "54,x", "--dry-run"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, BuildInfo{}); code == 0 {
			t.Fatalf("expected failure for %v", args)
		}
	}
	if len(puts) != 0 {
		t.Fatalf("invalid input sent PUT: %v", puts)
	}
}

func TestWooSetStatusSendsOnlyStatus(t *testing.T) {
	var puts []string
	newTermsWooServer(t, &puts)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "set-status", "--id", "31968", "--status", "publish", "--dry-run"}, &stdout, &stderr, BuildInfo{}); code != 0 || len(puts) != 0 {
		t.Fatalf("dry run: code=%d puts=%v stderr=%q", code, puts, stderr.String())
	}
	if code := Run([]string{"woo", "set-status", "--id", "31968", "--status", "publish", "--confirm"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(puts) != 1 || puts[0] != `{"status":"publish"}` {
		t.Fatalf("PUT bodies = %v", puts)
	}
	if code := Run([]string{"woo", "set-status", "--id", "31968", "--status", "trash", "--confirm"}, &stdout, &stderr, BuildInfo{}); code == 0 {
		t.Fatal("expected trash to be rejected")
	}
}

func TestWooSetPriceSendsOnlyRegularPrice(t *testing.T) {
	var puts []string
	newTermsWooServer(t, &puts)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "set-price", "--id", "31968", "--price", "16.5", "--dry-run"}, &stdout, &stderr, BuildInfo{}); code != 0 || len(puts) != 0 {
		t.Fatalf("dry run: code=%d puts=%v stderr=%q", code, puts, stderr.String())
	}
	if code := Run([]string{"woo", "set-price", "--id", "31968", "--price", "16.5", "--confirm"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(puts) != 1 || puts[0] != `{"regular_price":"16.50"}` {
		t.Fatalf("PUT bodies = %v", puts)
	}
}

// newVariableWooServer serves variable product 5 with the given number of
// variations (paged by per_page) and records variation batch bodies.
func newVariableWooServer(t *testing.T, count int, batches *[]string, writes *int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products/5":
			w.Write([]byte(`{"id":5,"name":"Boots","type":"variable","status":"publish","regular_price":"","images":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products/5/variations":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
			var items []string
			for id := (page-1)*perPage + 1; id <= count && id <= page*perPage; id++ {
				items = append(items, fmt.Sprintf(`{"id":%d,"regular_price":"140","sale_price":""}`, id))
			}
			w.Write([]byte("[" + strings.Join(items, ",") + "]"))
		case r.Method == http.MethodPost && r.URL.Path == "/wp-json/wc/v3/products/5/variations/batch":
			*writes++
			body, _ := io.ReadAll(r.Body)
			*batches = append(*batches, string(body))
			var req struct {
				Update []struct {
					ID           int    `json:"id"`
					RegularPrice string `json:"regular_price"`
				} `json:"update"`
			}
			json.Unmarshal(body, &req)
			var items []string
			for _, u := range req.Update {
				items = append(items, fmt.Sprintf(`{"id":%d,"regular_price":%q,"sale_price":""}`, u.ID, u.RegularPrice))
			}
			w.Write([]byte(`{"update":[` + strings.Join(items, ",") + `]}`))
		default:
			*writes++
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
}

func TestWooSetPriceUpdatesAllVariations(t *testing.T) {
	var batches []string
	var writes int
	newVariableWooServer(t, 3, &batches, &writes)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "set-price", "--id", "5", "--price", "154.95", "--dry-run"}, &stdout, &stderr, BuildInfo{}); code != 0 || writes != 0 {
		t.Fatalf("dry run: code=%d writes=%d stderr=%q", code, writes, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would change") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	stdout.Reset()
	if code := Run([]string{"woo", "set-price", "--id", "5", "--price", "154.95", "--confirm", "--json"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	want := `{"update":[{"id":1,"regular_price":"154.95"},{"id":2,"regular_price":"154.95"},{"id":3,"regular_price":"154.95"}]}`
	if len(batches) != 1 || batches[0] != want {
		t.Fatalf("batch bodies = %v", batches)
	}
	var report setPriceReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	e := report.Entries[0]
	if report.Changed != 1 || e.Variations != 3 || e.Before != "140" || e.After != "154.95" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestWooSetPriceBatchesOver100Variations(t *testing.T) {
	var batches []string
	var writes int
	newVariableWooServer(t, 150, &batches, &writes)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "set-price", "--id", "5", "--price", "99.95", "--confirm", "--json"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(batches) != 2 || strings.Count(batches[0], `"id"`) != 100 || strings.Count(batches[1], `"id"`) != 50 {
		t.Fatalf("got %d batches", len(batches))
	}
	var report setPriceReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Entries[0].Variations != 150 {
		t.Fatalf("variations = %d", report.Entries[0].Variations)
	}
}

func TestWooSetPriceRefusesVariableWithoutVariations(t *testing.T) {
	var batches []string
	var writes int
	newVariableWooServer(t, 0, &batches, &writes)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "set-price", "--id", "5", "--price", "20", "--confirm"}, &stdout, &stderr, BuildInfo{}); code == 0 {
		t.Fatal("expected refusal for a variable product without variations")
	}
	if writes != 0 || !strings.Contains(stdout.String(), "no variations") {
		t.Fatalf("writes=%d stdout=%s", writes, stdout.String())
	}
}

func TestWooSetNameSendsOnlyName(t *testing.T) {
	var puts []string
	newTermsWooServer(t, &puts)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "set-name", "--id", "31968", "--name", "New Name", "--dry-run"}, &stdout, &stderr, BuildInfo{}); code != 0 || len(puts) != 0 {
		t.Fatalf("dry run: code=%d puts=%v stderr=%q", code, puts, stderr.String())
	}
	if code := Run([]string{"woo", "set-name", "--id", "31968", "--name", "New Name", "--confirm"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(puts) != 1 || puts[0] != `{"name":"New Name"}` {
		t.Fatalf("PUT bodies = %v", puts)
	}
}

func TestWooTrashNeverForces(t *testing.T) {
	var deletes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes = append(deletes, r.URL.RawQuery)
			w.Write([]byte(`{"id":7,"name":"Dup","sku":"x","status":"trash","images":[]}`))
			return
		}
		w.Write([]byte(`{"id":7,"name":"Dup","sku":"x","status":"publish","images":[]}`))
	}))
	defer server.Close()
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "trash", "--id", "7", "--dry-run"}, &stdout, &stderr, BuildInfo{}); code != 0 || len(deletes) != 0 {
		t.Fatalf("dry run: code=%d deletes=%v stderr=%q", code, deletes, stderr.String())
	}
	if code := Run([]string{"woo", "trash", "--id", "7", "--confirm"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if len(deletes) != 1 || strings.Contains(deletes[0], "force") {
		t.Fatalf("DELETE queries = %v", deletes)
	}
}

func TestWooReplaceSendsOnlyThatField(t *testing.T) {
	var puts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			puts = append(puts, string(body))
			w.Write([]byte(`{"id":1,"name":"X","slug":"x","short_description":"<p>Candledust candles</p>","images":[]}`))
			return
		}
		w.Write([]byte(`{"id":1,"name":"X","slug":"x","short_description":"<p>Candlelust candles</p>","images":[]}`))
	}))
	defer server.Close()
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
	var stdout, stderr bytes.Buffer
	args := []string{"woo", "replace", "--id", "1", "--field", "short_description", "--find", "Candlelust", "--replace", "Candledust"}
	if code := Run(append(args, "--dry-run"), &stdout, &stderr, BuildInfo{}); code != 0 || len(puts) != 0 {
		t.Fatalf("dry run: code=%d puts=%v stderr=%q", code, puts, stderr.String())
	}
	if code := Run(append(args, "--confirm"), &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var sent map[string]string
	if len(puts) != 1 || json.Unmarshal([]byte(puts[0]), &sent) != nil || len(sent) != 1 || sent["short_description"] != "<p>Candledust candles</p>" {
		t.Fatalf("PUT bodies = %v", puts)
	}
	if code := Run([]string{"woo", "replace", "--id", "1", "--field", "short_description", "--find", "Nope", "--replace", "x", "--confirm"}, &stdout, &stderr, BuildInfo{}); code == 0 || len(puts) != 1 {
		t.Fatal("expected refusal when text is not found")
	}
}
