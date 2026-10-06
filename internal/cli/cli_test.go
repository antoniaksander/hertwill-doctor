package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/antoniaksander/hertwill-doctor/internal/model"
)

func TestVersionJSONCleanStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"version", "--json"}, &stdout, &stderr, BuildInfo{Version: "0.1.0", Commit: "abc1234", Date: "2026-07-07T12:00:00Z"})
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected clean stderr, got %q", stderr.String())
	}
	var payload BuildInfo
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.Version != "0.1.0" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestWooProductRequiresEitherSKUOrID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "product"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "provide either --sku or --id") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHelpCommandsExitSuccessfully(t *testing.T) {
	tests := [][]string{
		{"--help"},
		{"help"},
		{"woo", "--help"},
		{"help", "woo"},
		{"hertwill", "--help"},
		{"help", "hertwill"},
		{"diagnose", "--help"},
		{"help", "diagnose"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr, BuildInfo{})
			if code != 0 {
				t.Fatalf("code = %d stderr=%q", code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Fatalf("stdout missing usage: %q", stdout.String())
			}
		})
	}
}

func TestDoctorConfigOnlySkipsLiveChecks(t *testing.T) {
	clearConfigEnv(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "WooCommerce API: skipped - configuration incomplete or missing") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Hertwill API: skipped - configuration incomplete or missing") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "WooCommerce REST attempts: 0") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDoctorWooCommerceLiveSuccess(t *testing.T) {
	clearConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wp-json/wc/v3/products" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != "1" {
			t.Fatalf("per_page = %q", got)
		}
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	t.Setenv("WOO_BASE_URL", server.URL)
	t.Setenv("WOO_CONSUMER_KEY", "ck_test")
	t.Setenv("WOO_CONSUMER_SECRET", "cs_test")
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--json", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.LiveChecks.WooCommerce.Status != "ok" || payload.LiveChecks.WooCommerce.Message != "reachable" {
		t.Fatalf("unexpected woo check: %+v", payload.LiveChecks.WooCommerce)
	}
	if payload.LiveChecks.Hertwill.Status != "skipped" || payload.LiveChecks.Hertwill.Message != "no verified lightweight health/check endpoint yet" {
		t.Fatalf("unexpected hertwill check: %+v", payload.LiveChecks.Hertwill)
	}
	if !strings.Contains(stderr.String(), "WooCommerce REST attempts: 1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDoctorWooCommerceAuthFailure(t *testing.T) {
	clearConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	t.Setenv("WOO_BASE_URL", server.URL)
	t.Setenv("WOO_CONSUMER_KEY", "ck_test")
	t.Setenv("WOO_CONSUMER_SECRET", "cs_test")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--json", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.LiveChecks.WooCommerce.Status != "failed" || payload.LiveChecks.WooCommerce.Message != "invalid credentials or insufficient permissions" {
		t.Fatalf("unexpected woo check: %+v", payload.LiveChecks.WooCommerce)
	}
	if !strings.Contains(stderr.String(), "WooCommerce REST attempts: 1") || !strings.Contains(stderr.String(), "WooCommerce REST failures: 1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDoctorDoesNotFakeHertwillLiveSuccess(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--json", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.LiveChecks.Hertwill.Status != "skipped" || payload.LiveChecks.Hertwill.Message != "no verified lightweight health/check endpoint yet" {
		t.Fatalf("unexpected hertwill check: %+v", payload.LiveChecks.Hertwill)
	}
	if !strings.Contains(stderr.String(), "Hertwill API attempts: 0") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDiagnoseExistingWooCommerceProductWithoutHertwillID(t *testing.T) {
	clearConfigEnv(t)
	server := newWooProductServer(t, `[{"id":123,"name":"Pilot Boots","sku":"Aipi-2","price":"59.00","stock_status":"instock","status":"publish","images":[{}]}]`)
	defer server.Close()
	t.Setenv("WOO_BASE_URL", server.URL)
	t.Setenv("WOO_CONSUMER_KEY", "ck_test")
	t.Setenv("WOO_CONSUMER_SECRET", "cs_test")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "Aipi-2"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"Hertwill Doctor Diagnosis",
		"WooCommerce result:",
		"Found",
		"yes",
		"Hertwill result:",
		"* skipped - Hertwill configuration missing",
		"* Hertwill lookup skipped because configuration missing.",
		"* Product appears healthy in WooCommerce, but Hertwill status could not be confirmed.",
		"* Run hwd hertwill search --query <SKU> --debug to inspect Hertwill-side candidates.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestDiagnoseMissingWooCommerceProductWithoutHertwillID(t *testing.T) {
	clearConfigEnv(t)
	server := newWooProductServer(t, `[]`)
	defer server.Close()
	t.Setenv("WOO_BASE_URL", server.URL)
	t.Setenv("WOO_CONSUMER_KEY", "ck_test")
	t.Setenv("WOO_CONSUMER_SECRET", "cs_test")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "Aipi-2"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"Found",
		"no",
		"* Product not found in WooCommerce.",
		"* Hertwill lookup skipped because no Hertwill ID was provided.",
		"* WooCommerce product is missing. Check whether Hertwill has actually synced this product to the store.",
		"* Check WooCommerce logs or Hertwill import/sync status.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestDiagnoseJSONStructure(t *testing.T) {
	clearConfigEnv(t)
	server := newWooProductServer(t, `[{"id":123,"name":"Pilot Boots","sku":"Aipi-2","price":"59.00","stock_status":"instock","status":"publish","images":[]}]`)
	defer server.Close()
	t.Setenv("WOO_BASE_URL", server.URL)
	t.Setenv("WOO_CONSUMER_KEY", "ck_test")
	t.Setenv("WOO_CONSUMER_SECRET", "cs_test")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "Aipi-2", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if !payload.WooCommerce.Found || payload.Hertwill.Status != "skipped" || payload.LikelyCause == "" || len(payload.SuggestedNextAction) == 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.RequestSummary == nil || payload.RequestSummary.WooAttempts != 1 {
		t.Fatalf("unexpected request summary: %+v", payload.RequestSummary)
	}
}

func TestDiagnoseDebugKeepsSanitizedOutputOnStderr(t *testing.T) {
	clearConfigEnv(t)
	server := newWooProductServer(t, `[{"id":123,"name":"Pilot Boots","sku":"Aipi-2","price":"59.00","stock_status":"instock","status":"publish","images":[]}]`)
	defer server.Close()
	t.Setenv("WOO_BASE_URL", server.URL)
	t.Setenv("WOO_CONSUMER_KEY", "ck_test")
	t.Setenv("WOO_CONSUMER_SECRET", "cs_test")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "Aipi-2", "--json", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if payload.RequestSummary == nil || payload.RequestSummary.WooAttempts != 1 {
		t.Fatalf("unexpected request summary: %+v", payload.RequestSummary)
	}
	debugOutput := stderr.String()
	if !strings.Contains(debugOutput, "WooCommerce GET ") || !strings.Contains(debugOutput, "WooCommerce REST attempts: 1") {
		t.Fatalf("stderr missing debug details: %q", debugOutput)
	}
	if strings.Contains(debugOutput, "ck_test") || strings.Contains(debugOutput, "cs_test") {
		t.Fatalf("stderr leaked secrets: %q", debugOutput)
	}
}

func TestDiagnoseWooCommerceHealthWarnings(t *testing.T) {
	oneImage := 1
	zeroImages := 0
	tests := []struct {
		name                  string
		product               model.Product
		wantWarnings          []string
		wantNoWarnings        bool
		wantLikelyCause       string
		wantSuggestedFragment string
	}{
		{
			name: "published product with images is healthy",
			product: model.Product{
				Found: true, ID: "20832", SKU: "MOOMIN42B", Price: "94.95", Stock: "instock", Status: "publish", ImageCount: &oneImage,
			},
			wantNoWarnings:        true,
			wantLikelyCause:       "Product appears healthy in WooCommerce.",
			wantSuggestedFragment: "Re-run with --hertwill-id <ID> to include Hertwill status, if needed.",
		},
		{
			name: "private product warns",
			product: model.Product{
				Found: true, ID: "20832", SKU: "MOOMIN42B", Price: "94.95", Stock: "instock", Status: "private", ImageCount: &oneImage,
			},
			wantWarnings:          []string{"Product exists but is private."},
			wantLikelyCause:       "Product exists in WooCommerce but is not publicly visible yet. Hertwill imports may be private by default.",
			wantSuggestedFragment: "Open WooCommerce product ID 20832 and confirm product status/visibility.",
		},
		{
			name: "draft product warns",
			product: model.Product{
				Found: true, ID: "20832", SKU: "MOOMIN42B", Price: "94.95", Stock: "instock", Status: "draft", ImageCount: &oneImage,
			},
			wantWarnings:    []string{"Product exists but is draft."},
			wantLikelyCause: "Product exists in WooCommerce but appears incomplete or not publicly visible.",
		},
		{
			name: "zero images warns",
			product: model.Product{
				Found: true, ID: "20832", SKU: "MOOMIN42B", Price: "94.95", Stock: "instock", Status: "publish", ImageCount: &zeroImages,
			},
			wantWarnings:          []string{"Product has no images."},
			wantLikelyCause:       "Product exists in WooCommerce but appears incomplete. The image/media portion of the sync may have failed.",
			wantSuggestedFragment: "Check whether product images failed during Hertwill import/sync.",
		},
		{
			name: "private product with zero images warns twice",
			product: model.Product{
				Found: true, ID: "20832", SKU: "MOOMIN42B", Price: "94.95", Stock: "instock", Status: "private", ImageCount: &zeroImages,
			},
			wantWarnings: []string{
				"Product exists but is private.",
				"Product has no images.",
			},
			wantLikelyCause:       "Product exists in WooCommerce but appears incomplete. The image/media portion of the sync may have failed.",
			wantSuggestedFragment: "Run hwd hertwill search --query MOOMIN42B --debug to inspect Hertwill-side candidates.",
		},
		{
			name: "missing product keeps missing warnings",
			product: model.Product{
				Found: false, SKU: "MOOMIN42B",
			},
			wantWarnings: []string{
				"Product not found in WooCommerce.",
				"Hertwill lookup skipped because no Hertwill ID was provided.",
			},
			wantLikelyCause:       "WooCommerce product is missing. Check whether Hertwill has actually synced this product to the store.",
			wantSuggestedFragment: "Check WooCommerce logs or Hertwill import/sync status.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := buildDiagnoseReport(tt.product, "")
			if tt.wantNoWarnings && len(report.Warnings) != 0 {
				t.Fatalf("warnings = %+v", report.Warnings)
			}
			for _, warning := range tt.wantWarnings {
				if !containsString(report.Warnings, warning) {
					t.Fatalf("warning %q missing from %+v", warning, report.Warnings)
				}
			}
			if report.LikelyCause != tt.wantLikelyCause {
				t.Fatalf("likely cause = %q", report.LikelyCause)
			}
			if tt.wantSuggestedFragment != "" && !containsString(report.SuggestedNextAction, tt.wantSuggestedFragment) {
				t.Fatalf("suggested action %q missing from %+v", tt.wantSuggestedFragment, report.SuggestedNextAction)
			}
		})
	}
}

func TestDiagnoseAutoSearchesHertwillBySKU(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Bowl","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"publish","images":[{}]}]`)
	defer woo.Close()
	hw := newHertwillSearchServer(t, http.StatusOK, `[{"id":"hw-1","title":"Moomin Bowl","sku":"MOOMIN42B","status":"active"}]`)
	defer hw.Close()
	setWooEnv(t, woo.URL)
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Hertwill.Status != "found" || payload.Hertwill.Product == nil || payload.Hertwill.Product.ID != "hw-1" {
		t.Fatalf("unexpected hertwill result: %+v", payload.Hertwill)
	}
	if payload.Hertwill.MatchedBy != "sku" || len(payload.Hertwill.Searches) != 1 || payload.Hertwill.Searches[0].Type != "sku" {
		t.Fatalf("unexpected search metadata: %+v", payload.Hertwill)
	}
	if payload.RequestSummary == nil || payload.RequestSummary.HertwillAttempts != 1 || payload.RequestSummary.WooAttempts != 1 {
		t.Fatalf("unexpected request summary: %+v", payload.RequestSummary)
	}
}

func TestDiagnoseNoHertwillSearchFlagSkipsSearch(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Bowl","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"publish","images":[{}]}]`)
	defer woo.Close()
	setWooEnv(t, woo.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--no-hertwill-search", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Hertwill.Status != "skipped" || payload.RequestSummary.HertwillAttempts != 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestDiagnoseSkipsHertwillSearchWhenConfigMissing(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Bowl","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"publish","images":[{}]}]`)
	defer woo.Close()
	setWooEnv(t, woo.URL)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Hertwill.Status != "skipped" || payload.Hertwill.Message != "Hertwill configuration missing" {
		t.Fatalf("unexpected hertwill result: %+v", payload.Hertwill)
	}
}

func TestDiagnoseHertwillSearchAmbiguousAndNotFoundAndFailure(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantStatus string
		wantWarn   string
	}{
		{
			name:       "multiple candidates",
			statusCode: http.StatusOK,
			body:       `[{"id":"hw-1","title":"Moomin Bowl","sku":"MOOMIN42B"},{"id":"hw-2","title":"Moomin Mug","sku":"MOOMIN42B"}]`,
			wantStatus: "ambiguous",
			wantWarn:   "Hertwill search returned multiple possible matches.",
		},
		{
			name:       "no candidates",
			statusCode: http.StatusOK,
			body:       `[]`,
			wantStatus: "not_found",
			wantWarn:   "Hertwill title search returned no candidates.",
		},
		{
			name:       "search failure",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":"nope"}`,
			wantStatus: "failed",
			wantWarn:   "Hertwill lookup failed; run with --debug for sanitized details.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Bowl","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"publish","images":[{}]}]`)
			defer woo.Close()
			hw := newHertwillSearchServer(t, tt.statusCode, tt.body)
			defer hw.Close()
			setWooEnv(t, woo.URL)
			t.Setenv("HERTWILL_BASE_URL", hw.URL)
			t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

			var stdout, stderr bytes.Buffer
			code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--json"}, &stdout, &stderr, BuildInfo{})
			if code != 0 {
				t.Fatalf("code = %d stderr=%q", code, stderr.String())
			}
			var payload diagnoseReport
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Hertwill.Status != tt.wantStatus || !containsString(payload.Warnings, tt.wantWarn) {
				t.Fatalf("unexpected payload: %+v", payload)
			}
		})
	}
}

func TestDiagnoseFallbackTitleSearchCandidatesAreAmbiguous(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Adventure Rain Jacket - Yellow","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"private","images":[]}]`)
	defer woo.Close()
	hw := newHertwillSearchByQueryServer(t, map[string]string{
		"MOOMIN42B":                             `[]`,
		"Moomin Adventure Rain Jacket - Yellow": `[{"id":"hw-1","title":"Moomin Adventure Rain Jacket","sku":"HERT-1","status":"active"},{"id":"hw-2","title":"Moomin Rain Jacket Yellow","sku":"HERT-2","status":"active"}]`,
	})
	defer hw.Close()
	setWooEnv(t, woo.URL)
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--json", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Hertwill.Status != "ambiguous" || payload.Hertwill.Message != "no exact SKU match found, but title search returned possible candidates" {
		t.Fatalf("unexpected hertwill result: %+v", payload.Hertwill)
	}
	if len(payload.Hertwill.Searches) != 2 || payload.Hertwill.Searches[0].ResultCount != 0 || payload.Hertwill.Searches[1].ResultCount != 2 {
		t.Fatalf("unexpected searches: %+v", payload.Hertwill.Searches)
	}
	if len(payload.Hertwill.Candidates) != 2 || payload.Hertwill.MatchedBy != "" {
		t.Fatalf("unexpected candidates/match: %+v", payload.Hertwill)
	}
	if payload.RequestSummary == nil || payload.RequestSummary.HertwillAttempts != 2 {
		t.Fatalf("unexpected request summary: %+v", payload.RequestSummary)
	}
	for _, want := range []string{
		"Review Hertwill candidates and confirm whether one matches WooCommerce product ID/SKU.",
		"If the Hertwill candidate is correct, use its Hertwill ID with --hertwill-id <ID> once product-by-ID/sync-status endpoints are verified.",
		"Contact Hertwill support with WooCommerce product ID, WooCommerce SKU, product name, status, image count, and Hertwill candidate IDs.",
	} {
		if !containsString(payload.SuggestedNextAction, want) {
			t.Fatalf("suggested action %q missing from %+v", want, payload.SuggestedNextAction)
		}
	}
}

func TestDiagnoseFallbackTitleSearchNoCandidates(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Adventure Rain Jacket - Yellow","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"publish","images":[{}]}]`)
	defer woo.Close()
	hw := newHertwillSearchByQueryServer(t, map[string]string{
		"MOOMIN42B":                             `[]`,
		"Moomin Adventure Rain Jacket - Yellow": `[]`,
	})
	defer hw.Close()
	setWooEnv(t, woo.URL)
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Hertwill.Status != "not_found" || payload.Hertwill.Message != "no exact SKU or title match found" {
		t.Fatalf("unexpected hertwill result: %+v", payload.Hertwill)
	}
	if !containsString(payload.Warnings, "Hertwill product not found for SKU.") || !containsString(payload.Warnings, "Hertwill title search returned no candidates.") {
		t.Fatalf("warnings = %+v", payload.Warnings)
	}
	if payload.RequestSummary == nil || payload.RequestSummary.HertwillAttempts != 2 {
		t.Fatalf("unexpected request summary: %+v", payload.RequestSummary)
	}
}

func TestDiagnoseMoominLikeProductLikelyCauses(t *testing.T) {
	oneImage := 1
	zeroImages := 0
	privateNoImages := buildDiagnoseReport(model.Product{Found: true, ID: "20832", SKU: "MOOMIN42B", Price: "94.95", Stock: "instock", Status: "private", ImageCount: &zeroImages}, "")
	if privateNoImages.LikelyCause != "Product exists in WooCommerce but appears incomplete. The image/media portion of the sync may have failed." {
		t.Fatalf("likely cause = %q", privateNoImages.LikelyCause)
	}
	if !containsString(privateNoImages.Warnings, "Product exists but is private.") || !containsString(privateNoImages.Warnings, "Product has no images.") {
		t.Fatalf("warnings = %+v", privateNoImages.Warnings)
	}
	privateWithImages := buildDiagnoseReport(model.Product{Found: true, ID: "20832", SKU: "MOOMIN42B", Price: "94.95", Stock: "instock", Status: "private", ImageCount: &oneImage}, "")
	if privateWithImages.LikelyCause != "Product exists in WooCommerce but is not publicly visible yet. Hertwill imports may be private by default." {
		t.Fatalf("likely cause = %q", privateWithImages.LikelyCause)
	}
}

func TestDiagnoseDebugIncludesConfiguredLimitsAndRateLimitHeaders(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Bowl","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"publish","images":[{}]}]`)
	defer woo.Close()
	hw := newHertwillSearchServer(t, http.StatusOK, `[{"id":"hw-1","title":"Moomin Bowl","sku":"MOOMIN42B"}]`)
	defer hw.Close()
	setWooEnv(t, woo.URL)
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--json", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var payload diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequestSummary.RateLimitInfo["RateLimit"] != "299" {
		t.Fatalf("rate limit info = %+v", payload.RequestSummary.RateLimitInfo)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["rate_limit_info"]; ok {
		t.Fatalf("top-level rate_limit_info should not be present: %+v", raw)
	}
	debugOutput := stderr.String()
	for _, want := range []string{
		"This command:",
		"Configured limits:",
		"Hertwill public endpoints: 60 requests/minute per IP",
		"Hertwill authenticated endpoints: 300 requests/minute per API key",
		"RateLimit: 299",
		"RateLimit-Policy: 300;w=60",
	} {
		if !strings.Contains(debugOutput, want) {
			t.Fatalf("debug output missing %q:\n%s", want, debugOutput)
		}
	}
}

func TestHertwillListJSONAndContainsFilters(t *testing.T) {
	tests := []struct {
		name     string
		contains string
		wantID   string
	}{
		{name: "filters by name", contains: "Moomin", wantID: "p1"},
		{name: "filters by sku", contains: "RAIN42", wantID: "p1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/products" {
					t.Fatalf("path = %q", r.URL.Path)
				}
				w.Header().Set("RateLimit", "299")
				w.Write([]byte(`{"data":[{"id":"p1","title":"Moomin Rain Jacket","sku":"RAIN42","status":"active"},{"id":"p2","title":"Other","sku":"OTHER","status":"active"}]}`))
			}))
			defer server.Close()
			t.Setenv("HERTWILL_BASE_URL", server.URL)
			t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

			var stdout, stderr bytes.Buffer
			code := Run([]string{"hertwill", "list", "--limit", "100", "--contains", tt.contains, "--json", "--debug"}, &stdout, &stderr, BuildInfo{})
			if code != 0 {
				t.Fatalf("code = %d stderr=%q", code, stderr.String())
			}
			var payload hertwillListResult
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
			}
			if payload.Count != 1 || len(payload.Products) != 1 || payload.Products[0].ID != tt.wantID {
				t.Fatalf("payload = %+v", payload)
			}
			if payload.RequestSummary == nil || payload.RequestSummary.HertwillAttempts != 1 {
				t.Fatalf("request summary = %+v", payload.RequestSummary)
			}
			if payload.Note == "" {
				t.Fatalf("expected pagination note")
			}
		})
	}
}

func TestHertwillRawOutputAndDebugSeparation(t *testing.T) {
	clearConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[],"page":1,"total":0}`))
	}))
	defer server.Close()
	t.Setenv("HERTWILL_BASE_URL", server.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "list", "--raw", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("stdout is not JSON: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "token") || strings.Contains(stderr.String(), "Bearer token") {
		t.Fatalf("secret leaked stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	for _, want := range []string{"Hertwill response:", "Status: 200", "Content-Type: application/json", "Body bytes:"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestHertwillSearchRawOutput(t *testing.T) {
	clearConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/products/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`[{"id":"p1","title":"Moomin","sku":"MOOMIN"}]`))
	}))
	defer server.Close()
	t.Setenv("HERTWILL_BASE_URL", server.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "search", "--query", "MOOMIN", "--raw"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"sku": "MOOMIN"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestHertwillEmptyParsedProductsSuggestRaw(t *testing.T) {
	clearConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	t.Setenv("HERTWILL_BASE_URL", server.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	for _, args := range [][]string{
		{"hertwill", "search", "--query", "MOOMIN"},
		{"hertwill", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr, BuildInfo{})
			if code != 0 {
				t.Fatalf("code = %d stderr=%q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "No parsed products found.") || !strings.Contains(stdout.String(), "Use --raw to inspect the Hertwill response shape.") {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}

func TestHertwillProductJSONIncludesImagesAndVariations(t *testing.T) {
	clearConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(realHertwillProductResponseForCLI()))
	}))
	defer server.Close()
	t.Setenv("HERTWILL_BASE_URL", server.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "product", "--id", "3912", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var product model.Product
	if err := json.Unmarshal(stdout.Bytes(), &product); err != nil {
		t.Fatal(err)
	}
	if product.ID != "3912" || product.ImageCount == nil || *product.ImageCount != 18 || len(product.Variations) != 5 {
		t.Fatalf("unexpected product: %+v", product)
	}
}

func TestDiagnoseHertwillImagesWooNoneWarning(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Adventure Rain Jacket - Yellow","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"private","images":[]}]`)
	defer woo.Close()
	hw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(realHertwillProductResponseForCLI()))
	}))
	defer hw.Close()
	setWooEnv(t, woo.URL)
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var report diagnoseReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !containsString(report.Warnings, "Hertwill product has images, but WooCommerce has none.") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
	if report.LikelyCause != "Hertwill source product has images, but WooCommerce product has none. The media/image portion of the WooCommerce sync likely failed." {
		t.Fatalf("likely cause = %q", report.LikelyCause)
	}
	if report.Hertwill.Product == nil || report.Hertwill.Product.ImageCount == nil || *report.Hertwill.Product.ImageCount != 18 {
		t.Fatalf("hertwill product = %+v", report.Hertwill.Product)
	}
}

func TestDiagnoseWithHertwillIDDoesNotCallSyncStatus(t *testing.T) {
	clearConfigEnv(t)
	woo := newWooProductServer(t, `[{"id":20832,"name":"Moomin Adventure Rain Jacket - Yellow","sku":"MOOMIN42B","price":"94.95","stock_status":"instock","status":"private","images":[]}]`)
	defer woo.Close()
	hw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/products/3912" {
			t.Fatalf("unexpected Hertwill request to %q; sync-status must not be called while unverified", r.URL.Path)
		}
		w.Write([]byte(realHertwillProductResponseForCLI()))
	}))
	defer hw.Close()
	setWooEnv(t, woo.URL)
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"diagnose", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"WooCommerce result:",
		"Hertwill result:",
		"Product exists but is private.",
		"Product has no images.",
		"Hertwill product has images, but WooCommerce has none.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Hertwill sync status lookup failed") || strings.Contains(out, "Hertwill sync-status endpoint is not verified yet") {
		t.Fatalf("stdout should not mention sync-status: %s", out)
	}
	debugOut := stderr.String()
	for _, want := range []string{
		"Hertwill API attempts: 1",
		"Hertwill API failures: 0",
		"WooCommerce REST attempts: 1",
		"WooCommerce REST failures: 0",
	} {
		if !strings.Contains(debugOut, want) {
			t.Fatalf("debug output missing %q:\n%s", want, debugOut)
		}
	}
}

func TestHertwillSyncStatusCommandUnverified(t *testing.T) {
	clearConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Hertwill request to %q; sync-status endpoint is unverified", r.URL.Path)
	}))
	defer server.Close()
	t.Setenv("HERTWILL_BASE_URL", server.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"hertwill", "sync-status", "--id", "3912"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatalf("expected non-zero exit code, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "Hertwill sync-status endpoint is not verified yet.") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout output on failure, got %q", stdout.String())
	}
}

func realHertwillProductResponseForCLI() string {
	return `{"data":{"id":3912,"sku":"MOOMIN42B","name":"Moomin Adventure Rain Jacket - Yellow","price":59.24,"stock":20,"stock_status":"instock","slug":"moomin-adventure-rain-jacket-yellow","brand":{"id":42,"name":"Moomin by NordicBuddies"},"category":{"id":21,"name":"Outerwear"},"categories":[{"id":1,"name":"Apparel"},{"id":21,"name":"Outerwear"}],"images":{"featured":"https://assets.hertwill.com/1.jpg","gallery":["https://assets.hertwill.com/1.jpg","https://assets.hertwill.com/2.jpg","https://assets.hertwill.com/3.jpg","https://assets.hertwill.com/4.jpg","https://assets.hertwill.com/5.jpg","https://assets.hertwill.com/6.jpg","https://assets.hertwill.com/7.jpg","https://assets.hertwill.com/8.jpg","https://assets.hertwill.com/9.jpg","https://assets.hertwill.com/10.jpg","https://assets.hertwill.com/11.jpg","https://assets.hertwill.com/12.jpg","https://assets.hertwill.com/13.jpg","https://assets.hertwill.com/14.jpg","https://assets.hertwill.com/15.jpg","https://assets.hertwill.com/16.jpg","https://assets.hertwill.com/17.jpg","https://assets.hertwill.com/18.jpg"]},"variations":[{"id":8188,"sku":"MOOMIN42B-L","price":59.24,"stock":20,"stock_status":"instock","attributes":[{"name":"size","value":"L"}]},{"id":8189,"sku":"MOOMIN42B-M","price":59.24,"stock":20,"stock_status":"instock","attributes":[{"name":"size","value":"M"}]},{"id":8190,"sku":"MOOMIN42B-S","price":59.24,"stock":20,"stock_status":"instock","attributes":[{"name":"size","value":"S"}]},{"id":8191,"sku":"MOOMIN42B-XL","price":59.24,"stock":20,"stock_status":"instock","attributes":[{"name":"size","value":"XL"}]},{"id":8192,"sku":"MOOMIN42B-XS","price":59.24,"stock":20,"stock_status":"instock","attributes":[{"name":"size","value":"XS"}]}]}}`
}

func newWooProductServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wp-json/wc/v3/products" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		got := r.URL.Query().Get("sku")
		if got != "Aipi-2" && got != "MOOMIN42B" {
			t.Fatalf("sku = %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "1" {
			t.Fatalf("per_page = %q", got)
		}
		w.Write([]byte(body))
	}))
}

func newHertwillSearchServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/products/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		got := r.URL.Query().Get("q")
		if got != "MOOMIN42B" && got != "Moomin Bowl" {
			t.Fatalf("q = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("RateLimit", "299")
		w.Header().Set("RateLimit-Policy", "300;w=60")
		w.WriteHeader(statusCode)
		w.Write([]byte(body))
	}))
}

func newHertwillSearchByQueryServer(t *testing.T, responses map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/products/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		query := r.URL.Query().Get("q")
		body, ok := responses[query]
		if !ok {
			t.Fatalf("unexpected query = %q", query)
		}
		w.Header().Set("RateLimit", "299")
		w.Header().Set("RateLimit-Policy", "300;w=60")
		w.Write([]byte(body))
	}))
}

func setWooEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("WOO_BASE_URL", baseURL)
	t.Setenv("WOO_CONSUMER_KEY", "ck_test")
	t.Setenv("WOO_CONSUMER_SECRET", "cs_test")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"WOO_BASE_URL",
		"WOO_CONSUMER_KEY",
		"WOO_CONSUMER_SECRET",
		"HERTWILL_BASE_URL",
		"HERTWILL_ACCESS_TOKEN",
		"HERTWILL_EMAIL",
		"HERTWILL_PASSWORD",
		"HWD_TIMEOUT_SECONDS",
		"HWD_ENV_FILE",
		"HWD_STORE_NAME",
		"HERTWILL_API_KEY",
	} {
		t.Setenv(key, "")
	}
}

// repairWooServer is a stateful fake WooCommerce server for
// `hwd woo repair-images` tests. It serves sku search, PUT (image update),
// and the post-update GET re-fetch, all against one in-memory product.
type repairWooServer struct {
	server *httptest.Server

	mu           sync.Mutex
	id           string
	sku          string
	name         string
	status       string
	images       int
	putCount     int
	getByIDCount int
	lastPutBody  map[string]any
}

func newRepairWooServer(t *testing.T, id, sku, name, status string, initialImages int) *repairWooServer {
	t.Helper()
	rs := &repairWooServer{id: id, sku: sku, name: name, status: status, images: initialImages}
	rs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rs.mu.Lock()
		defer rs.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products":
			fmt.Fprintf(w, "[%s]", wooProductJSONObject(rs.id, rs.sku, rs.name, rs.status, rs.images))
		case r.Method == http.MethodPut && r.URL.Path == "/wp-json/wc/v3/products/"+rs.id:
			rs.putCount++
			var payload map[string]any
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("PUT body not JSON: %v", err)
			}
			rs.lastPutBody = payload
			if images, ok := payload["images"].([]any); ok {
				rs.images = len(images)
			}
			fmt.Fprint(w, wooProductJSONObject(rs.id, rs.sku, rs.name, rs.status, rs.images))
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products/"+rs.id:
			rs.getByIDCount++
			fmt.Fprint(w, wooProductJSONObject(rs.id, rs.sku, rs.name, rs.status, rs.images))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	return rs
}

func (rs *repairWooServer) Close() { rs.server.Close() }

func (rs *repairWooServer) URL() string { return rs.server.URL }

func (rs *repairWooServer) PutCount() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.putCount
}

func (rs *repairWooServer) GetByIDCount() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.getByIDCount
}

func (rs *repairWooServer) LastPutBody() map[string]any {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.lastPutBody
}

func (rs *repairWooServer) Images() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.images
}

func wooProductJSONObject(id, sku, name, status string, imageCount int) string {
	images := make([]string, imageCount)
	for i := range images {
		images[i] = "{}"
	}
	nameJSON, _ := json.Marshal(name)
	skuJSON, _ := json.Marshal(sku)
	statusJSON, _ := json.Marshal(status)
	var idField int
	fmt.Sscanf(id, "%d", &idField)
	return fmt.Sprintf(`{"id":%d,"name":%s,"sku":%s,"price":"94.95","stock_status":"instock","status":%s,"images":[%s]}`,
		idField, nameJSON, skuJSON, statusJSON, strings.Join(images, ","))
}

func hertwillProductJSON(id, sku, name string, imageURLs []string) string {
	urls, err := json.Marshal(imageURLs)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"data":{"id":%s,"sku":%q,"name":%q,"price":59.24,"stock":20,"stock_status":"instock","images":{"gallery":%s}}}`, id, sku, name, urls)
}

func newHertwillProductServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

func TestRepairImagesDryRunMakesNoWrite(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", []string{"https://a/1.jpg", "https://a/2.jpg"}))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if woo.PutCount() != 0 {
		t.Fatalf("expected no PUT request, got %d", woo.PutCount())
	}
	if !strings.Contains(stdout.String(), "Dry run only. No WooCommerce changes were made.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Images to apply") {
		t.Fatalf("stdout missing images-to-apply summary: %q", stdout.String())
	}
}

func TestRepairImagesConfirmSendsImagesOnlyPUTAndRefetches(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	urls := []string{"https://a/1.jpg", "https://a/2.jpg", "https://a/3.jpg"}
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", urls))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if woo.PutCount() != 1 {
		t.Fatalf("expected exactly one PUT request, got %d", woo.PutCount())
	}
	if woo.GetByIDCount() != 1 {
		t.Fatalf("expected exactly one re-fetch GET by ID, got %d", woo.GetByIDCount())
	}
	body := woo.LastPutBody()
	if len(body) != 1 {
		t.Fatalf("PUT payload should only contain images field, got keys: %+v", body)
	}
	images, ok := body["images"].([]any)
	if !ok || len(images) != 3 {
		t.Fatalf("images payload = %+v", body["images"])
	}
	if woo.Images() != 3 {
		t.Fatalf("expected WooCommerce image count 3 after update, got %d", woo.Images())
	}
	if !strings.Contains(stdout.String(), "Updated product ID 20832 images from 0 to 3.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Status remains private.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "No price, stock, description, status, or variations were changed.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRepairImagesRefusesWithoutDryRunOrConfirm(t *testing.T) {
	clearConfigEnv(t)
	setWooEnv(t, "http://example.invalid")
	t.Setenv("HERTWILL_BASE_URL", "http://example.invalid")
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "provide either --dry-run or --confirm") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRepairImagesRefusesWithBothDryRunAndConfirm(t *testing.T) {
	clearConfigEnv(t)
	setWooEnv(t, "http://example.invalid")
	t.Setenv("HERTWILL_BASE_URL", "http://example.invalid")
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--dry-run", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "provide only one of --dry-run or --confirm") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRepairImagesRefusesWhenWooAlreadyHasImagesUnlessForce(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 2)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", []string{"https://a/1.jpg", "https://a/2.jpg"}))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected refusal error")
	}
	if woo.PutCount() != 0 {
		t.Fatalf("expected no PUT request when refused, got %d", woo.PutCount())
	}
	if !strings.Contains(stdout.String(), "Provide --force to overwrite") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--confirm", "--force"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if woo.PutCount() != 1 {
		t.Fatalf("expected exactly one PUT request with --force, got %d", woo.PutCount())
	}
}

func TestRepairImagesRefusesSKUMismatch(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "OTHERSKU", "Moomin Adventure Rain Jacket - Yellow", []string{"https://a/1.jpg"}))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected refusal error")
	}
	if woo.PutCount() != 0 {
		t.Fatalf("expected no PUT request, got %d", woo.PutCount())
	}
	if !strings.Contains(stdout.String(), "WooCommerce SKU and Hertwill SKU do not match.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRepairImagesNameMismatchWarningButSKUMatchAllowed(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Rain Jacket (WooCommerce Name)", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", []string{"https://a/1.jpg"}))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--dry-run", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var report repairImagesReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.RefusedReason != "" {
		t.Fatalf("should not refuse on name mismatch alone: %+v", report)
	}
	if !containsString(report.Warnings, "WooCommerce product name and Hertwill product name differ.") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
}

func TestRepairImagesJSONDryRunOutput(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", []string{"https://a/1.jpg", "https://a/2.jpg"}))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--dry-run", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var report repairImagesReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != "dry-run" || report.Changed || report.RefusedReason != "" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.ImagesToApply) != 2 || report.WooCommerceAfter != nil {
		t.Fatalf("unexpected report: %+v", report)
	}
	if woo.PutCount() != 0 {
		t.Fatalf("dry-run must not write, PUT count = %d", woo.PutCount())
	}
}

func TestRepairImagesJSONConfirmOutput(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", []string{"https://a/1.jpg", "https://a/2.jpg"}))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--confirm", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var report repairImagesReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != "confirm" || !report.Changed {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.WooCommerceAfter == nil || report.WooCommerceAfter.ImageCount == nil || *report.WooCommerceAfter.ImageCount != 2 {
		t.Fatalf("unexpected woocommerce_after: %+v", report.WooCommerceAfter)
	}
}

func TestRepairImagesDebugMasksWooCommerceCredentials(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", []string{"https://a/1.jpg"}))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--confirm", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "ck_test") || strings.Contains(stderr.String(), "cs_test") {
		t.Fatalf("debug output leaked WooCommerce credentials: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "WooCommerce PUT") {
		t.Fatalf("debug output missing PUT log line: %s", stderr.String())
	}
}

func eighteenHertwillImageURLs() []string {
	urls := make([]string, 18)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://assets.hertwill.com/%d.jpg", i+1)
	}
	return urls
}

func TestRepairImagesMaxImagesOneSendsOnlyOneImage(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", eighteenHertwillImageURLs()))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "1", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if woo.PutCount() != 1 {
		t.Fatalf("expected exactly one PUT request, got %d", woo.PutCount())
	}
	body := woo.LastPutBody()
	images, ok := body["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("expected exactly one image in PUT payload, got %+v", body["images"])
	}
	first, ok := images[0].(map[string]any)
	if !ok || first["src"] != "https://assets.hertwill.com/1.jpg" {
		t.Fatalf("expected featured (first) image, got %+v", images[0])
	}
	if !strings.Contains(stdout.String(), "Updated product ID 20832 images from 0 to 1.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRepairImagesFeaturedOnlySendsOnlyOneImage(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", eighteenHertwillImageURLs()))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--featured-only", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if woo.PutCount() != 1 {
		t.Fatalf("expected exactly one PUT request, got %d", woo.PutCount())
	}
	body := woo.LastPutBody()
	images, ok := body["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("expected exactly one image in PUT payload, got %+v", body["images"])
	}
}

func TestRepairImagesMaxImagesThreeSendsFirstThreeInHertwillOrder(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", eighteenHertwillImageURLs()))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "3", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	body := woo.LastPutBody()
	images, ok := body["images"].([]any)
	if !ok || len(images) != 3 {
		t.Fatalf("expected exactly three images in PUT payload, got %+v", body["images"])
	}
	want := []string{"https://assets.hertwill.com/1.jpg", "https://assets.hertwill.com/2.jpg", "https://assets.hertwill.com/3.jpg"}
	for i, wantURL := range want {
		img, ok := images[i].(map[string]any)
		if !ok || img["src"] != wantURL {
			t.Fatalf("image[%d] = %+v, want src %q", i, images[i], wantURL)
		}
	}
}

func TestRepairImagesMaxImagesZeroRefuses(t *testing.T) {
	clearConfigEnv(t)
	setWooEnv(t, "http://example.invalid")
	t.Setenv("HERTWILL_BASE_URL", "http://example.invalid")
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "0", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "--max-images must be >= 1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRepairImagesMaxImagesAndFeaturedOnlyTogetherRefuses(t *testing.T) {
	clearConfigEnv(t)
	setWooEnv(t, "http://example.invalid")
	t.Setenv("HERTWILL_BASE_URL", "http://example.invalid")
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "3", "--featured-only", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "provide only one of --featured-only or --max-images") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRepairImagesMaxImagesDryRunReportsImageLimit(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", eighteenHertwillImageURLs()))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "1", "--dry-run"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if woo.PutCount() != 0 {
		t.Fatalf("dry-run must not write, PUT count = %d", woo.PutCount())
	}
	out := stdout.String()
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`Hertwill images total\s+18`),
		regexp.MustCompile(`Image limit\s+1`),
		regexp.MustCompile(`Images to apply\s+1`),
	} {
		if !want.MatchString(out) {
			t.Fatalf("stdout missing pattern %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Note: Only the first 1 Hertwill image(s) will be applied.") {
		t.Fatalf("stdout missing note:\n%s", out)
	}
}

func TestRepairImagesJSONIncludesImageLimitFields(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", eighteenHertwillImageURLs()))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "1", "--dry-run", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["image_limit"] != float64(1) {
		t.Fatalf("image_limit = %+v", raw["image_limit"])
	}
	if raw["hertwill_image_count_total"] != float64(18) {
		t.Fatalf("hertwill_image_count_total = %+v", raw["hertwill_image_count_total"])
	}
	if raw["images_to_apply_count"] != float64(1) {
		t.Fatalf("images_to_apply_count = %+v", raw["images_to_apply_count"])
	}
	images, ok := raw["images_to_apply"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("images_to_apply = %+v", raw["images_to_apply"])
	}
}

func TestRepairImagesAlreadyHasImagesWithMaxImagesRefusesUnlessForce(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 1)
	defer woo.Close()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", eighteenHertwillImageURLs()))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "3", "--confirm"}, &stdout, &stderr, BuildInfo{})
	if code == 0 {
		t.Fatal("expected refusal error")
	}
	if woo.PutCount() != 0 {
		t.Fatalf("expected no PUT request when refused, got %d", woo.PutCount())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--max-images", "3", "--confirm", "--force"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if woo.PutCount() != 1 {
		t.Fatalf("expected exactly one PUT request with --force, got %d", woo.PutCount())
	}
	body := woo.LastPutBody()
	images, ok := body["images"].([]any)
	if !ok || len(images) != 3 {
		t.Fatalf("expected exactly three images in PUT payload with --force, got %+v", body["images"])
	}
}

func TestRepairImagesDebugDoesNotDumpAllImageURLsForLargePayload(t *testing.T) {
	clearConfigEnv(t)
	woo := newRepairWooServer(t, "20832", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", "private", 0)
	defer woo.Close()
	urls := eighteenHertwillImageURLs()
	hw := newHertwillProductServer(t, hertwillProductJSON("3912", "MOOMIN42B", "Moomin Adventure Rain Jacket - Yellow", urls))
	defer hw.Close()
	setWooEnv(t, woo.URL())
	t.Setenv("HERTWILL_BASE_URL", hw.URL)
	t.Setenv("HERTWILL_ACCESS_TOKEN", "token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"woo", "repair-images", "--sku", "MOOMIN42B", "--hertwill-id", "3912", "--confirm", "--debug"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "WooCommerce PUT image count: 18") {
		t.Fatalf("debug output missing image count summary: %s", stderr.String())
	}
	for _, url := range urls {
		if strings.Contains(stderr.String(), url) {
			t.Fatalf("debug output dumped image URL %q for a large payload: %s", url, stderr.String())
		}
	}
}
