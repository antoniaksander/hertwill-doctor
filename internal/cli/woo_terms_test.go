package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestWooSetPriceRefusesVariableProducts(t *testing.T) {
	var puts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
		}
		w.Write([]byte(`{"id":5,"name":"Hat","type":"variable","status":"publish","regular_price":"","images":[]}`))
	}))
	defer server.Close()
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "set-price", "--id", "5", "--price", "20", "--confirm"}, &stdout, &stderr, BuildInfo{}); code == 0 {
		t.Fatal("expected refusal for a variable product")
	}
	if puts != 0 || !strings.Contains(stdout.String(), "variable product") {
		t.Fatalf("puts=%d stdout=%s", puts, stdout.String())
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
