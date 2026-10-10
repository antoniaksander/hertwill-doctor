package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// newTermsListServer serves a category tree (plus filler categories to force
// a second page) and two brands.
func newTermsListServer(t *testing.T, filler int) {
	t.Helper()
	categories := []string{
		`{"id":54,"name":"All","slug":"all","parent":0,"count":900}`,
		`{"id":178,"name":"Women","slug":"women","parent":0,"count":400}`,
		`{"id":182,"name":"Women Beauty products","slug":"women-beauty","parent":178,"count":200}`,
		`{"id":179,"name":"Women Skin care","slug":"women-skin-care","parent":182,"count":120}`,
		`{"id":184,"name":"Women Creams &amp; Scrubs","slug":"women-creams","parent":179,"count":85}`,
		`{"id":177,"name":"Men","slug":"men","parent":0,"count":300}`,
	}
	for i := 0; i < filler; i++ {
		categories = append(categories, fmt.Sprintf(`{"id":%d,"name":"Filler %03d","slug":"filler-%d","parent":54,"count":1}`, 1000+i, i, i))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		switch r.URL.Path {
		case "/wp-json/wc/v3/products/categories":
			start, end := (page-1)*perPage, page*perPage
			if start > len(categories) {
				start = len(categories)
			}
			if end > len(categories) {
				end = len(categories)
			}
			w.Write([]byte("[" + strings.Join(categories[start:end], ",") + "]"))
		case "/wp-json/wc/v3/products/brands":
			if page > 1 {
				w.Write([]byte(`[]`))
				return
			}
			w.Write([]byte(`[{"id":760,"name":"Wooden Story","slug":"wooden-story","parent":0,"count":24},{"id":700,"name":"Vegan Fox","slug":"vegan-fox","parent":0,"count":22}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
}

func TestWooCategoriesPrintsTree(t *testing.T) {
	newTermsListServer(t, 0)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "categories"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	// Children are indented under their parent; HTML entities are decoded.
	for _, want := range []string{"178   Women (400)", "  182   Women Beauty products (200)", "      184   Women Creams & Scrubs (85)", "6 categories"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "Women Beauty") < strings.Index(out, "178   Women") {
		t.Fatalf("child printed before parent:\n%s", out)
	}
}

func TestWooCategoriesContainsKeepsAncestors(t *testing.T) {
	newTermsListServer(t, 0)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "categories", "--contains", "creams", "--json"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var report termReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	var ids []int
	for _, e := range report.Terms {
		ids = append(ids, e.ID)
	}
	if fmt.Sprint(ids) != "[178 182 179 184]" {
		t.Fatalf("ids = %v", ids)
	}
	if last := report.Terms[3]; last.Path != "Women > Women Beauty products > Women Skin care > Women Creams & Scrubs" {
		t.Fatalf("path = %q", last.Path)
	}
}

func TestWooCategoriesFollowsPages(t *testing.T) {
	newTermsListServer(t, 150)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "categories", "--json"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var report termReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Count != 156 {
		t.Fatalf("count = %d, want 156", report.Count)
	}
}

func TestWooBrandsLists(t *testing.T) {
	newTermsListServer(t, 0)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"woo", "brands", "--contains", "fox"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "700   Vegan Fox (22)") || strings.Contains(stdout.String(), "Wooden Story") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
