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
