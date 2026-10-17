package cli

import (
	"context"
	"flag"
	"fmt"
	"html"
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
	TagIDs         []int            `json:"tag_ids,omitempty"`
	After          *termsSnapshot   `json:"after,omitempty"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

type termsSnapshot struct {
	Categories []string `json:"categories"`
	Brand      string   `json:"brand"`
	Tags       []string `json:"tags"`
}

func snapshotTerms(p model.Product) termsSnapshot {
	return termsSnapshot{Categories: p.Categories, Brand: p.Brand, Tags: p.Tags}
}

func runWooSetTerms(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-terms", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	categories := fs.String("categories", "", "")
	brands := fs.String("brands", "", "")
	tags := fs.String("tags", "", "")
	addTags := fs.String("add-tags", "", "")
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
	tagIDs, err := parseTermIDs("--tags", *tags)
	if err != nil {
		return err
	}
	addTagIDs, err := parseTermIDs("--add-tags", *addTags)
	if err != nil {
		return err
	}
	if tagIDs != nil && addTagIDs != nil {
		return fmt.Errorf("provide --tags or --add-tags, not both")
	}
	if categoryIDs == nil && brandIDs == nil && tagIDs == nil && len(addTagIDs) == 0 {
		return fmt.Errorf("provide --categories, --brands, --tags and/or --add-tags")
	}

	product, err := client.ProductByID(context.Background(), *id)
	if err != nil {
		return err
	}
	if len(addTagIDs) > 0 {
		tagIDs = mergeIDs(product.TagIDs, addTagIDs)
	}
	report := setTermsReport{
		Mode:        "dry-run",
		ProductID:   product.ID,
		Name:        product.Name,
		Status:      product.Status,
		Before:      snapshotTerms(product),
		CategoryIDs: categoryIDs,
		BrandIDs:    brandIDs,
		TagIDs:      tagIDs,
	}
	if *confirm {
		report.Mode = "confirm"
		updated, err := client.UpdateProductTerms(context.Background(), *id, categoryIDs, brandIDs, tagIDs)
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
	fmt.Fprintf(stdout, "Before:  categories %s; brand %s; tags %s\n", joinOrNone(report.Before.Categories), fallbackNone(report.Before.Brand), joinOrNone(report.Before.Tags))
	if categoryIDs != nil {
		fmt.Fprintf(stdout, "Set categories to IDs: %s\n", joinInts(categoryIDs))
	}
	if brandIDs != nil {
		fmt.Fprintf(stdout, "Set brands to IDs: %s\n", joinInts(brandIDs))
	}
	if tagIDs != nil {
		fmt.Fprintf(stdout, "Set tags to IDs: %s\n", joinOrNoneInts(tagIDs))
	}
	if report.After == nil {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintf(stdout, "After:   categories %s; brand %s; tags %s\n", joinOrNone(report.After.Categories), fallbackNone(report.After.Brand), joinOrNone(report.After.Tags))
	return nil
}

// mergeIDs returns current followed by the IDs from add that it doesn't
// already contain.
func mergeIDs(current, add []int) []int {
	out := append([]int{}, current...)
	seen := map[int]bool{}
	for _, id := range current {
		seen[id] = true
	}
	for _, id := range add {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func joinOrNoneInts(ids []int) string {
	if len(ids) == 0 {
		return "none"
	}
	return joinInts(ids)
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
	fmt.Fprintln(w, "Replace a WooCommerce product's categories, brands and/or tags.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-terms --id <ID> [--categories <ID,ID,...>] [--brands <ID,...>]")
	fmt.Fprintln(w, "       [--tags <ID,...> | --add-tags <ID,...>] (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The given lists replace the current ones; a flag that is left out is not changed.")
	fmt.Fprintln(w, "--add-tags keeps the product's current tags and adds the given ones.")
	fmt.Fprintln(w, "Use \"none\" to clear a list. Only categories, brands and tags are sent: price,")
	fmt.Fprintln(w, "stock, status and content are never touched, and the product is never published.")
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
	// Variations is the number of variations updated for a variable product.
	Variations int    `json:"variations,omitempty"`
	After      string `json:"after,omitempty"`
	Warning    string `json:"warning,omitempty"`
	Refused    string `json:"refused_reason,omitempty"`
	Error      string `json:"error,omitempty"`
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
		newPrice := strconv.FormatFloat(t.Price, 'f', 2, 64)
		product, err := client.ProductByID(context.Background(), wooID)
		var variations []woocommerce.Variation
		switch {
		case err != nil:
			entry.Refused = err.Error()
		case product.Type == "variation" && product.ParentID == "":
			entry.Refused = "variation without a parent product ID"
		case product.Type == "variable":
			variations, err = client.ProductVariations(context.Background(), wooID)
			if err != nil {
				entry.Refused = "listing variations: " + err.Error()
			} else if len(variations) == 0 {
				entry.Refused = "variable product has no variations"
			}
		}
		if product.ID != "" {
			entry.Name, entry.Status, entry.Before = product.Name, product.Status, product.Regular
			if product.Sale != "" {
				entry.Warning = "has sale price " + product.Sale + "; customers still see the sale price"
			}
		}
		if len(variations) > 0 {
			entry.Variations = len(variations)
			entry.Before = distinctPrices(variations, func(v woocommerce.Variation) string { return v.RegularPrice })
			if sale := distinctPrices(variations, func(v woocommerce.Variation) string { return v.SalePrice }); sale != "" {
				entry.Warning = "variations have sale price " + sale + "; customers still see the sale price"
			}
		}
		if entry.Refused != "" {
			report.Refused++
		} else if *confirm {
			if len(variations) > 0 {
				ids := make([]int, 0, len(variations))
				for _, v := range variations {
					ids = append(ids, v.ID)
				}
				updated, err := client.UpdateVariationPrices(context.Background(), wooID, ids, newPrice)
				entry.After = distinctPrices(updated, func(v woocommerce.Variation) string { return v.RegularPrice })
				if err != nil {
					entry.Error = fmt.Sprintf("%s (%d of %d variations updated)", err, len(updated), len(ids))
					report.Failed++
				} else {
					report.Changed++
				}
			} else if product.Type == "variation" {
				// A variation can't be updated through /products/{id}; it goes
				// through its parent's variations batch endpoint.
				updated, err := client.UpdateVariationPrices(context.Background(), product.ParentID, []int{t.ID}, newPrice)
				if err != nil {
					entry.Error = err.Error()
					report.Failed++
				} else {
					entry.After = distinctPrices(updated, func(v woocommerce.Variation) string { return v.RegularPrice })
					report.Changed++
				}
			} else {
				updated, err := client.UpdateProductPrice(context.Background(), wooID, newPrice)
				if err != nil {
					entry.Error = err.Error()
					report.Failed++
				} else {
					entry.After = updated.Regular
					report.Changed++
				}
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
		fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tVARIATIONS\tBEFORE\tNEW\tRESULT")
		for _, e := range report.Entries {
			variations := "-"
			if e.Variations > 0 {
				variations = strconv.Itoa(e.Variations)
			}
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
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%.2f\t%s\n", e.ProductID, e.Name, e.Status, variations, e.Before, e.Price, result)
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

// distinctPrices joins the distinct non-empty prices of variations, in order
// of first appearance, e.g. "140" or "135/140".
func distinctPrices(variations []woocommerce.Variation, field func(woocommerce.Variation) string) string {
	var prices []string
	seen := map[string]bool{}
	for _, v := range variations {
		if p := field(v); p != "" && !seen[p] {
			seen[p] = true
			prices = append(prices, p)
		}
	}
	return strings.Join(prices, "/")
}

func wooSetPriceHelp(w io.Writer) {
	fmt.Fprintln(w, "Change the regular price of WooCommerce products.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-price (--id <WOO ID> --price <PRICE> | --file <PATH>) (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "PRICE is the selling price incl. VAT. The file has one \"<woo id> <price>\" per line.")
	fmt.Fprintln(w, "Only regular_price is sent. For a variable product, every variation gets PRICE")
	fmt.Fprintln(w, "(one batch request per 100 variations). Status, stock and terms are not touched.")
	fmt.Fprintln(w, "Sale prices are reported, not changed.")
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

func runWooTrash(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo trash", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
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
	product, err := client.ProductByID(context.Background(), *id)
	if err != nil {
		return err
	}
	if product.Status == "trash" {
		return fmt.Errorf("product %s is already in the trash", product.ID)
	}
	result := map[string]string{"mode": "dry-run", "product_id": product.ID, "name": product.Name, "sku": product.SKU, "before": product.Status}
	if *confirm {
		result["mode"] = "confirm"
		if _, err := client.TrashProduct(context.Background(), *id); err != nil {
			return err
		}
		// The DELETE response shows the product as it was before; re-read it.
		after, err := client.ProductByID(context.Background(), *id)
		if err != nil {
			return err
		}
		result["after"] = after.Status
	}
	if g.json {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Product: %s %s (SKU %s)\nStatus:  %s -> trash\n", product.ID, product.Name, product.SKU, product.Status)
	if result["after"] == "" {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintf(stdout, "Now:     %s (restore it from Products > Trash in wp-admin)\n", result["after"])
	return nil
}

func wooTrashHelp(w io.Writer) {
	fmt.Fprintln(w, "Move a WooCommerce product to the trash (bin). It can be restored from wp-admin.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo trash --id <ID> (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Never deletes permanently. If the product is still on the Hertwill import list,")
	fmt.Fprintln(w, "remove it there too, or a later sync may create it again.")
}

var replaceableFields = map[string]bool{"name": true, "slug": true, "short_description": true, "description": true}

func runWooReplace(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo replace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	field := fs.String("field", "", "")
	find := fs.String("find", "", "")
	replace := fs.String("replace", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if *id == "" || *find == "" {
		return fmt.Errorf("--id and --find are required")
	}
	if !replaceableFields[*field] {
		return fmt.Errorf("--field must be one of name, slug, short_description, description")
	}
	fields, err := client.ProductFields(context.Background(), *id)
	if err != nil {
		return err
	}
	before := fields[*field]
	count := strings.Count(before, *find)
	result := map[string]any{"mode": "dry-run", "product_id": *id, "field": *field, "matches": count}
	if count == 0 {
		return fmt.Errorf("%q not found in %s of product %s", *find, *field, *id)
	}
	after := strings.ReplaceAll(before, *find, *replace)
	if *confirm {
		result["mode"] = "confirm"
		updated, err := client.UpdateProductField(context.Background(), *id, *field, after)
		if err != nil {
			return err
		}
		after = updated[*field]
		result["remaining_matches"] = strings.Count(after, *find)
	}
	if g.json {
		result["after"] = after
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Product %s, %s: %d match(es) of %q -> %q\n", *id, *field, count, *find, *replace)
	if *field == "name" || *field == "slug" {
		fmt.Fprintf(stdout, "Before: %s\nAfter:  %s\n", before, after)
	}
	if result["mode"] == "dry-run" {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
	}
	return nil
}

func wooReplaceHelp(w io.Writer) {
	fmt.Fprintln(w, "Find and replace text in one field of a WooCommerce product.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo replace --id <ID> --field <name|slug|short_description|description> --find <TEXT> --replace <TEXT> (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Case-sensitive, replaces every match, and sends only that field. Refuses if the")
	fmt.Fprintln(w, "text isn't found. Changing a slug changes the product URL; WordPress normally")
	fmt.Fprintln(w, "redirects the old URL.")
}

func runWooCreateCategory(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo create-category", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "")
	parent := fs.Int("parent", 0, "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	n := strings.TrimSpace(*name)
	if n == "" {
		return fmt.Errorf("--name is required")
	}
	existing, err := client.FindCategories(context.Background(), n)
	if err != nil {
		return err
	}
	for _, c := range existing {
		if strings.EqualFold(html.UnescapeString(c.Name), n) {
			return fmt.Errorf("category %q already exists (ID %d, parent %d)", n, c.ID, c.Parent)
		}
	}
	result := map[string]any{"mode": "dry-run", "name": n, "parent": *parent}
	if *confirm {
		result["mode"] = "confirm"
		cat, err := client.CreateCategory(context.Background(), n, *parent)
		if err != nil {
			return err
		}
		result["id"], result["slug"] = cat.ID, cat.Slug
	}
	if g.json {
		return writeJSON(stdout, result)
	}
	if *dryRun {
		fmt.Fprintf(stdout, "Would create category %q under parent %d.\n\nDry run only. No WooCommerce changes were made.\n", n, *parent)
		return nil
	}
	fmt.Fprintf(stdout, "Created category %q: ID %v, slug %v, parent %d\n", n, result["id"], result["slug"], *parent)
	return nil
}

func wooCreateCategoryHelp(w io.Writer) {
	fmt.Fprintln(w, "Create a WooCommerce product category.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo create-category --name <NAME> [--parent <ID>] (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Refuses if a category with the same name already exists.")
}

// categoryTree indexes every category by ID for parent/child checks.
type categoryTree map[int]woocommerce.Category

func loadCategoryTree(client woocommerce.Client) (categoryTree, error) {
	cats, err := client.ListCategories(context.Background())
	if err != nil {
		return nil, err
	}
	tree := categoryTree{}
	for _, c := range cats {
		c.Name = html.UnescapeString(c.Name)
		tree[c.ID] = c
	}
	return tree, nil
}

// path returns "Parent > Child" for a category, or "(top level)" for 0.
func (t categoryTree) path(id int) string {
	if id == 0 {
		return "(top level)"
	}
	var parts []string
	for seen := map[int]bool{}; id != 0 && !seen[id]; {
		seen[id] = true
		c, ok := t[id]
		if !ok {
			parts = append([]string{fmt.Sprintf("#%d", id)}, parts...)
			break
		}
		parts = append([]string{c.Name}, parts...)
		id = c.Parent
	}
	return strings.Join(parts, " > ")
}

func (t categoryTree) children(id int) []woocommerce.Category {
	var out []woocommerce.Category
	for _, c := range t {
		if c.Parent == id {
			out = append(out, c)
		}
	}
	return out
}

// isDescendant reports whether candidate sits anywhere below id.
func (t categoryTree) isDescendant(candidate, id int) bool {
	for seen := map[int]bool{}; candidate != 0 && !seen[candidate]; {
		seen[candidate] = true
		c, ok := t[candidate]
		if !ok {
			return false
		}
		if c.Parent == id {
			return true
		}
		candidate = c.Parent
	}
	return false
}

func runWooUpdateCategory(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo update-category", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.Int("id", 0, "")
	name := fs.String("name", "", "")
	parent := fs.Int("parent", 0, "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	parentSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "parent" {
			parentSet = true
		}
	})
	newName := strings.TrimSpace(*name)
	if *id <= 0 {
		return fmt.Errorf("--id is required")
	}
	if newName == "" && !parentSet {
		return fmt.Errorf("give --name and/or --parent")
	}
	tree, err := loadCategoryTree(client)
	if err != nil {
		return err
	}
	cat, ok := tree[*id]
	if !ok {
		return fmt.Errorf("category %d not found", *id)
	}
	var namePtr *string
	if newName != "" {
		if newName == cat.Name {
			return fmt.Errorf("category %d is already called %q", *id, newName)
		}
		for _, c := range tree {
			if c.ID != *id && strings.EqualFold(c.Name, newName) {
				return fmt.Errorf("category %q already exists (ID %d, parent %d)", newName, c.ID, c.Parent)
			}
		}
		namePtr = &newName
	}
	var parentPtr *int
	if parentSet {
		switch {
		case *parent < 0:
			return fmt.Errorf("--parent must be a category ID, or 0 for top level")
		case *parent == *id:
			return fmt.Errorf("a category can't be its own parent")
		case *parent == cat.Parent:
			return fmt.Errorf("category %d is already under %s", *id, tree.path(*parent))
		case *parent != 0 && tree[*parent].ID == 0:
			return fmt.Errorf("parent category %d not found", *parent)
		case tree.isDescendant(*parent, *id):
			return fmt.Errorf("can't move %q under its own subcategory %s", cat.Name, tree.path(*parent))
		}
		parentPtr = parent
	}

	result := map[string]any{"mode": "dry-run", "id": cat.ID, "slug": cat.Slug, "before": map[string]any{"name": cat.Name, "parent": cat.Parent, "path": tree.path(cat.ID)}}
	requested := map[string]any{}
	if namePtr != nil {
		requested["name"] = *namePtr
	}
	if parentPtr != nil {
		requested["parent"] = *parentPtr
		requested["parent_path"] = tree.path(*parentPtr)
	}
	result["requested"] = requested
	if *confirm {
		result["mode"] = "confirm"
		updated, err := client.UpdateCategory(context.Background(), *id, namePtr, parentPtr)
		if err != nil {
			return err
		}
		result["after"] = map[string]any{"name": html.UnescapeString(updated.Name), "parent": updated.Parent, "slug": updated.Slug}
	}
	if g.json {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Category: %d %s (slug %s, %d published products)\n", cat.ID, tree.path(cat.ID), cat.Slug, cat.Count)
	if namePtr != nil {
		fmt.Fprintf(stdout, "Name:     %s -> %s\n", cat.Name, *namePtr)
	}
	if parentPtr != nil {
		fmt.Fprintf(stdout, "Parent:   %s -> %s\n", tree.path(cat.Parent), tree.path(*parentPtr))
		fmt.Fprintln(stdout, "Note:     moving changes the category URL; update menu links and add a redirect for the old one.")
	}
	after, done := result["after"].(map[string]any)
	if !done {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintf(stdout, "Now:      %v (parent %v, slug %v)\n", after["name"], after["parent"], after["slug"])
	return nil
}

func wooUpdateCategoryHelp(w io.Writer) {
	fmt.Fprintln(w, "Rename a WooCommerce product category and/or move it under another parent.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo update-category --id <ID> [--name <NAME>] [--parent <ID>] (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Sends only the given fields; the slug is never changed, so a rename keeps the URL.")
	fmt.Fprintln(w, "--parent 0 moves it to the top level. Moving changes the URL path of the category")
	fmt.Fprintln(w, "and its subcategories. Refuses a name another category already has, a parent that")
	fmt.Fprintln(w, "doesn't exist, and a move under the category's own subcategory.")
}

func runWooDeleteCategory(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo delete-category", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.Int("id", 0, "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if *id <= 0 {
		return fmt.Errorf("--id is required")
	}
	tree, err := loadCategoryTree(client)
	if err != nil {
		return err
	}
	cat, ok := tree[*id]
	if !ok {
		return fmt.Errorf("category %d not found", *id)
	}
	if cat.Slug == "uncategorized" {
		return fmt.Errorf("%q is WooCommerce's default category and can't be deleted; rename it instead", cat.Name)
	}
	if kids := tree.children(*id); len(kids) > 0 {
		names := make([]string, 0, len(kids))
		for _, k := range kids {
			names = append(names, fmt.Sprintf("%s (%d)", k.Name, k.ID))
		}
		return fmt.Errorf("category %q has %d subcategories (%s); move or delete them first", cat.Name, len(kids), strings.Join(names, ", "))
	}
	hasProducts, err := client.CategoryHasProducts(context.Background(), *id)
	if err != nil {
		return err
	}
	if hasProducts {
		return fmt.Errorf("category %q still has products (any status, incl. private); move them with set-terms first", cat.Name)
	}

	result := map[string]any{"mode": "dry-run", "id": cat.ID, "name": cat.Name, "slug": cat.Slug, "path": tree.path(cat.ID)}
	if *confirm {
		result["mode"] = "confirm"
		if _, err := client.DeleteCategory(context.Background(), *id); err != nil {
			return err
		}
		result["deleted"] = true
	}
	if g.json {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Category: %d %s (slug %s), no products, no subcategories\n", cat.ID, tree.path(cat.ID), cat.Slug)
	if result["deleted"] != true {
		fmt.Fprintln(stdout, "Would be deleted permanently: categories have no trash.")
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintln(stdout, "Deleted permanently. Remove any menu links that pointed to it.")
	return nil
}

func wooDeleteCategoryHelp(w io.Writer) {
	fmt.Fprintln(w, "Delete an empty WooCommerce product category, permanently.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo delete-category --id <ID> (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Categories have no trash, so this can't be undone. Refuses if the category has")
	fmt.Fprintln(w, "subcategories or any product in any status (private and draft too; WooCommerce's")
	fmt.Fprintln(w, "own count only includes published products), and refuses the default category.")
}
