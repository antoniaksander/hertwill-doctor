package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
	"github.com/antoniaksander/hertwill-doctor/internal/model"
	"github.com/antoniaksander/hertwill-doctor/internal/woocommerce"
)

type setTermsReport struct {
	Mode           string           `json:"mode"`
	ProductID      string           `json:"product_id"`
	Name           string           `json:"name,omitempty"`
	Status         string           `json:"status,omitempty"`
	Before         termsSnapshot    `json:"before"`
	CategoryIDs    []int            `json:"category_ids,omitempty"`
	BrandIDs       []int            `json:"brand_ids,omitempty"`
	After          *termsSnapshot   `json:"after,omitempty"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

type termsSnapshot struct {
	Categories []string `json:"categories"`
	Brand      string   `json:"brand"`
}

func snapshotTerms(p model.Product) termsSnapshot {
	return termsSnapshot{Categories: p.Categories, Brand: p.Brand}
}

func runWooSetTerms(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-terms", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	categories := fs.String("categories", "", "")
	brands := fs.String("brands", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if *id == "" {
		return fmt.Errorf("--id is required")
	}
	categoryIDs, err := parseTermIDs("--categories", *categories)
	if err != nil {
		return err
	}
	brandIDs, err := parseTermIDs("--brands", *brands)
	if err != nil {
		return err
	}
	if categoryIDs == nil && brandIDs == nil {
		return fmt.Errorf("provide --categories and/or --brands")
	}

	product, err := client.ProductByID(context.Background(), *id)
	if err != nil {
		return err
	}
	report := setTermsReport{
		Mode:        "dry-run",
		ProductID:   product.ID,
		Name:        product.Name,
		Status:      product.Status,
		Before:      snapshotTerms(product),
		CategoryIDs: categoryIDs,
		BrandIDs:    brandIDs,
	}
	if *confirm {
		report.Mode = "confirm"
		updated, err := client.UpdateProductTerms(context.Background(), *id, categoryIDs, brandIDs)
		if err != nil {
			return err
		}
		after := snapshotTerms(updated)
		report.After = &after
	}

	if g.json {
		snapshot := stats.Snapshot()
		report.RequestSummary = &snapshot
		return writeJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "Product: %s %s (%s)\n", report.ProductID, report.Name, report.Status)
	fmt.Fprintf(stdout, "Before:  categories %s; brand %s\n", joinOrNone(report.Before.Categories), fallbackNone(report.Before.Brand))
	if categoryIDs != nil {
		fmt.Fprintf(stdout, "Set categories to IDs: %s\n", joinInts(categoryIDs))
	}
	if brandIDs != nil {
		fmt.Fprintf(stdout, "Set brands to IDs: %s\n", joinInts(brandIDs))
	}
	if report.After == nil {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintf(stdout, "After:   categories %s; brand %s\n", joinOrNone(report.After.Categories), fallbackNone(report.After.Brand))
	return nil
}

// parseTermIDs returns nil when the flag was not given, so that field is left
// untouched. "none" clears the field.
func parseTermIDs(flagName, raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if raw == "none" {
		return []int{}, nil
	}
	var ids []int
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s: invalid term ID %q", flagName, part)
		}
		ids = append(ids, n)
	}
	return ids, nil
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func fallbackNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func wooSetTermsHelp(w io.Writer) {
	fmt.Fprintln(w, "Replace a WooCommerce product's categories and/or brands.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-terms --id <ID> [--categories <ID,ID,...>] [--brands <ID,...>] (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The given lists replace the current ones; a flag that is left out is not changed.")
	fmt.Fprintln(w, "Use \"none\" to clear a list. Only categories and brands are sent: price, stock,")
	fmt.Fprintln(w, "status and content are never touched, and the product is never published.")
}

var allowedStatuses = map[string]bool{"publish": true, "private": true, "draft": true, "pending": true}

type setStatusReport struct {
	Mode           string           `json:"mode"`
	ProductID      string           `json:"product_id"`
	Name           string           `json:"name,omitempty"`
	Before         string           `json:"before"`
	Requested      string           `json:"requested"`
	After          string           `json:"after,omitempty"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

func runWooSetStatus(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	status := fs.String("status", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if *id == "" {
		return fmt.Errorf("--id is required")
	}
	if !allowedStatuses[*status] {
		return fmt.Errorf("--status must be one of publish, private, draft, pending")
	}
	product, err := client.ProductByID(context.Background(), *id)
	if err != nil {
		return err
	}
	report := setStatusReport{Mode: "dry-run", ProductID: product.ID, Name: product.Name, Before: product.Status, Requested: *status}
	if *confirm {
		report.Mode = "confirm"
		updated, err := client.UpdateProductStatus(context.Background(), *id, *status)
		if err != nil {
			return err
		}
		report.After = updated.Status
	}
	if g.json {
		snapshot := stats.Snapshot()
		report.RequestSummary = &snapshot
		return writeJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "Product: %s %s\n", report.ProductID, report.Name)
	fmt.Fprintf(stdout, "Status:  %s -> %s\n", report.Before, report.Requested)
	if report.After == "" {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintf(stdout, "Now:     %s\n", report.After)
	return nil
}

func wooSetStatusHelp(w io.Writer) {
	fmt.Fprintln(w, "Change a WooCommerce product's status, e.g. publish a private product.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-status --id <ID> --status <publish|private|draft|pending> (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Only the status field is sent; nothing else on the product changes.")
}
