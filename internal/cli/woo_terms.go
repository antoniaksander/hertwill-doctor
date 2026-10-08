package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

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

type setPriceEntry struct {
	ProductID string  `json:"product_id"`
	Name      string  `json:"name,omitempty"`
	Status    string  `json:"status,omitempty"`
	Before    string  `json:"before,omitempty"`
	Price     float64 `json:"price"`
	After     string  `json:"after,omitempty"`
	Warning   string  `json:"warning,omitempty"`
	Refused   string  `json:"refused_reason,omitempty"`
	Error     string  `json:"error,omitempty"`
}

type setPriceReport struct {
	Mode           string           `json:"mode"`
	Entries        []setPriceEntry  `json:"entries"`
	Changed        int              `json:"changed"`
	Failed         int              `json:"failed"`
	Refused        int              `json:"refused"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

func runWooSetPrice(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-price", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.Int("id", 0, "")
	price := fs.Float64("price", 0, "")
	file := fs.String("file", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if (*id != 0) == (*file != "") {
		return fmt.Errorf("provide either --id with --price, or --file")
	}
	var targets []priceTarget
	if *id != 0 {
		if *price <= 0 {
			return fmt.Errorf("--price must be the selling price incl. VAT, e.g. 16.50")
		}
		targets = []priceTarget{{ID: *id, Price: *price}}
	} else {
		parsed, err := readPriceFile(*file)
		if err != nil {
			return err
		}
		targets = parsed
	}

	report := setPriceReport{Mode: "dry-run"}
	if *confirm {
		report.Mode = "confirm"
	}
	for _, t := range targets {
		wooID := strconv.Itoa(t.ID)
		entry := setPriceEntry{ProductID: wooID, Price: t.Price}
		product, err := client.ProductByID(context.Background(), wooID)
		switch {
		case err != nil:
			entry.Refused = err.Error()
		case product.Type == "variable":
			entry.Refused = "variable product: prices are set on its variations"
		}
		if err == nil {
			entry.Name, entry.Status, entry.Before = product.Name, product.Status, product.Regular
			if product.Sale != "" {
				entry.Warning = "has sale price " + product.Sale + "; customers still see the sale price"
			}
		}
		if entry.Refused != "" {
			report.Refused++
		} else if *confirm {
			updated, err := client.UpdateProductPrice(context.Background(), wooID, strconv.FormatFloat(t.Price, 'f', 2, 64))
			if err != nil {
				entry.Error = err.Error()
				report.Failed++
			} else {
				entry.After = updated.Regular
				report.Changed++
			}
		}
		report.Entries = append(report.Entries, entry)
	}

	if g.json {
		snapshot := stats.Snapshot()
		report.RequestSummary = &snapshot
		if err := writeJSON(stdout, report); err != nil {
			return err
		}
	} else {
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(stdout, "Mode: %s (regular price incl. VAT)\n\n", report.Mode)
		fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tBEFORE\tNEW\tRESULT")
		for _, e := range report.Entries {
			result := "would change"
			switch {
			case e.Refused != "":
				result = "refused: " + e.Refused
			case e.Error != "":
				result = "failed: " + e.Error
			case e.After != "":
				result = "now " + e.After
			}
			if e.Warning != "" {
				result += " (" + e.Warning + ")"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%.2f\t%s\n", e.ProductID, e.Name, e.Status, e.Before, e.Price, result)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if report.Mode == "dry-run" {
			fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		} else {
			fmt.Fprintf(stdout, "\nChanged: %d, failed: %d, refused: %d\n", report.Changed, report.Failed, report.Refused)
		}
	}
	if report.Failed > 0 || report.Refused > 0 {
		return fmt.Errorf("%d failed, %d refused", report.Failed, report.Refused)
	}
	return nil
}

func wooSetPriceHelp(w io.Writer) {
	fmt.Fprintln(w, "Change the regular price of simple WooCommerce products.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-price (--id <WOO ID> --price <PRICE> | --file <PATH>) (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "PRICE is the selling price incl. VAT. The file has one \"<woo id> <price>\" per line.")
	fmt.Fprintln(w, "Only regular_price is sent. Variable products are refused; a sale price is reported.")
	fmt.Fprintln(w, "Note: a later Hertwill re-sync of the product may set its own price again.")
}

func runWooSetName(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-name", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	name := fs.String("name", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	newName := strings.TrimSpace(*name)
	if *id == "" || newName == "" {
		return fmt.Errorf("--id and --name are required")
	}
	product, err := client.ProductByID(context.Background(), *id)
	if err != nil {
		return err
	}
	result := map[string]string{"mode": "dry-run", "product_id": product.ID, "before": product.Name, "requested": newName}
	if *confirm {
		result["mode"] = "confirm"
		updated, err := client.UpdateProductName(context.Background(), *id, newName)
		if err != nil {
			return err
		}
		result["after"] = updated.Name
	}
	if g.json {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Product: %s\nName:    %s -> %s\n", product.ID, product.Name, newName)
	if result["after"] == "" {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintf(stdout, "Now:     %s\n", result["after"])
	return nil
}

func wooSetNameHelp(w io.Writer) {
	fmt.Fprintln(w, "Change a WooCommerce product's name (title). The URL slug is not changed.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-name --id <ID> --name <NAME> (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Only the name field is sent. A later Hertwill re-sync may set its own name again.")
}
