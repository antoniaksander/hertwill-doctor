package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/antoniaksander/hertwill-doctor/internal/hertwill"
)

// fakeHertwill serves a two-page import list and records POST bodies.
type fakeHertwill struct {
	server *httptest.Server
	mu     sync.Mutex
	posts  map[string][]string
	// syncStatus maps product ID to the HTTP status returned for its sync.
	syncStatus map[int]int
}

const importListPage1 = `{"data":[
 {"id":9107,"product_id":3253248,"name":"BREDEN - Roadbuild Day","sku":"breden-roadbuild","status":"approved","price":19.82,"currency":"EUR",
  "variations":[{"id":20610,"dropship_id":3253249},{"id":20611,"dropship_id":3253251}]},
 {"id":4549,"product_id":3253480,"name":"Elf Hat JOLLY","sku":"elf-hat-jolly","status":"approved","price":8.8,"currency":"EUR","variations":[]}
],"meta":{"pagination":{"page":1,"per_page":20,"total":3,"page_count":2}}}`

const importListPage2 = `{"data":[
 {"id":8096,"product_id":3252544,"name":"BREM - Tractors","sku":"brem-tractors","status":"approved","price":23.01,"currency":"EUR",
  "variations":[{"id":18903,"dropship_id":3252548}]}
],"meta":{"pagination":{"page":2,"per_page":20,"total":3,"page_count":2}}}`

// importListSyncFailed is what ?status=sync-failed returns: 811 is not in the
// default list; 9107 is in both.
const importListSyncFailed = `{"data":[
 {"id":811,"product_id":3100001,"name":"Blue Widgets","sku":"5113661-sinine","status":"sync-failed","price":48.45,"currency":"EUR",
  "variations":[{"id":3001,"dropship_id":3100002}]},
 {"id":9107,"product_id":3253248,"name":"BREDEN - Roadbuild Day","sku":"breden-roadbuild","status":"sync-failed","price":19.82,"currency":"EUR",
  "variations":[{"id":20610,"dropship_id":3253249}]}
],"meta":{"pagination":{"page":1,"per_page":20,"total":2,"page_count":1}}}`

func newFakeHertwill(t *testing.T) *fakeHertwill {
	t.Helper()
	f := &fakeHertwill{posts: map[string][]string{}, syncStatus: map[int]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == hertwill.ImportListPath:
			if r.URL.Query().Get("status") == "sync-failed" {
				w.Write([]byte(importListSyncFailed))
				return
			}
			if r.URL.Query().Get("page") == "2" {
				w.Write([]byte(importListPage2))
				return
			}
			w.Write([]byte(importListPage1))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/products/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/products/")
			w.Write([]byte(`{"id":` + id + `,"name":"Product ` + id + `","sku":"sku-` + id + `","price":{"amount":10,"currency":"EUR"}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/import-list/products/"):
			f.mu.Lock()
			f.posts["DELETE "+r.URL.Path] = append(f.posts["DELETE "+r.URL.Path], "")
			f.mu.Unlock()
			w.Write([]byte(`{"data":{"removed":true}}`))
		case r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.posts[r.URL.Path] = append(f.posts[r.URL.Path], string(body))
			f.mu.Unlock()
			switch r.URL.Path {
			case hertwill.ImportListProductsPath:
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(`{"data":[{"product_id":8097,"status":"added","variations":[{"id":18907,"dropship_id":3252568}]}]}`))
			case hertwill.SyncProductsPath:
				var req hertwill.SyncRequest
				json.Unmarshal(body, &req)
				if status := f.syncStatus[req.ProductID]; status != 0 {
					w.WriteHeader(status)
					w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"You are not allowed to sync this product"}}`))
					return
				}
				w.WriteHeader(http.StatusAccepted)
				w.Write([]byte(`{"data":{"product_id":` + itoa(req.ProductID) + `,"status":"syncing","message":"Sync started"}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.server.Close)
	clearConfigEnv(t)
	t.Setenv("HERTWILL_BASE_URL", f.server.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = time.Sleep })
	return f
}

func (f *fakeHertwill) postCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts[path])
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func writePriceFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prices.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportListFetchesAllPages(t *testing.T) {
	newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "import-list", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload importListResult
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 3 || payload.Items[2].ID != 8096 || payload.Items[0].Variations[1].DropshipID != 3253251 {
		t.Fatalf("unexpected items: %+v", payload.Items)
	}
}

func TestImportListContainsFilter(t *testing.T) {
	newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "import-list", "--contains", "breden"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "9107") || strings.Contains(stdout.String(), "4549") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestSyncRequiresExactlyOneMode(t *testing.T) {
	newFakeHertwill(t)
	for _, args := range [][]string{
		{"hertwill", "sync", "--id", "9107", "--price", "40.95"},
		{"hertwill", "sync", "--id", "9107", "--price", "40.95", "--dry-run", "--confirm"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, BuildInfo{}); code == 0 {
			t.Fatalf("expected failure for %v", args)
		}
		if !strings.Contains(stderr.String(), "exactly one of --dry-run or --confirm") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	}
}

func TestSyncDryRunSendsNothing(t *testing.T) {
	f := newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "sync", "--id", "9107", "--price", "40.95", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if f.postCount(hertwill.SyncProductsPath) != 0 {
		t.Fatal("dry run must not POST")
	}
	if !strings.Contains(stdout.String(), "would sync") || !strings.Contains(stdout.String(), "Dry run only") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestSyncConfirmSendsAbsolutePriceOnEveryVariation(t *testing.T) {
	f := newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "sync", "--id", "9107", "--price", "40.95", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var req hertwill.SyncRequest
	if err := json.Unmarshal([]byte(f.posts[hertwill.SyncProductsPath][0]), &req); err != nil {
		t.Fatal(err)
	}
	want := hertwill.SyncRequest{ProductID: 9107, DefaultStoreMarkup: 40.95, Currency: "EUR", Variations: []hertwill.SyncVariation{
		{ID: 20610, DropshipID: 3253249, DefaultStoreMarkup: 40.95},
		{ID: 20611, DropshipID: 3253251, DefaultStoreMarkup: 40.95},
	}}
	got, _ := json.Marshal(req)
	exp, _ := json.Marshal(want)
	if string(got) != string(exp) {
		t.Fatalf("body = %s\nwant %s", got, exp)
	}
}

func TestSyncFileRefusesAndKeepsGoingAfterFailure(t *testing.T) {
	f := newFakeHertwill(t)
	f.syncStatus[9107] = http.StatusForbidden
	path := writePriceFile(t, "# id price\n9107 40.95\n4549, 5.00\n1234 10\n8096\t45.95\n")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "sync", "--file", path, "--confirm", "--json"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected non-zero exit when a product fails or is refused")
	}
	var report syncReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if report.Started != 1 || report.Failed != 1 || report.Refused != 2 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if !strings.Contains(report.Entries[0].Error, "FORBIDDEN") {
		t.Fatalf("missing API error: %+v", report.Entries[0])
	}
	if !strings.Contains(report.Entries[1].Refused, "below cost") || !strings.Contains(report.Entries[2].Refused, "not in the unsynced import list") {
		t.Fatalf("unexpected refusals: %+v", report.Entries)
	}
	if f.postCount(hertwill.SyncProductsPath) != 2 {
		t.Fatalf("expected 2 sync POSTs (refused ones skipped), got %d", f.postCount(hertwill.SyncProductsPath))
	}
}

func TestSyncRefusesSyncFailedWithoutRetryFlag(t *testing.T) {
	f := newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"hertwill", "sync", "--id", "811", "--price", "79.95", "--confirm"}, &stdout, &stderr, BuildInfo{}); code == 0 {
		t.Fatal("expected refusal without --retry-failed")
	}
	if f.postCount(hertwill.SyncProductsPath) != 0 || !strings.Contains(stdout.String(), "not in the unsynced import list") {
		t.Fatalf("posts=%d stdout=%s", f.postCount(hertwill.SyncProductsPath), stdout.String())
	}
}

func TestSyncRetryFailedSendsSyncFailedProduct(t *testing.T) {
	f := newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "sync", "--id", "811", "--price", "79.95", "--retry-failed", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q stdout=%s", code, stderr.String(), stdout.String())
	}
	var req hertwill.SyncRequest
	if err := json.Unmarshal([]byte(f.posts[hertwill.SyncProductsPath][0]), &req); err != nil {
		t.Fatal(err)
	}
	if req.ProductID != 811 || len(req.Variations) != 1 || req.Variations[0].DropshipID != 3100002 || req.Variations[0].DefaultStoreMarkup != 79.95 {
		t.Fatalf("unexpected request: %+v", req)
	}
}

func TestSyncRetryFailedKeepsDefaultListEntry(t *testing.T) {
	f := newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"hertwill", "sync", "--id", "9107", "--price", "40.95", "--retry-failed", "--confirm"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var req hertwill.SyncRequest
	json.Unmarshal([]byte(f.posts[hertwill.SyncProductsPath][0]), &req)
	if len(req.Variations) != 2 {
		t.Fatalf("expected the default-list entry with 2 variations, got %+v", req)
	}
}

// newFakeWooSKUs serves WooCommerce SKU lookups; skus maps SKU to product ID.
func newFakeWooSKUs(t *testing.T, skus map[string]int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := skus[r.URL.Query().Get("sku")]; ok {
			w.Write([]byte(`[{"id":` + itoa(id) + `,"name":"Live","sku":"` + r.URL.Query().Get("sku") + `","status":"publish","images":[]}]`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	setWooEnv(t, server.URL)
}

func TestRemoveDryRunSendsNothing(t *testing.T) {
	f := newFakeHertwill(t)
	newFakeWooSKUs(t, nil)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"hertwill", "remove", "--ids", "811", "--dry-run"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if f.postCount("DELETE /v1/import-list/products/811") != 0 || !strings.Contains(stdout.String(), "would remove") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRemoveConfirmDeletesEachID(t *testing.T) {
	f := newFakeHertwill(t)
	newFakeWooSKUs(t, nil)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"hertwill", "remove", "--ids", "811,913", "--confirm"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if f.postCount("DELETE /v1/import-list/products/811") != 1 || f.postCount("DELETE /v1/import-list/products/913") != 1 {
		t.Fatalf("deletes = %v", f.posts)
	}
}

func TestRemoveRefusesProductLiveInWooUnlessForced(t *testing.T) {
	f := newFakeHertwill(t)
	newFakeWooSKUs(t, map[string]int{"sku-811": 6192})
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"hertwill", "remove", "--ids", "811", "--confirm"}, &stdout, &stderr, BuildInfo{}); code == 0 {
		t.Fatal("expected refusal for a product that is in WooCommerce")
	}
	if f.postCount("DELETE /v1/import-list/products/811") != 0 || !strings.Contains(stdout.String(), "in WooCommerce as 6192") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	stdout.Reset()
	if code := Run([]string{"hertwill", "remove", "--ids", "811", "--confirm", "--force"}, &stdout, &stderr, BuildInfo{}); code != 0 {
		t.Fatalf("forced: code = %d stderr=%q", code, stderr.String())
	}
	if f.postCount("DELETE /v1/import-list/products/811") != 1 {
		t.Fatal("expected a DELETE with --force")
	}
}

func TestImportOnlyAddsMissingIDs(t *testing.T) {
	f := newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "import", "--ids", "8096,8097", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code != 0 || f.postCount(hertwill.ImportListProductsPath) != 0 {
		t.Fatalf("dry run: code=%d posts=%d stderr=%q", code, f.postCount(hertwill.ImportListProductsPath), stderr.String())
	}
	stdout.Reset()
	code = Run([]string{"hertwill", "import", "--ids", "8096,8097", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if got := f.posts[hertwill.ImportListProductsPath]; len(got) != 1 || got[0] != `{"product_ids":[8097]}` {
		t.Fatalf("import body = %v", got)
	}
	if !strings.Contains(stdout.String(), "8097") || !strings.Contains(stdout.String(), "added") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestReadPriceFileRejectsBadLines(t *testing.T) {
	for _, content := range []string{"9107\n", "abc 40.95\n", "9107 -1\n", "9107 40.95\n9107 41.95\n", "# only comments\n"} {
		if _, err := readPriceFile(writePriceFile(t, content)); err == nil {
			t.Fatalf("expected error for %q", content)
		}
	}
}

func TestSyncJobPrintsRawResponse(t *testing.T) {
	newFakeHertwill(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "sync-job", "--id", "4223"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("fake server has no sync-job route; expected an HTTP error")
	}
	if code := Run([]string{"hertwill", "sync-job"}, &stdout, &stderr, BuildInfo{}); code == 0 {
		t.Fatal("expected --id to be required")
	}
}
