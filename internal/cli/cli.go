package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/antoniaksander/hertwill-doctor/internal/compare"
	"github.com/antoniaksander/hertwill-doctor/internal/config"
	"github.com/antoniaksander/hertwill-doctor/internal/hertwill"
	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
	"github.com/antoniaksander/hertwill-doctor/internal/model"
	"github.com/antoniaksander/hertwill-doctor/internal/woocommerce"
)

type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type globals struct {
	json    bool
	debug   bool
	noColor bool
}

func Run(args []string, stdout, stderr io.Writer, build BuildInfo) int {
	g, rest, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if len(rest) == 0 {
		rootHelp(stdout)
		return 0
	}
	if rest[0] == "help" {
		help(rest[1:], stdout)
		return 0
	}
	if isHelpArg(rest[0]) {
		rootHelp(stdout)
		return 0
	}

	cfg := config.Load()
	stats := &httpstats.Stats{}
	debug := func(line string) {
		if g.debug {
			fmt.Fprintln(stderr, line)
		}
	}

	var runErr error
	switch rest[0] {
	case "version":
		runErr = runVersion(rest[1:], stdout, stderr, g, build)
	case "doctor":
		runErr = runDoctor(rest[1:], stdout, stderr, g, cfg, stats, debug)
	case "woo":
		runErr = runWoo(rest[1:], stdout, stderr, g, cfg, stats, debug)
	case "hertwill":
		runErr = runHertwill(rest[1:], stdout, stderr, g, cfg, stats, debug)
	case "compare":
		runErr = runCompare(rest[1:], stdout, stderr, g, cfg, stats, debug)
	case "diagnose":
		runErr = runDiagnose(rest[1:], stdout, stderr, g, cfg, stats, debug)
	default:
		runErr = fmt.Errorf("unknown command %q", rest[0])
	}
	if g.debug {
		printRequestSummary(stderr, stats.Snapshot())
	}
	if runErr != nil {
		if g.json {
			fmt.Fprintln(stderr, runErr)
		} else {
			fmt.Fprintln(stderr, "Error:", runErr)
		}
		return 1
	}
	return 0
}

func parseGlobals(args []string) (globals, []string, error) {
	var g globals
	var rest []string
	for _, arg := range args {
		switch arg {
		case "--json":
			g.json = true
		case "--debug":
			g.debug = true
		case "--no-color":
			g.noColor = true
		default:
			rest = append(rest, arg)
		}
	}
	return g, rest, nil
}

func runVersion(args []string, stdout, _ io.Writer, g globals, build BuildInfo) error {
	if hasHelpArg(args) {
		versionHelp(stdout)
		return nil
	}
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonFlag := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if g.json || *jsonFlag {
		return writeJSON(stdout, build)
	}
	fmt.Fprintf(stdout, "Version: %s\nCommit: %s\nDate: %s\n", build.Version, build.Commit, build.Date)
	return nil
}

type doctorCheck struct {
	OK      bool   `json:"ok"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type doctorResult struct {
	ConfigChecks struct {
		WooCommerce config.Validation `json:"woocommerce"`
		Hertwill    config.Validation `json:"hertwill"`
		Timeout     doctorCheck       `json:"timeout"`
	} `json:"config_checks"`
	LiveChecks struct {
		WooCommerce doctorCheck `json:"woocommerce_api"`
		Hertwill    doctorCheck `json:"hertwill_api"`
	} `json:"live_checks"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Warnings       []string `json:"warnings"`
}

type hertwillListResult struct {
	Products       []model.Product  `json:"products"`
	Count          int              `json:"count"`
	Limit          int              `json:"limit"`
	Contains       string           `json:"contains,omitempty"`
	Note           string           `json:"note,omitempty"`
	Metadata       map[string]any   `json:"metadata,omitempty"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

func runDoctor(args []string, stdout, stderr io.Writer, g globals, cfg config.Config, stats *httpstats.Stats, debug func(string)) error {
	if hasHelpArg(args) {
		doctorHelp(stdout)
		return nil
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	woo := cfg.ValidateWoo()
	hw := cfg.ValidateHertwill()
	result := doctorResult{
		TimeoutSeconds: cfg.TimeoutSeconds,
		Warnings:       cfg.Warnings,
	}
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	result.ConfigChecks.WooCommerce = woo
	result.ConfigChecks.Hertwill = hw
	result.ConfigChecks.Timeout = doctorCheck{OK: true, Status: "ok", Message: fmt.Sprintf("%d seconds", cfg.TimeoutSeconds)}
	result.LiveChecks.WooCommerce = runWooDoctorCheck(cfg, woo, stats, debug)
	result.LiveChecks.Hertwill = runHertwillDoctorCheck(hw)

	if g.json {
		return writeJSON(stdout, result)
	}
	fmt.Fprintln(stdout, "Hertwill Doctor")
	fmt.Fprintf(stdout, "WooCommerce config: %s\n", statusLine(woo))
	fmt.Fprintf(stdout, "Hertwill config: %s\n", statusLine(hw))
	fmt.Fprintf(stdout, "HTTP timeout: %d seconds\n", cfg.TimeoutSeconds)
	fmt.Fprintf(stdout, "WooCommerce API: %s - %s\n", result.LiveChecks.WooCommerce.Status, result.LiveChecks.WooCommerce.Message)
	fmt.Fprintf(stdout, "Hertwill API: %s - %s\n", result.LiveChecks.Hertwill.Status, result.LiveChecks.Hertwill.Message)
	for _, warning := range cfg.Warnings {
		fmt.Fprintln(stderr, "Warning:", warning)
	}
	return nil
}

func runWooDoctorCheck(cfg config.Config, validation config.Validation, stats *httpstats.Stats, debug func(string)) doctorCheck {
	if !validation.OK {
		return doctorCheck{OK: false, Status: "skipped", Message: "configuration incomplete or missing"}
	}
	client := woocommerce.Client{
		BaseURL:        cfg.WooBaseURL,
		ConsumerKey:    cfg.WooConsumerKey,
		ConsumerSecret: cfg.WooConsumerSecret,
		Timeout:        cfg.Timeout(),
		Stats:          stats,
		Debug:          debug,
	}
	if err := client.HealthCheck(context.Background()); err != nil {
		if apiErr, ok := woocommerce.ClassifyError(err); ok {
			switch {
			case apiErr.Kind == woocommerce.ErrorKindHTTP && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403):
				return doctorCheck{OK: false, Status: "failed", Message: "invalid credentials or insufficient permissions"}
			case apiErr.Kind == woocommerce.ErrorKindTimeout:
				return doctorCheck{OK: false, Status: "failed", Message: "timeout"}
			case apiErr.Kind == woocommerce.ErrorKindInvalidJSON:
				return doctorCheck{OK: false, Status: "failed", Message: "invalid JSON response"}
			}
		}
		return doctorCheck{OK: false, Status: "failed", Message: "request failed; run with --debug for sanitized details"}
	}
	return doctorCheck{OK: true, Status: "ok", Message: "reachable"}
}

func runHertwillDoctorCheck(validation config.Validation) doctorCheck {
	if !validation.OK {
		return doctorCheck{OK: false, Status: "skipped", Message: "configuration incomplete or missing"}
	}
	if !hertwill.LightweightHealthEndpointVerified() {
		return doctorCheck{OK: false, Status: "skipped", Message: "no verified lightweight health/check endpoint yet"}
	}
	return doctorCheck{OK: false, Status: "skipped", Message: "no verified lightweight health/check endpoint yet"}
}

func runWoo(args []string, stdout, _ io.Writer, g globals, cfg config.Config, stats *httpstats.Stats, debug func(string)) error {
	if len(args) == 0 {
		wooHelp(stdout)
		return nil
	}
	if args[0] == "help" {
		wooHelpFor(args[1:], stdout)
		return nil
	}
	if isHelpArg(args[0]) {
		wooHelp(stdout)
		return nil
	}
	client := woocommerce.Client{
		BaseURL:        cfg.WooBaseURL,
		ConsumerKey:    cfg.WooConsumerKey,
		ConsumerSecret: cfg.WooConsumerSecret,
		Timeout:        cfg.Timeout(),
		Stats:          stats,
		Debug:          debug,
	}
	switch args[0] {
	case "product":
		if hasHelpArg(args[1:]) {
			wooProductHelp(stdout)
			return nil
		}
		fs := flag.NewFlagSet("woo product", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		sku := fs.String("sku", "", "")
		id := fs.String("id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *sku == "" && *id == "" {
			return fmt.Errorf("provide either --sku or --id")
		}
		if *sku != "" && *id != "" {
			return fmt.Errorf("provide only one of --sku or --id")
		}
		var product model.Product
		var err error
		if *id != "" {
			product, err = client.ProductByID(context.Background(), *id)
		} else {
			product, err = client.ProductBySKU(context.Background(), *sku)
		}
		if err != nil {
			return err
		}
		return printProduct(stdout, g, product)
	case "search":
		if hasHelpArg(args[1:]) {
			wooSearchHelp(stdout)
			return nil
		}
		fs := flag.NewFlagSet("woo search", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		query := fs.String("query", "", "")
		limit := fs.Int("limit", config.DefaultLimit, "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *query == "" {
			return fmt.Errorf("--query is required")
		}
		if err := config.ValidateLimit(*limit); err != nil {
			return err
		}
		products, err := client.Search(context.Background(), *query, *limit)
		if err != nil {
			return err
		}
		return printProducts(stdout, g, products)
	case "repair-images":
		return runWooRepairImages(args[1:], stdout, g, cfg, client, stats, debug)
	case "set-terms":
		if hasHelpArg(args[1:]) {
			wooSetTermsHelp(stdout)
			return nil
		}
		return runWooSetTerms(args[1:], stdout, g, client, stats)
	case "set-status":
		if hasHelpArg(args[1:]) {
			wooSetStatusHelp(stdout)
			return nil
		}
		return runWooSetStatus(args[1:], stdout, g, client, stats)
	default:
		return fmt.Errorf("unknown woo subcommand %q", args[0])
	}
}

type repairImagesReport struct {
	Mode                    string           `json:"mode"`
	Changed                 bool             `json:"changed"`
	WooCommerceBefore       model.Product    `json:"woocommerce_before"`
	WooCommerceAfter        *model.Product   `json:"woocommerce_after,omitempty"`
	Hertwill                model.Product    `json:"hertwill"`
	ImageLimit              int              `json:"image_limit,omitempty"`
	HertwillImageCountTotal int              `json:"hertwill_image_count_total"`
	ImagesToApply           []string         `json:"images_to_apply"`
	ImagesToApplyCount      int              `json:"images_to_apply_count"`
	Warnings                []string         `json:"warnings"`
	RefusedReason           string           `json:"refused_reason,omitempty"`
	RequestSummary          *httpstats.Stats `json:"request_summary,omitempty"`
}

// runWooRepairImages implements `hwd woo repair-images`: a narrowly-scoped,
// explicitly-confirmed write that replaces only a WooCommerce product's
// images with Hertwill's image URLs. It never touches price, stock, status,
// description, or variations, and never publishes a product.
func runWooRepairImages(args []string, stdout io.Writer, g globals, cfg config.Config, wooClient woocommerce.Client, stats *httpstats.Stats, debug func(string)) error {
	if hasHelpArg(args) {
		wooRepairImagesHelp(stdout)
		return nil
	}
	fs := flag.NewFlagSet("woo repair-images", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sku := fs.String("sku", "", "")
	hertwillID := fs.String("hertwill-id", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	force := fs.Bool("force", false, "")
	maxImages := fs.Int("max-images", 0, "")
	featuredOnly := fs.Bool("featured-only", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	maxImagesProvided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "max-images" {
			maxImagesProvided = true
		}
	})
	if *sku == "" {
		return fmt.Errorf("--sku is required")
	}
	if *hertwillID == "" {
		return fmt.Errorf("--hertwill-id is required")
	}
	if *dryRun && *confirm {
		return fmt.Errorf("provide only one of --dry-run or --confirm")
	}
	if !*dryRun && !*confirm {
		return fmt.Errorf("provide either --dry-run or --confirm")
	}
	if maxImagesProvided && *featuredOnly {
		return fmt.Errorf("provide only one of --featured-only or --max-images")
	}
	if maxImagesProvided && *maxImages < 1 {
		return fmt.Errorf("--max-images must be >= 1")
	}

	imageLimit := 0
	switch {
	case *featuredOnly:
		imageLimit = 1
	case maxImagesProvided:
		imageLimit = *maxImages
	}

	mode := "dry-run"
	if *confirm {
		mode = "confirm"
	}
	report := repairImagesReport{Mode: mode, Warnings: []string{}}

	wooValidation := cfg.ValidateWoo()
	if !wooValidation.OK {
		return fmt.Errorf("WooCommerce configuration incomplete: %s", wooValidation.Message)
	}

	hwClient := hertwill.Client{
		BaseURL:     cfg.HertwillBaseURL,
		AccessToken: cfg.HertwillAccessToken,
		Email:       cfg.HertwillEmail,
		Password:    cfg.HertwillPassword,
		Timeout:     cfg.Timeout(),
		Stats:       stats,
		Debug:       debug,
	}

	wooProduct, err := wooClient.ProductBySKU(context.Background(), *sku)
	if err != nil {
		return err
	}
	report.WooCommerceBefore = wooProduct

	hwProduct, err := hwClient.Product(context.Background(), *hertwillID)
	if err != nil {
		return err
	}
	report.Hertwill = hwProduct

	refusedReason, warnings, imageURLs := evaluateImageRepair(wooProduct, hwProduct, *confirm, *force)
	report.Warnings = append(report.Warnings, warnings...)

	imagesToApply := imageURLs
	if imageLimit > 0 && imageLimit < len(imagesToApply) {
		imagesToApply = imagesToApply[:imageLimit]
	}
	report.ImageLimit = imageLimit
	report.HertwillImageCountTotal = imageCountValue(hwProduct)
	report.ImagesToApply = imagesToApply
	report.ImagesToApplyCount = len(imagesToApply)

	if refusedReason != "" {
		report.RefusedReason = refusedReason
		attachRepairRequestInfo(&report, stats)
		if writeErr := writeRepairImagesReport(stdout, g, report); writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("repair refused: %s", refusedReason)
	}

	if *confirm {
		if _, updateErr := wooClient.UpdateProductImages(context.Background(), wooProduct.ID, imagesToApply); updateErr != nil {
			return updateErr
		}
		refetched, refetchErr := wooClient.ProductByID(context.Background(), wooProduct.ID)
		if refetchErr != nil {
			return refetchErr
		}
		report.WooCommerceAfter = &refetched
		report.Changed = imageCountValue(refetched) != imageCountValue(wooProduct)
	}

	attachRepairRequestInfo(&report, stats)
	return writeRepairImagesReport(stdout, g, report)
}

// evaluateImageRepair runs the safety checks that gate this command. It
// returns a non-empty refusedReason when the repair must not proceed, plus
// any non-blocking warnings and the Hertwill image URLs that would be
// applied. Checks that only matter when writing (already-has-images, and
// name-only matching) are gated on confirming/force so dry-run can still
// preview them as warnings.
func evaluateImageRepair(woo, hw model.Product, confirming, force bool) (refusedReason string, warnings []string, imageURLs []string) {
	if !woo.Found {
		return "WooCommerce product not found.", nil, nil
	}
	if strings.TrimSpace(woo.ID) == "" {
		return "WooCommerce product ID is empty.", nil, nil
	}
	if !hertwillProductFound(hw) {
		return "Hertwill product not found.", nil, nil
	}
	imageURLs = hw.ImageURLs
	if len(imageURLs) == 0 {
		return "Hertwill product has no images.", nil, nil
	}

	wooSKU := strings.TrimSpace(woo.SKU)
	hwSKU := strings.TrimSpace(hw.SKU)
	wooName := strings.TrimSpace(woo.Name)
	hwName := strings.TrimSpace(hw.Name)
	nameMatch := wooName != "" && hwName != "" && wooName == hwName

	switch {
	case wooSKU != "" && hwSKU != "":
		if !strings.EqualFold(wooSKU, hwSKU) {
			return "WooCommerce SKU and Hertwill SKU do not match.", nil, imageURLs
		}
		if wooName != "" && hwName != "" && wooName != hwName {
			warnings = append(warnings, "WooCommerce product name and Hertwill product name differ.")
		}
	default:
		if !nameMatch {
			return "Cannot confidently match WooCommerce and Hertwill products; SKU is missing on at least one side and names do not match.", nil, imageURLs
		}
		warnings = append(warnings, "SKU is missing on at least one side; matched by product name only.")
		if confirming && !force {
			return "SKU is missing on at least one side; matched by product name only. Provide --force to confirm this repair.", warnings, imageURLs
		}
	}

	if woo.ImageCount != nil && *woo.ImageCount > 0 {
		warnings = append(warnings, "WooCommerce product already has images; --force will replace the existing WooCommerce featured/gallery images with the selected Hertwill image set.")
		if confirming && !force {
			return "WooCommerce product already has images. Provide --force to overwrite.", warnings, imageURLs
		}
	}

	return "", warnings, imageURLs
}

func hertwillProductFound(p model.Product) bool {
	return strings.TrimSpace(p.ID) != "" || strings.TrimSpace(p.SKU) != "" || strings.TrimSpace(p.Name) != ""
}

func attachRepairRequestInfo(report *repairImagesReport, stats *httpstats.Stats) {
	snapshot := stats.Snapshot()
	report.RequestSummary = &snapshot
}

func writeRepairImagesReport(stdout io.Writer, g globals, report repairImagesReport) error {
	if g.json {
		return writeJSON(stdout, report)
	}
	return printRepairImagesReport(stdout, report)
}

func printRepairImagesReport(w io.Writer, report repairImagesReport) error {
	fmt.Fprintln(w, "WooCommerce image repair")
	fmt.Fprintln(w)

	if report.RefusedReason != "" {
		fmt.Fprintf(w, "Mode: %s\n", report.Mode)
		fmt.Fprintf(w, "Refused: %s\n", report.RefusedReason)
		printRepairWarnings(w, report.Warnings)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "No WooCommerce changes were made.")
		return nil
	}

	if report.Mode == "confirm" {
		before := imageCountValue(report.WooCommerceBefore)
		after := before
		status := report.WooCommerceBefore.Status
		if report.WooCommerceAfter != nil {
			after = imageCountValue(*report.WooCommerceAfter)
			if report.WooCommerceAfter.Status != "" {
				status = report.WooCommerceAfter.Status
			}
		}
		fmt.Fprintln(w, "Mode: confirm")
		fmt.Fprintf(w, "Updated product ID %s images from %d to %d.\n", fallback(report.WooCommerceBefore.ID), before, after)
		fmt.Fprintf(w, "Status remains %s.\n", fallback(status))
		fmt.Fprintln(w, "No price, stock, description, status, or variations were changed.")
		if report.ImageLimit > 0 {
			fmt.Fprintf(w, "Applied the first %d of %d available Hertwill image(s) (--max-images/--featured-only).\n", report.ImagesToApplyCount, report.HertwillImageCountTotal)
		}
		printRepairWarnings(w, report.Warnings)
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Field\tValue")
	fmt.Fprintf(tw, "Mode\t%s\n", report.Mode)
	fmt.Fprintf(tw, "WooCommerce product ID\t%s\n", fallback(report.WooCommerceBefore.ID))
	fmt.Fprintf(tw, "WooCommerce SKU\t%s\n", fallback(report.WooCommerceBefore.SKU))
	fmt.Fprintf(tw, "WooCommerce current images\t%d\n", imageCountValue(report.WooCommerceBefore))
	fmt.Fprintf(tw, "WooCommerce status\t%s\n", fallback(report.WooCommerceBefore.Status))
	fmt.Fprintf(tw, "Hertwill product ID\t%s\n", fallback(report.Hertwill.ID))
	fmt.Fprintf(tw, "Hertwill SKU\t%s\n", fallback(report.Hertwill.SKU))
	fmt.Fprintf(tw, "Hertwill images total\t%d\n", report.HertwillImageCountTotal)
	if report.ImageLimit > 0 {
		fmt.Fprintf(tw, "Image limit\t%d\n", report.ImageLimit)
	}
	fmt.Fprintf(tw, "Images to apply\t%d\n", report.ImagesToApplyCount)
	if err := tw.Flush(); err != nil {
		return err
	}

	if report.ImageLimit > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Note: Only the first %d Hertwill image(s) will be applied.\n", report.ImageLimit)
	}

	if len(report.ImagesToApply) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Image URLs:")
		fmt.Fprintln(w)
		shown := len(report.ImagesToApply)
		if shown > 5 {
			shown = 5
		}
		for i := 0; i < shown; i++ {
			fmt.Fprintf(w, "%d. %s\n", i+1, report.ImagesToApply[i])
		}
		if remaining := len(report.ImagesToApply) - shown; remaining > 0 {
			fmt.Fprintf(w, "... and %d more\n", remaining)
		}
	}
	printRepairWarnings(w, report.Warnings)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Dry run only. No WooCommerce changes were made.")
	return nil
}

func printRepairWarnings(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Warnings:")
	for _, warning := range warnings {
		fmt.Fprintf(w, "* %s\n", warning)
	}
}

func runHertwill(args []string, stdout, _ io.Writer, g globals, cfg config.Config, stats *httpstats.Stats, debug func(string)) error {
	if len(args) == 0 {
		hertwillHelp(stdout)
		return nil
	}
	if args[0] == "help" {
		hertwillHelpFor(args[1:], stdout)
		return nil
	}
	if isHelpArg(args[0]) {
		hertwillHelp(stdout)
		return nil
	}
	client := hertwill.Client{
		BaseURL:     cfg.HertwillBaseURL,
		AccessToken: cfg.HertwillAccessToken,
		Email:       cfg.HertwillEmail,
		Password:    cfg.HertwillPassword,
		Timeout:     cfg.Timeout(),
		Stats:       stats,
		Debug:       debug,
	}
	switch args[0] {
	case "list":
		if hasHelpArg(args[1:]) {
			hertwillListHelp(stdout)
			return nil
		}
		fs := flag.NewFlagSet("hertwill list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		limit := fs.Int("limit", config.DefaultLimit, "")
		contains := fs.String("contains", "", "")
		raw := fs.Bool("raw", false, "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := config.ValidateLimit(*limit); err != nil {
			return err
		}
		if *raw {
			response, err := client.RawListProducts(context.Background())
			if err != nil {
				return err
			}
			return writeRawBody(stdout, response.Body)
		}
		products, metadata, err := client.ListProductsWithMetadata(context.Background(), config.MaxLimit)
		if err != nil {
			return err
		}
		notes := []string{}
		if strings.TrimSpace(*contains) != "" {
			products = filterProductsContains(products, *contains)
			notes = append(notes, "Note: --contains filters products returned by this API response only. It may not search the entire Hertwill catalog if the API is paginated.")
		}
		if len(products) == 0 {
			notes = append(notes, "No parsed products found. Use --raw to inspect the Hertwill response shape.")
		}
		if len(products) > *limit {
			products = products[:*limit]
		}
		note := strings.Join(notes, "\n")
		result := hertwillListResult{
			Products: products,
			Count:    len(products),
			Limit:    *limit,
			Contains: strings.TrimSpace(*contains),
			Note:     note,
			Metadata: metadata,
		}
		snapshot := stats.Snapshot()
		result.RequestSummary = &snapshot
		if g.json {
			return writeJSON(stdout, result)
		}
		if err := printProducts(stdout, g, products); err != nil {
			return err
		}
		if note != "" {
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, note)
		}
		return nil
	case "product":
		if hasHelpArg(args[1:]) {
			hertwillProductHelp(stdout)
			return nil
		}
		fs := flag.NewFlagSet("hertwill product", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "")
		raw := fs.Bool("raw", false, "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("--id is required")
		}
		if *raw {
			response, err := client.RawProduct(context.Background(), *id)
			if err != nil {
				return err
			}
			return writeRawBody(stdout, response.Body)
		}
		product, err := client.Product(context.Background(), *id)
		if err != nil {
			return err
		}
		return printProduct(stdout, g, product)
	case "search":
		if hasHelpArg(args[1:]) {
			hertwillSearchHelp(stdout)
			return nil
		}
		fs := flag.NewFlagSet("hertwill search", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		query := fs.String("query", "", "")
		limit := fs.Int("limit", config.DefaultLimit, "")
		raw := fs.Bool("raw", false, "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *query == "" {
			return fmt.Errorf("--query is required")
		}
		if err := config.ValidateLimit(*limit); err != nil {
			return err
		}
		if *raw {
			response, err := client.RawSearch(context.Background(), *query)
			if err != nil {
				return err
			}
			return writeRawBody(stdout, response.Body)
		}
		products, err := client.Search(context.Background(), *query, *limit)
		if err != nil {
			return err
		}
		if err := printProducts(stdout, g, products); err != nil {
			return err
		}
		if len(products) == 0 && !g.json {
			fmt.Fprintln(stdout, "No parsed products found.")
			fmt.Fprintln(stdout, "Use --raw to inspect the Hertwill response shape.")
		}
		return nil
	case "import-list":
		if hasHelpArg(args[1:]) {
			hertwillImportListHelp(stdout)
			return nil
		}
		return runHertwillImportList(args[1:], stdout, g, client, stats)
	case "import":
		if hasHelpArg(args[1:]) {
			hertwillImportHelp(stdout)
			return nil
		}
		return runHertwillImport(args[1:], stdout, g, client, stats)
	case "sync":
		if hasHelpArg(args[1:]) {
			hertwillSyncHelp(stdout)
			return nil
		}
		return runHertwillSync(args[1:], stdout, g, client, stats)
	case "sync-status":
		if hasHelpArg(args[1:]) {
			hertwillSyncStatusHelp(stdout)
			return nil
		}
		fs := flag.NewFlagSet("hertwill sync-status", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("--id is required")
		}
		status, err := client.SyncStatus(context.Background(), *id)
		if err != nil {
			return err
		}
		if g.json {
			return writeJSON(stdout, status)
		}
		return printMap(stdout, status)
	default:
		return fmt.Errorf("unknown hertwill subcommand %q", args[0])
	}
}

func runCompare(args []string, stdout, _ io.Writer, g globals, cfg config.Config, stats *httpstats.Stats, debug func(string)) error {
	if hasHelpArg(args) {
		compareHelp(stdout)
		return nil
	}
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sku := fs.String("sku", "", "")
	hertwillID := fs.String("hertwill-id", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sku == "" || *hertwillID == "" {
		return fmt.Errorf("--sku and --hertwill-id are required")
	}
	wooClient := woocommerce.Client{BaseURL: cfg.WooBaseURL, ConsumerKey: cfg.WooConsumerKey, ConsumerSecret: cfg.WooConsumerSecret, Timeout: cfg.Timeout(), Stats: stats, Debug: debug}
	hwClient := hertwill.Client{BaseURL: cfg.HertwillBaseURL, AccessToken: cfg.HertwillAccessToken, Email: cfg.HertwillEmail, Password: cfg.HertwillPassword, Timeout: cfg.Timeout(), Stats: stats, Debug: debug}
	wooProduct, err := wooClient.ProductBySKU(context.Background(), *sku)
	if err != nil {
		return err
	}
	hwProduct, err := hwClient.Product(context.Background(), *hertwillID)
	if err != nil {
		return err
	}
	rows := compare.Products(wooProduct, hwProduct)
	if g.json {
		return writeJSON(stdout, rows)
	}
	return printCompare(stdout, rows)
}

func runDiagnose(args []string, stdout, stderr io.Writer, g globals, cfg config.Config, stats *httpstats.Stats, debug func(string)) error {
	if hasHelpArg(args) {
		diagnoseHelp(stdout)
		return nil
	}
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sku := fs.String("sku", "", "")
	hertwillID := fs.String("hertwill-id", "", "")
	noHertwillSearch := fs.Bool("no-hertwill-search", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sku == "" {
		return fmt.Errorf("--sku is required")
	}
	wooClient := woocommerce.Client{
		BaseURL:        cfg.WooBaseURL,
		ConsumerKey:    cfg.WooConsumerKey,
		ConsumerSecret: cfg.WooConsumerSecret,
		Timeout:        cfg.Timeout(),
		Stats:          stats,
		Debug:          debug,
	}
	wooProduct, err := wooClient.ProductBySKU(context.Background(), *sku)
	if err != nil {
		report := buildFailedWooDiagnoseReport(*sku, *hertwillID)
		if *hertwillID == "" && !*noHertwillSearch {
			report.Warnings = removeWarning(report.Warnings, "Hertwill lookup skipped because no Hertwill ID was provided.")
			report = runAutomaticHertwillDiagnoseSearch(report, cfg, *sku, stats, debug)
		} else if *noHertwillSearch {
			report.Warnings = removeWarning(report.Warnings, "Hertwill lookup skipped because no Hertwill ID was provided.")
			report.Hertwill = diagnoseHertwillResult{Status: "skipped", Message: "automatic Hertwill search disabled"}
			report.Warnings = append(report.Warnings, "Hertwill lookup skipped because --no-hertwill-search was provided.")
		}
		attachRequestInfo(&report, stats)
		if g.json {
			return writeJSON(stdout, report)
		}
		return printDiagnoseReport(stdout, report)
	}

	report := buildDiagnoseReport(wooProduct, *hertwillID)
	if *hertwillID != "" {
		hwClient := hertwill.Client{
			BaseURL:     cfg.HertwillBaseURL,
			AccessToken: cfg.HertwillAccessToken,
			Email:       cfg.HertwillEmail,
			Password:    cfg.HertwillPassword,
			Timeout:     cfg.Timeout(),
			Stats:       stats,
			Debug:       debug,
		}
		hwProduct, productErr := hwClient.Product(context.Background(), *hertwillID)
		if productErr != nil {
			report.Hertwill = diagnoseHertwillResult{
				Status:  "failed",
				Message: "Hertwill product lookup failed; endpoint may need verification or credentials may be invalid.",
			}
			report.Warnings = append(report.Warnings, "Hertwill product lookup failed; no Hertwill status was inferred.")
		} else {
			report.Hertwill = diagnoseHertwillResult{
				Status:  "found",
				Message: "product fetched",
				Product: &hwProduct,
			}
		}
		if hertwill.SyncStatusEndpointVerified() {
			status, statusErr := hwClient.SyncStatus(context.Background(), *hertwillID)
			if statusErr != nil {
				report.Warnings = append(report.Warnings, "Hertwill sync status lookup failed; endpoint may need verification.")
			} else {
				report.Hertwill.SyncStatus = status
			}
		}
		report = finalizeDiagnoseReport(report)
	} else if *noHertwillSearch {
		report.Hertwill = diagnoseHertwillResult{Status: "skipped", Message: "automatic Hertwill search disabled"}
		report.Warnings = append(report.Warnings, "Hertwill lookup skipped because --no-hertwill-search was provided.")
		report = finalizeDiagnoseReport(report)
	} else {
		report = runAutomaticHertwillDiagnoseSearch(report, cfg, *sku, stats, debug)
	}
	attachRequestInfo(&report, stats)
	if g.json {
		return writeJSON(stdout, report)
	}
	return printDiagnoseReport(stdout, report)
}

func attachRequestInfo(report *diagnoseReport, stats *httpstats.Stats) {
	snapshot := stats.Snapshot()
	report.RequestSummary = &snapshot
}

func buildFailedWooDiagnoseReport(sku, hertwillID string) diagnoseReport {
	report := diagnoseReport{
		WooCommerce: model.Product{Found: false, SKU: sku},
		Warnings: []string{
			"WooCommerce product lookup failed; run with --debug for sanitized request details.",
		},
		LikelyCause: "WooCommerce API request failed, so product health could not be confirmed.",
		SuggestedNextAction: []string{
			"Check WooCommerce credentials, store URL, and network connectivity.",
			"Re-run with --debug for sanitized request details.",
		},
	}
	if hertwillID == "" {
		report.Hertwill = diagnoseHertwillResult{Status: "skipped", Message: "no --hertwill-id provided"}
		report.Warnings = append(report.Warnings, "Hertwill lookup skipped because no Hertwill ID was provided.")
		return report
	}
	report.Hertwill = diagnoseHertwillResult{Status: "skipped", Message: "WooCommerce lookup failed first"}
	return report
}

type diagnoseHertwillResult struct {
	Status     string           `json:"status"`
	Message    string           `json:"message"`
	MatchedBy  string           `json:"matched_by,omitempty"`
	Searches   []diagnoseSearch `json:"searches,omitempty"`
	Product    *model.Product   `json:"product,omitempty"`
	Candidates []model.Product  `json:"candidates,omitempty"`
	SyncStatus map[string]any   `json:"sync_status,omitempty"`
}

type diagnoseSearch struct {
	Query       string `json:"query"`
	Type        string `json:"type"`
	ResultCount int    `json:"result_count"`
}

type diagnoseReport struct {
	WooCommerce         model.Product          `json:"woocommerce"`
	Hertwill            diagnoseHertwillResult `json:"hertwill"`
	Warnings            []string               `json:"warnings"`
	LikelyCause         string                 `json:"likely_cause"`
	SuggestedNextAction []string               `json:"suggested_next_actions"`
	RequestSummary      *httpstats.Stats       `json:"request_summary,omitempty"`
}

func runAutomaticHertwillDiagnoseSearch(report diagnoseReport, cfg config.Config, sku string, stats *httpstats.Stats, debug func(string)) diagnoseReport {
	if !cfg.ValidateHertwill().OK {
		report.Hertwill = diagnoseHertwillResult{Status: "skipped", Message: "Hertwill configuration missing"}
		report.Warnings = append(report.Warnings, "Hertwill lookup skipped because configuration missing.")
		return finalizeDiagnoseReport(report)
	}
	if !hertwill.ProductSearchEndpointVerified() {
		report.Hertwill = diagnoseHertwillResult{Status: "skipped", Message: "Hertwill search endpoint not verified"}
		report.Warnings = append(report.Warnings, "Hertwill lookup skipped because search endpoint is not verified.")
		return finalizeDiagnoseReport(report)
	}
	client := hertwill.Client{
		BaseURL:     cfg.HertwillBaseURL,
		AccessToken: cfg.HertwillAccessToken,
		Email:       cfg.HertwillEmail,
		Password:    cfg.HertwillPassword,
		Timeout:     cfg.Timeout(),
		Stats:       stats,
		Debug:       debug,
	}
	products, err := client.Search(context.Background(), sku, config.MaxLimit)
	searches := []diagnoseSearch{{Query: sku, Type: "sku", ResultCount: len(products)}}
	if err != nil {
		message := "Hertwill lookup failed; run with --debug for sanitized details."
		if isAuthFailure(err) {
			report.Hertwill = diagnoseHertwillResult{Status: "failed", Message: "Hertwill authentication failed or token invalid", Searches: searches}
			report.Warnings = append(report.Warnings, "Hertwill authentication failed or token invalid.")
		} else {
			report.Hertwill = diagnoseHertwillResult{Status: "failed", Message: message, Searches: searches}
			report.Warnings = append(report.Warnings, message)
		}
		return finalizeDiagnoseReport(report)
	}
	strong := strongHertwillMatches(products, sku)
	switch {
	case len(strong) == 1:
		report.Hertwill = diagnoseHertwillResult{Status: "found", Message: "exact SKU match found", MatchedBy: "sku", Searches: searches, Product: &strong[0]}
	case len(strong) > 1:
		report.Hertwill = diagnoseHertwillResult{Status: "ambiguous", Message: "multiple exact SKU matches found", Searches: searches, Candidates: topCandidates(strong, 5)}
		report.Warnings = append(report.Warnings, "Hertwill search returned multiple possible matches.")
	default:
		if strings.TrimSpace(report.WooCommerce.Name) != "" {
			titleProducts, titleErr := client.Search(context.Background(), report.WooCommerce.Name, config.MaxLimit)
			searches = append(searches, diagnoseSearch{Query: report.WooCommerce.Name, Type: "title", ResultCount: len(titleProducts)})
			if titleErr != nil {
				report.Hertwill = diagnoseHertwillResult{Status: "failed", Message: "Hertwill title lookup failed; run with --debug for sanitized details.", Searches: searches}
				report.Warnings = append(report.Warnings, "Hertwill title lookup failed; run with --debug for sanitized details.")
			} else if len(titleProducts) > 0 {
				report.Hertwill = diagnoseHertwillResult{Status: "ambiguous", Message: "no exact SKU match found, but title search returned possible candidates", Searches: searches, Candidates: topCandidates(titleProducts, 5)}
				report.Warnings = append(report.Warnings, "Hertwill product not found for SKU.")
				report.Warnings = append(report.Warnings, "Hertwill search returned multiple possible matches.")
			} else {
				report.Hertwill = diagnoseHertwillResult{Status: "not_found", Message: "no exact SKU or title match found", Searches: searches}
				report.Warnings = append(report.Warnings, "Hertwill product not found for SKU.")
				report.Warnings = append(report.Warnings, "Hertwill title search returned no candidates.")
			}
		} else {
			report.Hertwill = diagnoseHertwillResult{Status: "not_found", Message: "no exact SKU match found", Searches: searches}
			report.Warnings = append(report.Warnings, "Hertwill product not found for SKU.")
		}
	}
	return finalizeDiagnoseReport(report)
}

func buildDiagnoseReport(wooProduct model.Product, hertwillID string) diagnoseReport {
	report := diagnoseReport{
		WooCommerce: wooProduct,
		Warnings:    []string{},
	}
	if hertwillID == "" {
		report.Hertwill = diagnoseHertwillResult{Status: "skipped", Message: "no --hertwill-id provided"}
		if wooProduct.Found {
			report.Warnings = append(report.Warnings, wooCommerceProductWarnings(wooProduct)...)
			if len(report.Warnings) == 0 {
				report.LikelyCause = "Product appears healthy in WooCommerce."
				report.SuggestedNextAction = []string{"Re-run with --hertwill-id <ID> to include Hertwill status, if needed."}
				return report
			}
			applyIncompleteWooCommerceConclusion(&report, hertwillID)
			return report
		}
		report.Warnings = append(report.Warnings,
			"Product not found in WooCommerce.",
			"Hertwill lookup skipped because no Hertwill ID was provided.",
		)
		report.LikelyCause = "WooCommerce product is missing. Check whether Hertwill has actually synced this product to the store."
		report.SuggestedNextAction = []string{
			"Re-run with --hertwill-id <ID> if you know the Hertwill product ID.",
			"Check WooCommerce logs or Hertwill import/sync status.",
		}
		return report
	}
	report.Hertwill = diagnoseHertwillResult{Status: "pending", Message: "lookup requested"}
	if wooProduct.Found {
		report.Warnings = append(report.Warnings, wooCommerceProductWarnings(wooProduct)...)
	}
	return finalizeDiagnoseReport(report)
}

func finalizeDiagnoseReport(report diagnoseReport) diagnoseReport {
	if report.Warnings == nil {
		report.Warnings = []string{}
	}
	if !report.WooCommerce.Found && report.Hertwill.Status == "found" {
		report.LikelyCause = "Hertwill product exists, but WooCommerce product is missing. Check whether sync was started or completed."
		report.SuggestedNextAction = []string{
			"Check whether the product was added to the Hertwill import list and sync was completed.",
			"Check WooCommerce logs or Hertwill import/sync status.",
		}
		return report
	}
	if !report.WooCommerce.Found && (report.Hertwill.Status == "not_found" || report.Hertwill.Status == "ambiguous") {
		report.LikelyCause = "Neither WooCommerce nor Hertwill lookup found a clear product match."
		report.SuggestedNextAction = []string{
			"Verify the SKU.",
			"Run hwd hertwill search --query <SKU> --debug to inspect Hertwill-side candidates.",
			"Run hwd hertwill list --limit 100 --contains \"Moomin\" to inspect visible Hertwill products returned by /v1/products.",
		}
		return report
	}
	if wooImagesZero(report.WooCommerce) && report.Hertwill.Product != nil && imageCountValue(*report.Hertwill.Product) > 0 {
		if !containsWarning(report.Warnings, "Hertwill product has images, but WooCommerce has none.") {
			report.Warnings = append(report.Warnings, "Hertwill product has images, but WooCommerce has none.")
		}
		report.LikelyCause = "Hertwill source product has images, but WooCommerce product has none. The media/image portion of the WooCommerce sync likely failed."
		report.SuggestedNextAction = []string{
			"Open WooCommerce product ID " + fallback(report.WooCommerce.ID) + " and check media/gallery.",
			"Contact Hertwill support with Hertwill product ID " + fallback(report.Hertwill.Product.ID) + ", WooCommerce product ID " + fallback(report.WooCommerce.ID) + ", SKU " + fallback(report.WooCommerce.SKU) + ", WooCommerce status " + fallback(report.WooCommerce.Status) + ", WooCommerce image count 0, and Hertwill image count " + fmt.Sprintf("%d", imageCountValue(*report.Hertwill.Product)) + ".",
			"Do not publish until images are present.",
		}
		return report
	}
	if report.WooCommerce.Found && hasWooCommerceProductWarning(report.Warnings) {
		hertwillID := ""
		if report.Hertwill.Status == "found" || report.Hertwill.Status == "failed" {
			hertwillID = "provided"
		}
		applyIncompleteWooCommerceConclusion(&report, hertwillID)
		return report
	}
	if report.WooCommerce.Found && report.Hertwill.Status == "found" {
		report.LikelyCause = "Product was found in WooCommerce and Hertwill product data was fetched."
		report.SuggestedNextAction = []string{"Review field differences with hwd compare --sku <SKU> --hertwill-id <ID> if needed."}
		return report
	}
	if report.WooCommerce.Found {
		report.LikelyCause = "Product appears healthy in WooCommerce, but Hertwill status could not be confirmed."
		report.SuggestedNextAction = []string{"Run hwd hertwill search --query <SKU> --debug to inspect Hertwill-side candidates."}
		return report
	}
	report.LikelyCause = "WooCommerce product is missing. Check whether Hertwill has actually synced this product to the store."
	report.SuggestedNextAction = []string{
		"Check WooCommerce logs or Hertwill import/sync status.",
		"Verify the SKU and Hertwill product ID.",
	}
	return report
}

func wooImagesZero(product model.Product) bool {
	return product.Found && product.ImageCount != nil && *product.ImageCount == 0
}

func imageCountValue(product model.Product) int {
	if product.ImageCount == nil {
		return 0
	}
	return *product.ImageCount
}

func strongHertwillMatches(products []model.Product, query string) []model.Product {
	normalized := strings.ToLower(strings.TrimSpace(query))
	var matches []model.Product
	for _, product := range products {
		if strings.ToLower(strings.TrimSpace(product.SKU)) == normalized {
			matches = append(matches, product)
			continue
		}
		if strings.TrimSpace(product.SKU) == "" && strings.ToLower(strings.TrimSpace(product.ID)) == normalized {
			matches = append(matches, product)
		}
	}
	return matches
}

func topCandidates(products []model.Product, limit int) []model.Product {
	if limit <= 0 || len(products) <= limit {
		return products
	}
	return products[:limit]
}

func filterProductsContains(products []model.Product, text string) []model.Product {
	needle := strings.ToLower(strings.TrimSpace(text))
	if needle == "" {
		return products
	}
	var filtered []model.Product
	for _, product := range products {
		haystack := strings.ToLower(strings.Join([]string{
			product.ID,
			product.Name,
			product.SKU,
			product.Status,
			product.Stock,
		}, " "))
		if strings.Contains(haystack, needle) {
			filtered = append(filtered, product)
		}
	}
	return filtered
}

func isAuthFailure(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "http 401") || strings.Contains(message, "http 403") || strings.Contains(message, "authentication")
}

func wooCommerceProductWarnings(product model.Product) []string {
	var warnings []string
	if !product.Found {
		return []string{"Product not found in WooCommerce."}
	}
	status := strings.ToLower(strings.TrimSpace(product.Status))
	switch status {
	case "":
		warnings = append(warnings, "Product status is empty.")
	case "publish":
	case "private":
		warnings = append(warnings, "Product exists but is private.")
	case "draft":
		warnings = append(warnings, "Product exists but is draft.")
	default:
		warnings = append(warnings, "Product status is not publish.")
	}
	if product.ImageCount != nil && *product.ImageCount == 0 {
		warnings = append(warnings, "Product has no images.")
	}
	if strings.TrimSpace(product.Price) == "" {
		warnings = append(warnings, "Product price is empty.")
	}
	stock := strings.ToLower(strings.TrimSpace(product.Stock))
	if stock == "" {
		warnings = append(warnings, "Product stock is empty.")
	} else if stock == "outofstock" {
		warnings = append(warnings, "Product is out of stock.")
	}
	if strings.TrimSpace(product.SKU) == "" {
		warnings = append(warnings, "Product SKU is empty.")
	}
	return warnings
}

func hasWooCommerceProductWarning(warnings []string) bool {
	for _, warning := range warnings {
		switch warning {
		case "Product exists but is private.",
			"Product exists but is draft.",
			"Product status is not publish.",
			"Product status is empty.",
			"Product has no images.",
			"Product price is empty.",
			"Product stock is empty.",
			"Product is out of stock.",
			"Product SKU is empty.",
			"Product not found in WooCommerce.":
			return true
		}
	}
	return false
}

func applyIncompleteWooCommerceConclusion(report *diagnoseReport, hertwillID string) {
	hasNoImages := containsWarning(report.Warnings, "Product has no images.")
	isPrivateOnly := containsWarning(report.Warnings, "Product exists but is private.") && len(report.Warnings) == 1
	switch {
	case hasNoImages:
		report.LikelyCause = "Product exists in WooCommerce but appears incomplete. The image/media portion of the sync may have failed."
	case isPrivateOnly:
		report.LikelyCause = "Product exists in WooCommerce but is not publicly visible yet. Hertwill imports may be private by default."
	default:
		report.LikelyCause = "Product exists in WooCommerce but appears incomplete or not publicly visible."
	}
	var actions []string
	if report.WooCommerce.ID != "" {
		actions = append(actions, "Open WooCommerce product ID "+report.WooCommerce.ID+" and confirm product status/visibility.")
	} else {
		actions = append(actions, "Open the WooCommerce product and confirm product status/visibility.")
	}
	if hasNoImages {
		actions = append(actions, "Check whether product images failed during Hertwill import/sync.")
	}
	if containsWarning(report.Warnings, "Product price is empty.") {
		actions = append(actions, "Check whether product pricing failed during import/sync.")
	}
	if containsWarning(report.Warnings, "Product stock is empty.") || containsWarning(report.Warnings, "Product is out of stock.") {
		actions = append(actions, "Check WooCommerce stock status and inventory settings.")
	}
	if containsWarning(report.Warnings, "Product SKU is empty.") {
		actions = append(actions, "Check whether the SKU failed during import/sync.")
	}
	if hertwillID == "" {
		if report.Hertwill.Status == "ambiguous" && len(report.Hertwill.Candidates) > 0 {
			actions = append(actions, "Review Hertwill candidates and confirm whether one matches WooCommerce product ID/SKU.")
			actions = append(actions, "If the Hertwill candidate is correct, use its Hertwill ID with --hertwill-id <ID> once product-by-ID/sync-status endpoints are verified.")
		}
		actions = append(actions, "Run hwd hertwill search --query "+fallback(report.WooCommerce.SKU)+" --debug to inspect Hertwill-side candidates.")
		actions = append(actions, "Run hwd hertwill list --limit 100 --contains \"Moomin\" to inspect visible Hertwill products returned by /v1/products.")
		actions = append(actions, "If Hertwill confirms the product and sync looks stuck, contact Hertwill support with WooCommerce product ID, SKU, status, and image count.")
		if report.Hertwill.Status == "ambiguous" && len(report.Hertwill.Candidates) > 0 {
			actions = append(actions, "Contact Hertwill support with WooCommerce product ID, WooCommerce SKU, product name, status, image count, and Hertwill candidate IDs.")
		}
	}
	report.SuggestedNextAction = actions
}

func containsWarning(warnings []string, target string) bool {
	for _, warning := range warnings {
		if warning == target {
			return true
		}
	}
	return false
}

func removeWarning(warnings []string, target string) []string {
	filtered := warnings[:0]
	for _, warning := range warnings {
		if warning != target {
			filtered = append(filtered, warning)
		}
	}
	return filtered
}

func printProduct(w io.Writer, g globals, product model.Product) error {
	if g.json {
		return writeJSON(w, product)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Field\tValue")
	fmt.Fprintf(tw, "Found\t%s\n", yesNo(product.Found))
	fmt.Fprintf(tw, "ID\t%s\n", fallback(product.ID))
	fmt.Fprintf(tw, "Name\t%s\n", fallback(product.Name))
	fmt.Fprintf(tw, "SKU\t%s\n", fallback(product.SKU))
	fmt.Fprintf(tw, "Price\t%s\n", fallback(product.Price))
	fmt.Fprintf(tw, "Stock\t%s\n", fallback(product.Stock))
	if product.ImageCount == nil {
		fmt.Fprintln(tw, "Images\tn/a")
	} else {
		fmt.Fprintf(tw, "Images\t%d\n", *product.ImageCount)
	}
	fmt.Fprintf(tw, "Status\t%s\n", fallback(product.Status))
	fmt.Fprintf(tw, "Brand\t%s\n", fallback(product.Brand))
	fmt.Fprintf(tw, "Category\t%s\n", fallback(product.Category))
	if len(product.Categories) > 0 {
		fmt.Fprintf(tw, "Categories\t%s\n", strings.Join(product.Categories, ", "))
	}
	fmt.Fprintf(tw, "Slug\t%s\n", fallback(product.Slug))
	fmt.Fprintf(tw, "Variations\t%d\n", len(product.Variations))
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(product.Variations) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Variations:")
		return printVariations(w, product.Variations)
	}
	return nil
}

func printVariations(w io.Writer, variations []model.Variation) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSKU\tPrice\tStock\tImages\tAttributes")
	for _, variation := range variations {
		imageCount := "n/a"
		if variation.ImageCount != nil {
			imageCount = fmt.Sprintf("%d", *variation.ImageCount)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			fallback(variation.ID),
			fallback(variation.SKU),
			fallback(variation.Price),
			fallback(variation.Stock),
			imageCount,
			formatAttributes(variation.Attributes),
		)
	}
	return tw.Flush()
}

func formatAttributes(attrs map[string]string) string {
	if len(attrs) == 0 {
		return "n/a"
	}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+attrs[key])
	}
	return strings.Join(parts, ", ")
}

func printDiagnoseReport(w io.Writer, report diagnoseReport) error {
	fmt.Fprintln(w, "Hertwill Doctor Diagnosis")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "WooCommerce result:")
	fmt.Fprintln(w)
	if err := printProduct(w, globals{}, report.WooCommerce); err != nil {
		return err
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Hertwill result:")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "* %s - %s\n", report.Hertwill.Status, report.Hertwill.Message)
	if report.Hertwill.Product != nil {
		fmt.Fprintln(w)
		if err := printProduct(w, globals{}, *report.Hertwill.Product); err != nil {
			return err
		}
	}
	if len(report.Hertwill.Candidates) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Candidates:")
		if err := printProducts(w, globals{}, report.Hertwill.Candidates); err != nil {
			return err
		}
	}
	if report.Hertwill.SyncStatus != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Hertwill sync status:")
		if err := printMap(w, report.Hertwill.SyncStatus); err != nil {
			return err
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Warnings:")
	if len(report.Warnings) == 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "* none")
	} else {
		fmt.Fprintln(w)
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "* %s\n", warning)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Likely cause:")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "* %s\n", report.LikelyCause)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Suggested next action:")
	fmt.Fprintln(w)
	for _, action := range report.SuggestedNextAction {
		fmt.Fprintf(w, "* %s\n", action)
	}
	return nil
}

func printProducts(w io.Writer, g globals, products []model.Product) error {
	if g.json {
		return writeJSON(w, products)
	}
	if len(products) == 0 {
		fmt.Fprintln(w, "No results")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tName\tSKU\tPrice\tStock\tImages\tStatus")
	for _, product := range products {
		imageCount := "n/a"
		if product.ImageCount != nil {
			imageCount = fmt.Sprintf("%d", *product.ImageCount)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", fallback(product.ID), fallback(product.Name), fallback(product.SKU), fallback(product.Price), fallback(product.Stock), imageCount, fallback(product.Status))
	}
	return tw.Flush()
}

func printCompare(w io.Writer, rows []compare.Row) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Field\tWooCommerce\tHertwill\tMatch")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Field, row.WooCommerce, row.Hertwill, row.Match)
	}
	return tw.Flush()
}

func printMap(w io.Writer, values map[string]any) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Field\tValue")
	for key, value := range values {
		fmt.Fprintf(tw, "%s\t%v\n", key, value)
	}
	return tw.Flush()
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeRawBody(w io.Writer, body []byte) error {
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		_, err := pretty.WriteTo(w)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w)
		return err
	}
	_, err := w.Write(body)
	if err != nil {
		return err
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, err = fmt.Fprintln(w)
	}
	return err
}

func printRequestSummary(w io.Writer, stats httpstats.Stats) {
	fmt.Fprintln(w, "Request summary:")
	fmt.Fprintln(w, "This command:")
	fmt.Fprintf(w, "Hertwill API attempts: %d\n", stats.HertwillAttempts)
	fmt.Fprintf(w, "Hertwill API failures: %d\n", stats.HertwillFailures)
	fmt.Fprintf(w, "WooCommerce REST attempts: %d\n", stats.WooAttempts)
	fmt.Fprintf(w, "WooCommerce REST failures: %d\n", stats.WooFailures)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Configured limits:")
	fmt.Fprintln(w, "Hertwill public endpoints: 60 requests/minute per IP")
	fmt.Fprintln(w, "Hertwill authenticated endpoints: 300 requests/minute per API key")
	fmt.Fprintln(w, "WooCommerce REST: store/server dependent")
	if len(stats.RateLimitInfo) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Hertwill rate-limit headers:")
		for _, key := range []string{"RateLimit", "RateLimit-Policy"} {
			if value := stats.RateLimitInfo[key]; value != "" {
				fmt.Fprintf(w, "%s: %s\n", key, value)
			}
		}
	}
}

func statusLine(v config.Validation) string {
	status := "missing"
	if v.OK {
		status = "ok"
	}
	return status + " - " + v.Message
}

func fallback(value string) string {
	if strings.TrimSpace(value) == "" {
		return "n/a"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func help(args []string, w io.Writer) {
	if len(args) == 0 {
		rootHelp(w)
		return
	}
	switch args[0] {
	case "version":
		versionHelp(w)
	case "doctor":
		doctorHelp(w)
	case "woo":
		wooHelpFor(args[1:], w)
	case "hertwill":
		hertwillHelpFor(args[1:], w)
	case "diagnose":
		diagnoseHelp(w)
	case "compare":
		compareHelp(w)
	default:
		rootHelp(w)
	}
}

func wooHelpFor(args []string, w io.Writer) {
	if len(args) == 0 {
		wooHelp(w)
		return
	}
	switch args[0] {
	case "product":
		wooProductHelp(w)
	case "search":
		wooSearchHelp(w)
	case "repair-images":
		wooRepairImagesHelp(w)
	case "set-terms":
		wooSetTermsHelp(w)
	case "set-status":
		wooSetStatusHelp(w)
	default:
		wooHelp(w)
	}
}

func hertwillHelpFor(args []string, w io.Writer) {
	if len(args) == 0 {
		hertwillHelp(w)
		return
	}
	switch args[0] {
	case "product":
		hertwillProductHelp(w)
	case "search":
		hertwillSearchHelp(w)
	case "sync-status":
		hertwillSyncStatusHelp(w)
	case "list":
		hertwillListHelp(w)
	case "import-list":
		hertwillImportListHelp(w)
	case "import":
		hertwillImportHelp(w)
	case "sync":
		hertwillSyncHelp(w)
	default:
		hertwillHelp(w)
	}
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if isHelpArg(arg) {
			return true
		}
	}
	return false
}

func rootHelp(w io.Writer) {
	fmt.Fprintln(w, "Hertwill Doctor")
	fmt.Fprintln(w, "Usage: hwd [--json] [--debug] [--no-color] <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  version    Print build version information")
	fmt.Fprintln(w, "  doctor     Validate local configuration")
	fmt.Fprintln(w, "  woo        WooCommerce diagnostics")
	fmt.Fprintln(w, "  hertwill   Hertwill API diagnostics")
	fmt.Fprintln(w, "  diagnose   Diagnose a SKU")
	fmt.Fprintln(w, "  compare    Compare WooCommerce and Hertwill product data")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Use \"hwd help <command>\" for command-specific help.")
}

func versionHelp(w io.Writer) {
	fmt.Fprintln(w, "Print build version information.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd version [--json]")
}

func doctorHelp(w io.Writer) {
	fmt.Fprintln(w, "Validate local configuration.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd doctor [--json]")
}

func wooHelp(w io.Writer) {
	fmt.Fprintln(w, "WooCommerce diagnostics.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  product        Look up a WooCommerce product by SKU or ID")
	fmt.Fprintln(w, "  search         Search WooCommerce products")
	fmt.Fprintln(w, "  repair-images  Repair WooCommerce product images from Hertwill (write operation)")
	fmt.Fprintln(w, "  set-terms      Replace a product's categories and/or brands (write operation)")
	fmt.Fprintln(w, "  set-status     Change a product's status, e.g. publish (write operation)")
}

func wooProductHelp(w io.Writer) {
	fmt.Fprintln(w, "Look up a WooCommerce product by SKU or ID.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo product (--sku <SKU> | --id <ID>) [--json]")
}

func wooSearchHelp(w io.Writer) {
	fmt.Fprintln(w, "Search WooCommerce products.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo search --query <TEXT> [--limit <N>] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --limit int   Maximum results to return, default 10, max 100")
}

func wooRepairImagesHelp(w io.Writer) {
	fmt.Fprintln(w, "Repair WooCommerce product images using Hertwill image URLs.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo repair-images --sku <SKU> --hertwill-id <ID> (--dry-run | --confirm)")
	fmt.Fprintln(w, "       [--max-images <N> | --featured-only] [--force] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This command only ever replaces the WooCommerce product's images field with")
	fmt.Fprintln(w, "Hertwill's image URLs. It never changes price, stock, status, description, or")
	fmt.Fprintln(w, "variations, and never publishes a product.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exactly one of --dry-run or --confirm is required. --dry-run previews the repair")
	fmt.Fprintln(w, "and makes no WooCommerce changes. --confirm performs a WooCommerce write (PUT")
	fmt.Fprintln(w, "/wp-json/wc/v3/products/{id} with only an images field).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --max-images int   Only apply the first N deduplicated Hertwill image URLs.")
	fmt.Fprintln(w, "                     Must be >= 1. Mutually exclusive with --featured-only.")
	fmt.Fprintln(w, "  --featured-only    Shorthand for --max-images 1.")
	fmt.Fprintln(w, "  --force            Allow --confirm to overwrite existing WooCommerce images, or")
	fmt.Fprintln(w, "                     to match products by name only when a SKU is missing on one")
	fmt.Fprintln(w, "                     side. --force replaces the existing WooCommerce")
	fmt.Fprintln(w, "                     featured/gallery images with the selected Hertwill image set.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The command refuses to run (and never writes) if the WooCommerce or Hertwill")
	fmt.Fprintln(w, "product cannot be found, the Hertwill product has no images, or the WooCommerce")
	fmt.Fprintln(w, "and Hertwill SKUs are both present but do not match.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Timeout guidance: WooCommerce/WordPress sideloads each remote image during the")
	fmt.Fprintln(w, "PUT request, so applying many images at once can exceed hosting/PHP/API request")
	fmt.Fprintln(w, "timeouts, leaving the update incomplete. If a --confirm run times out, retry with")
	fmt.Fprintln(w, "a smaller batch (--max-images 1 or --featured-only), or raise the client timeout")
	fmt.Fprintln(w, "with HWD_TIMEOUT_SECONDS=120. The client timeout is never increased automatically.")
}

func hertwillHelp(w io.Writer) {
	fmt.Fprintln(w, "Hertwill API diagnostics, import list and sync.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  list         List Hertwill products")
	fmt.Fprintln(w, "  product      Look up a Hertwill product by ID")
	fmt.Fprintln(w, "  search       Search Hertwill products")
	fmt.Fprintln(w, "  import-list  List the store's import list with variant counts")
	fmt.Fprintln(w, "  import       Add products to the import list (--dry-run | --confirm)")
	fmt.Fprintln(w, "  sync         Sync import-list products at a selling price (--dry-run | --confirm)")
	fmt.Fprintln(w, "  sync-status  Show Hertwill sync status for a product")
}

func hertwillListHelp(w io.Writer) {
	fmt.Fprintln(w, "List Hertwill products.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill list [--limit <N>] [--contains <TEXT>] [--raw] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --limit int       Maximum results to print, default 10, max 100")
	fmt.Fprintln(w, "  --contains text   Local case-insensitive filter over returned products")
	fmt.Fprintln(w, "  --raw             Print raw Hertwill JSON response")
}

func hertwillProductHelp(w io.Writer) {
	fmt.Fprintln(w, "Look up a Hertwill product by ID.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill product --id <ID> [--raw] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --raw   Print raw Hertwill JSON response")
}

func hertwillSearchHelp(w io.Writer) {
	fmt.Fprintln(w, "Search Hertwill products.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill search --query <TEXT> [--limit <N>] [--raw] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --limit int   Maximum results to return, default 10, max 100")
	fmt.Fprintln(w, "  --raw         Print raw Hertwill JSON response")
}

func hertwillSyncStatusHelp(w io.Writer) {
	fmt.Fprintln(w, "Show Hertwill sync status for a product.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill sync-status --id <ID> [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The sync-status endpoint is not verified yet. This command does not call it;")
	fmt.Fprintln(w, "it exits with an error stating the endpoint is unverified, and hwd diagnose")
	fmt.Fprintln(w, "does not call it automatically either.")
}

func diagnoseHelp(w io.Writer) {
	fmt.Fprintln(w, "Diagnose a SKU.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd diagnose --sku <SKU> [--hertwill-id <ID>] [--json]")
}

func compareHelp(w io.Writer) {
	fmt.Fprintln(w, "Compare WooCommerce and Hertwill product data.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd compare --sku <SKU> --hertwill-id <ID> [--json]")
}

func MainForTests(args []string) int {
	return Run(args, os.Stdout, os.Stderr, BuildInfo{Version: "dev", Commit: "none", Date: "unknown"})
}
