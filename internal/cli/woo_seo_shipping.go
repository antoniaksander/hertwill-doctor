package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
	"github.com/antoniaksander/hertwill-doctor/internal/woocommerce"
)

// Rank Math stores the SEO title and description as plain post meta.
const (
	seoTitleKey       = "rank_math_title"
	seoDescriptionKey = "rank_math_description"
)

type setSEOReport struct {
	Mode           string             `json:"mode"`
	ProductID      string             `json:"product_id"`
	Name           string             `json:"name"`
	Before         map[string]string  `json:"before"`
	Requested      map[string]string  `json:"requested"`
	After          map[string]string  `json:"after,omitempty"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

func runWooSetSEO(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-seo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	title := fs.String("title", "", "")
	description := fs.String("description", "", "")
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
	requested := map[string]string{}
	if t := strings.TrimSpace(*title); t != "" {
		requested[seoTitleKey] = t
	}
	if d := strings.TrimSpace(*description); d != "" {
		requested[seoDescriptionKey] = d
	}
	if len(requested) == 0 {
		return fmt.Errorf("provide --title and/or --description")
	}
	keys := sortedKeys(requested)
	name, before, err := client.ProductMeta(context.Background(), *id, keys)
	if err != nil {
		return err
	}
	report := setSEOReport{Mode: "dry-run", ProductID: *id, Name: name, Before: before, Requested: requested}
	if *confirm {
		report.Mode = "confirm"
		after, err := client.UpdateProductMeta(context.Background(), *id, requested)
		if err != nil {
			return err
		}
		report.After = after
		for _, key := range keys {
			if after[key] != requested[key] {
				return fmt.Errorf("WooCommerce returned %s %q instead of %q", key, after[key], requested[key])
			}
		}
	}
	if g.json {
		snapshot := stats.Snapshot()
		report.RequestSummary = &snapshot
		return writeJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "Product: %s %s\n", report.ProductID, report.Name)
	for _, key := range keys {
		fmt.Fprintf(stdout, "%s\n  before: %s\n  set to: %s\n", key, fallbackNone(before[key]), requested[key])
		if report.After != nil {
			fmt.Fprintf(stdout, "  after:  %s\n", fallbackNone(report.After[key]))
		}
	}
	if report.After == nil {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
	}
	return nil
}

func wooSetSEOHelp(w io.Writer) {
	fmt.Fprintln(w, "Set a product's SEO title and/or meta description (Rank Math meta).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-seo --id <ID> [--title <TEXT>] [--description <TEXT>] (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Writes rank_math_title / rank_math_description through the product's meta_data;")
	fmt.Fprintln(w, "nothing else is sent. With --confirm the command fails if WooCommerce didn't")
	fmt.Fprintln(w, "store the value.")
}

func runWooShippingClasses(stdout io.Writer, g globals, client woocommerce.Client) error {
	classes, err := client.ListShippingClasses(context.Background())
	if err != nil {
		return err
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i].Slug < classes[j].Slug })
	if g.json {
		return writeJSON(stdout, classes)
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSLUG\tNAME\tPRODUCTS")
	for _, c := range classes {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d\n", c.ID, c.Slug, c.Name, c.Count)
	}
	return tw.Flush()
}

func wooShippingClassesHelp(w io.Writer) {
	fmt.Fprintln(w, "List WooCommerce shipping classes with IDs and product counts.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo shipping-classes [--json]")
}

func runWooCreateShippingClass(args []string, stdout io.Writer, g globals, client woocommerce.Client) error {
	fs := flag.NewFlagSet("woo create-shipping-class", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "")
	slug := fs.String("slug", "", "")
	description := fs.String("description", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if strings.TrimSpace(*name) == "" || strings.TrimSpace(*slug) == "" {
		return fmt.Errorf("--name and --slug are required")
	}
	existing, err := client.ListShippingClasses(context.Background())
	if err != nil {
		return err
	}
	for _, c := range existing {
		if c.Slug == *slug {
			return fmt.Errorf("shipping class %q already exists (ID %d)", c.Slug, c.ID)
		}
	}
	if *dryRun {
		if g.json {
			return writeJSON(stdout, map[string]string{"mode": "dry-run", "name": *name, "slug": *slug, "description": *description})
		}
		fmt.Fprintf(stdout, "Would create shipping class %q (slug %s).\n\nDry run only. No WooCommerce changes were made.\n", *name, *slug)
		return nil
	}
	created, err := client.CreateShippingClass(context.Background(), *name, *slug, *description)
	if err != nil {
		return err
	}
	if created.Slug != *slug {
		return fmt.Errorf("WooCommerce created slug %q instead of %q", created.Slug, *slug)
	}
	if g.json {
		return writeJSON(stdout, created)
	}
	fmt.Fprintf(stdout, "Created shipping class %q: ID %d, slug %s.\n", created.Name, created.ID, created.Slug)
	return nil
}

func wooCreateShippingClassHelp(w io.Writer) {
	fmt.Fprintln(w, "Create a product shipping class.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo create-shipping-class --name <NAME> --slug <SLUG> [--description <TEXT>] (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Refuses if the slug already exists. Costs are set per zone with set-class-costs.")
}

type classCostEntry struct {
	ZoneID     int    `json:"zone_id"`
	Zone       string `json:"zone"`
	Countries  string `json:"countries"`
	InstanceID int    `json:"instance_id"`
	Method     string `json:"method"`
	Before     string `json:"before"`
	New        string `json:"new"`
	After      string `json:"after,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
	Error      string `json:"error,omitempty"`
}

// parseCountryCosts reads "EE=5.23,FI=13.49" into a map of upper-case codes.
func parseCountryCosts(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		code, cost, ok := strings.Cut(part, "=")
		code = strings.ToUpper(strings.TrimSpace(code))
		cost = strings.TrimSpace(cost)
		if !ok || len(code) != 2 || cost == "" {
			return nil, fmt.Errorf("bad cost %q: use CC=COST, e.g. EE=5.23", part)
		}
		if _, err := strconv.ParseFloat(cost, 64); err != nil && !strings.ContainsAny(cost, "[]*") {
			return nil, fmt.Errorf("bad cost %q for %s", cost, code)
		}
		out[code] = cost
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--costs is empty")
	}
	return out, nil
}

func runWooSetClassCosts(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-class-costs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	class := fs.String("class", "", "")
	costsRaw := fs.String("costs", "", "")
	others := fs.String("others", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	if *class == "" {
		return fmt.Errorf("--class is required (shipping class slug)")
	}
	costs, err := parseCountryCosts(*costsRaw)
	if err != nil {
		return err
	}
	classes, err := client.ListShippingClasses(context.Background())
	if err != nil {
		return err
	}
	classID := 0
	for _, c := range classes {
		if c.Slug == *class {
			classID = c.ID
		}
	}
	if classID == 0 {
		return fmt.Errorf("shipping class %q not found; create it first with woo create-shipping-class", *class)
	}
	settingKey := "class_cost_" + strconv.Itoa(classID)
	zones, err := client.ShippingZones(context.Background())
	if err != nil {
		return err
	}
	var entries []classCostEntry
	changed, failed := 0, 0
	for _, z := range zones {
		cost, reason := "", ""
		matched := map[string]bool{}
		for _, code := range z.Countries {
			if c, ok := costs[code]; ok {
				matched[c] = true
				cost = c
			}
		}
		switch {
		case len(z.Countries) == 0:
			reason = "zone has no country locations"
		case len(matched) > 1:
			reason = "countries in this zone have different costs"
		case len(matched) == 0 && *others != "":
			cost = *others
		case len(matched) == 0:
			reason = "no cost given for " + strings.Join(z.Countries, ",")
		case len(matched) == 1 && len(z.Countries) > 1:
			for _, code := range z.Countries {
				if _, ok := costs[code]; !ok {
					reason = "cost missing for " + code + " in this zone"
				}
			}
		}
		if len(z.FlatRates) == 0 && reason == "" {
			reason = "no flat rate method"
		}
		if reason != "" {
			entries = append(entries, classCostEntry{ZoneID: z.ID, Zone: z.Name, Countries: strings.Join(z.Countries, ","), Skipped: reason})
			continue
		}
		for _, fr := range z.FlatRates {
			entry := classCostEntry{ZoneID: z.ID, Zone: z.Name, Countries: strings.Join(z.Countries, ","), InstanceID: fr.InstanceID, Method: fr.Title, Before: fr.Settings[settingKey], New: cost}
			if *confirm {
				after, err := client.UpdateFlatRateSettings(context.Background(), z.ID, fr.InstanceID, map[string]string{settingKey: cost})
				switch {
				case err != nil:
					entry.Error = err.Error()
					failed++
				case after[settingKey] != cost:
					entry.Error = fmt.Sprintf("WooCommerce kept %q", after[settingKey])
					failed++
				default:
					entry.After = after[settingKey]
					changed++
				}
			}
			entries = append(entries, entry)
		}
	}
	if g.json {
		snapshot := stats.Snapshot()
		return writeJSON(stdout, map[string]any{"mode": modeName(*confirm), "class": *class, "class_id": classID, "entries": entries, "changed": changed, "failed": failed, "request_summary": &snapshot})
	}
	fmt.Fprintf(stdout, "Mode: %s, class %s (ID %d, setting %s)\n\n", modeName(*confirm), *class, classID, settingKey)
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ZONE\tCOUNTRIES\tMETHOD\tBEFORE\tNEW\tRESULT")
	for _, e := range entries {
		result := "would change"
		switch {
		case e.Skipped != "":
			result = "skipped: " + e.Skipped
		case e.Error != "":
			result = "failed: " + e.Error
		case e.After != "":
			result = "now " + e.After
		}
		fmt.Fprintf(tw, "%d %s\t%s\t%s\t%s\t%s\t%s\n", e.ZoneID, e.Zone, e.Countries, e.Method, fallbackNone(e.Before), e.New, result)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if !*confirm {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
	} else {
		fmt.Fprintf(stdout, "\nChanged: %d, failed: %d\n", changed, failed)
		fmt.Fprintln(stdout, "Note: customers with an open cart may see cached rates until the shipping cache refreshes (re-save one zone in wp-admin to force it).")
	}
	if failed > 0 {
		return fmt.Errorf("%d failed", failed)
	}
	return nil
}

func modeName(confirm bool) string {
	if confirm {
		return "confirm"
	}
	return "dry-run"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func wooSetClassCostsHelp(w io.Writer) {
	fmt.Fprintln(w, "Set a shipping class's cost in every zone's flat rate method, by country.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-class-costs --class <SLUG> --costs <CC=COST,...> [--others <COST>] (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Each zone's countries are matched against --costs; zones whose countries")
	fmt.Fprintln(w, "aren't listed get --others (e.g. the store's \"not deliverable\" cost) or are")
	fmt.Fprintln(w, "skipped. COST is what WooCommerce stores: a number or a formula like")
	fmt.Fprintln(w, "\"4.99 * [qty]\". Only the class_cost_<ID> setting is sent.")
}
