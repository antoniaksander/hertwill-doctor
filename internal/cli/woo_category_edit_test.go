package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// categoryEditRequest records a write sent to the fake category server.
type categoryEditRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

// newCategoryEditServer serves a small category tree. Category 230 holds a
// private product (WooCommerce's own count shows 0 for it).
func newCategoryEditServer(t *testing.T, writes *[]categoryEditRequest) {
	t.Helper()
	categories := `[
	  {"id":54,"name":"All","slug":"all","parent":0,"count":2000},
	  {"id":177,"name":"Men","slug":"men","parent":0,"count":273},
	  {"id":227,"name":"Men All bags","slug":"men-all-bags","parent":177,"count":63},
	  {"id":230,"name":"Men Wallets","slug":"men-wallets","parent":227,"count":0},
	  {"id":237,"name":"Men All Fashion","slug":"men-all-fashion","parent":177,"count":53},
	  {"id":791,"name":"Men T-shirts &amp; Tops","slug":"men-t-shirts-tops","parent":237,"count":0},
	  {"id":793,"name":"Men Jewellery &amp; Accessories","slug":"men-jewellery-accessories","parent":177,"count":0},
	  {"id":650,"name":"test 10","slug":"test-10","parent":0,"count":0},
	  {"id":606,"name":"Uncategorized","slug":"uncategorized","parent":0,"count":0}
	]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products/categories":
			w.Write([]byte(categories))
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products":
			if r.URL.Query().Get("status") != "any" {
				t.Errorf("product check must use status=any, got %q", r.URL.RawQuery)
			}
			if r.URL.Query().Get("category") == "230" {
				w.Write([]byte(`[{"id":1,"name":"Wallet","status":"private"}]`))
				return
			}
			w.Write([]byte(`[]`))
		case strings.HasPrefix(r.URL.Path, "/wp-json/wc/v3/products/categories/"):
			body, _ := io.ReadAll(r.Body)
			q := r.URL.Query()
			q.Del("consumer_key")
			q.Del("consumer_secret")
			*writes = append(*writes, categoryEditRequest{r.Method, r.URL.Path, q.Encode(), string(body)})
			id := strings.TrimPrefix(r.URL.Path, "/wp-json/wc/v3/products/categories/")
			w.Write([]byte(`{"id":` + id + `,"name":"Men Clothing","slug":"men-all-fashion","parent":177,"count":53}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr, BuildInfo{})
	return code, stdout.String(), stderr.String()
}

func TestWooUpdateCategoryRenameSendsOnlyName(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	code, out, errOut := runCLI(t, "woo", "update-category", "--id", "237", "--name", "Men Clothing", "--confirm")
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, errOut)
	}
	if len(writes) != 1 || writes[0].Method != http.MethodPut || writes[0].Path != "/wp-json/wc/v3/products/categories/237" || writes[0].Body != `{"name":"Men Clothing"}` {
		t.Fatalf("writes = %+v", writes)
	}
	if !strings.Contains(out, "Men All Fashion -> Men Clothing") || !strings.Contains(out, "Now:") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestWooUpdateCategoryMoveSendsOnlyParent(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	code, out, errOut := runCLI(t, "woo", "update-category", "--id", "230", "--parent", "793", "--confirm")
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, errOut)
	}
	if len(writes) != 1 || writes[0].Body != `{"parent":793}` {
		t.Fatalf("writes = %+v", writes)
	}
	if !strings.Contains(out, "Men > Men All bags -> Men > Men Jewellery & Accessories") || !strings.Contains(out, "changes the category URL") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestWooUpdateCategoryParentZeroMovesToTopLevel(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	code, _, errOut := runCLI(t, "woo", "update-category", "--id", "230", "--parent", "0", "--confirm")
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, errOut)
	}
	if len(writes) != 1 || writes[0].Body != `{"parent":0}` {
		t.Fatalf("writes = %+v", writes)
	}
}

func TestWooUpdateCategoryDryRunSendsNothing(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	code, out, errOut := runCLI(t, "woo", "update-category", "--id", "237", "--name", "Men Clothing", "--parent", "54", "--dry-run")
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, errOut)
	}
	if len(writes) != 0 {
		t.Fatalf("dry run sent writes: %+v", writes)
	}
	if !strings.Contains(out, "Dry run only") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestWooUpdateCategoryRefusals(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--id", "237", "--confirm"}, "--name and/or --parent"},
		{[]string{"--id", "237", "--name", "x"}, "exactly one of"},
		{[]string{"--id", "999", "--name", "x", "--confirm"}, "not found"},
		{[]string{"--id", "237", "--name", "men t-shirts & tops", "--confirm"}, "already exists"},
		{[]string{"--id", "237", "--name", "Men All Fashion", "--confirm"}, "already called"},
		{[]string{"--id", "237", "--parent", "237", "--confirm"}, "own parent"},
		{[]string{"--id", "177", "--parent", "791", "--confirm"}, "own subcategory"},
		{[]string{"--id", "237", "--parent", "177", "--confirm"}, "already under"},
		{[]string{"--id", "237", "--parent", "4242", "--confirm"}, "parent category 4242 not found"},
	} {
		code, _, errOut := runCLI(t, append([]string{"woo", "update-category"}, tc.args...)...)
		if code == 0 || !strings.Contains(errOut, tc.want) {
			t.Errorf("args %v: code = %d stderr=%q, want %q", tc.args, code, errOut, tc.want)
		}
	}
	if len(writes) != 0 {
		t.Fatalf("refused updates sent writes: %+v", writes)
	}
}

func TestWooDeleteCategoryConfirmForcesOnlyForCategories(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	code, out, errOut := runCLI(t, "woo", "delete-category", "--id", "650", "--confirm")
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, errOut)
	}
	if len(writes) != 1 || writes[0].Method != http.MethodDelete || writes[0].Path != "/wp-json/wc/v3/products/categories/650" || writes[0].Query != "force=true" || writes[0].Body != "" {
		t.Fatalf("writes = %+v", writes)
	}
	if !strings.Contains(out, "Deleted permanently") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestWooDeleteCategoryDryRunSendsNothing(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	code, out, errOut := runCLI(t, "woo", "delete-category", "--id", "650", "--dry-run")
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, errOut)
	}
	if len(writes) != 0 {
		t.Fatalf("dry run sent writes: %+v", writes)
	}
	if !strings.Contains(out, "no trash") || !strings.Contains(out, "Dry run only") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestWooDeleteCategoryRefusals(t *testing.T) {
	var writes []categoryEditRequest
	newCategoryEditServer(t, &writes)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--id", "650"}, "exactly one of"},
		{[]string{"--confirm"}, "--id is required"},
		{[]string{"--id", "999", "--confirm"}, "not found"},
		{[]string{"--id", "237", "--confirm"}, "has 1 subcategories"},
		{[]string{"--id", "230", "--confirm"}, "still has products"},
		{[]string{"--id", "606", "--confirm"}, "default category"},
	} {
		code, _, errOut := runCLI(t, append([]string{"woo", "delete-category"}, tc.args...)...)
		if code == 0 || !strings.Contains(errOut, tc.want) {
			t.Errorf("args %v: code = %d stderr=%q, want %q", tc.args, code, errOut, tc.want)
		}
	}
	if len(writes) != 0 {
		t.Fatalf("refused deletes sent writes: %+v", writes)
	}
}
