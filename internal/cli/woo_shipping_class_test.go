package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// newShippingClassServer serves one product (ID 35761) whose shipping class
// is set by PUT; slugs listed in known are applied, others are ignored the
// way WooCommerce ignores an unknown class slug.
func newShippingClassServer(t *testing.T, puts *[]string, known map[string]bool) {
	t.Helper()
	var mu sync.Mutex
	class := "brand-old"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/wp-json/wc/v3/products/35761" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			*puts = append(*puts, string(body))
			for slug := range known {
				if strings.Contains(string(body), `"shipping_class":"`+slug+`"`) {
					class = slug
				}
			}
			if string(body) == `{"shipping_class":""}` {
				class = ""
			}
		}
		w.Write([]byte(`{"id":35761,"name":"Sekhmet Ring","shipping_class":"` + class + `"}`))
	}))
	t.Cleanup(server.Close)
	clearConfigEnv(t)
	setWooEnv(t, server.URL)
}

func TestWooSetShippingClassDryRunSendsNothing(t *testing.T) {
	var puts []string
	newShippingClassServer(t, &puts, map[string]bool{"brand-new-vintage-by-kriss": true})
	code, out, errOut := runCLI(t, "woo", "set-shipping-class", "--id", "35761", "--class", "brand-new-vintage-by-kriss", "--dry-run")
	if code != 0 || len(puts) != 0 {
		t.Fatalf("code=%d puts=%v stderr=%q", code, puts, errOut)
	}
	if !strings.Contains(out, "Before:  shipping class brand-old") || !strings.Contains(out, "Dry run only") {
		t.Fatalf("out = %q", out)
	}
}

func TestWooSetShippingClassSendsOnlyTheClass(t *testing.T) {
	var puts []string
	newShippingClassServer(t, &puts, map[string]bool{"brand-new-vintage-by-kriss": true})
	code, out, errOut := runCLI(t, "woo", "set-shipping-class", "--id", "35761", "--class", "brand-new-vintage-by-kriss", "--confirm")
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, errOut)
	}
	if len(puts) != 1 || puts[0] != `{"shipping_class":"brand-new-vintage-by-kriss"}` {
		t.Fatalf("puts = %v", puts)
	}
	if !strings.Contains(out, "After:   shipping class brand-new-vintage-by-kriss") {
		t.Fatalf("out = %q", out)
	}
}

func TestWooSetShippingClassFailsWhenSlugIsNotApplied(t *testing.T) {
	var puts []string
	newShippingClassServer(t, &puts, map[string]bool{})
	code, _, errOut := runCLI(t, "woo", "set-shipping-class", "--id", "35761", "--class", "brand-typo", "--confirm")
	if code == 0 || !strings.Contains(errOut, "check that the class slug exists") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestWooSetShippingClassNoneRemovesTheClass(t *testing.T) {
	var puts []string
	newShippingClassServer(t, &puts, map[string]bool{})
	code, _, errOut := runCLI(t, "woo", "set-shipping-class", "--id", "35761", "--class", "none", "--confirm")
	if code != 0 || len(puts) != 1 || puts[0] != `{"shipping_class":""}` {
		t.Fatalf("code=%d puts=%v stderr=%q", code, puts, errOut)
	}
}
