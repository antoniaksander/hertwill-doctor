package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/antoniaksander/hertwill-doctor/internal/hertwill"
	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
)

// sleep is swapped out in tests so batch syncs don't wait between requests.
var sleep = time.Sleep

type importListResult struct {
	Items          []hertwill.ImportListItem `json:"items"`
	Count          int                       `json:"count"`
	Status         string                    `json:"status,omitempty"`
	Contains       string                    `json:"contains,omitempty"`
	RequestSummary *httpstats.Stats          `json:"request_summary,omitempty"`
}

type importReport struct {
	Mode           string                  `json:"mode"`
	Requested      []int                   `json:"requested"`
	AlreadyListed  []int                   `json:"already_listed,omitempty"`
	ToAdd          []int                   `json:"to_add,omitempty"`
	Results        []hertwill.ImportResult `json:"results,omitempty"`
	RequestSummary *httpstats.Stats        `json:"request_summary,omitempty"`
}

type syncPlanEntry struct {
	ID         int     `json:"id"`
	Name       string  `json:"name,omitempty"`
	Status     string  `json:"import_status,omitempty"`
	Cost       float64 `json:"cost"`
	Price      float64 `json:"price"`
	Variations int     `json:"variations"`
	Refused    string  `json:"refused_reason,omitempty"`
	Result     string  `json:"result,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type syncReport struct {
	Mode           string           `json:"mode"`
	Currency       string           `json:"currency"`
	Entries        []syncPlanEntry  `json:"entries"`
	Started        int              `json:"started"`
	Failed         int              `json:"failed"`
	Refused        int              `json:"refused"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

type priceTarget struct {
	ID    int
	Price float64
}

func runHertwillImportList(args []string, stdout io.Writer, g globals, client hertwill.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("hertwill import-list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	status := fs.String("status", "", "")
	contains := fs.String("contains", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	items, err := client.ImportList(context.Background(), *status)
	if err != nil {
		return err
	}
	if text := strings.ToLower(strings.TrimSpace(*contains)); text != "" {
		filtered := items[:0]
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Name), text) || strings.Contains(strings.ToLower(item.SKU), text) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if items == nil {
		items = []hertwill.ImportListItem{}
	}
	if g.json {
		snapshot := stats.Snapshot()
		return writeJSON(stdout, importListResult{Items: items, Count: len(items), Status: *status, Contains: strings.TrimSpace(*contains), RequestSummary: &snapshot})
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No results")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tCOST\tVARIANTS\tSKU")
	for _, item := range items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.2f\t%d\t%s\n", item.ID, item.Name, item.Status, item.Price, len(item.Variations), item.SKU)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n%d product(s)\n", len(items))
	return nil
}

func runHertwillImport(args []string, stdout io.Writer, g globals, client hertwill.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("hertwill import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	idsFlag := fs.String("ids", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	confirm := fs.Bool("confirm", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *confirm {
		return fmt.Errorf("provide exactly one of --dry-run or --confirm")
	}
	ids, err := parseIDList(*idsFlag)
	if err != nil {
		return err
	}
	if len(ids) > 50 {
		return fmt.Errorf("at most 50 product IDs per import")
	}

	report := importReport{Mode: "dry-run", Requested: ids}
	if *confirm {
		report.Mode = "confirm"
	}
	listed, err := client.ImportList(context.Background(), "")
	if err != nil {
		return err
	}
	inList := map[int]bool{}
	for _, item := range listed {
		inList[item.ID] = true
	}
	for _, id := range ids {
		if inList[id] {
			report.AlreadyListed = append(report.AlreadyListed, id)
		} else {
			report.ToAdd = append(report.ToAdd, id)
		}
	}
	if *confirm && len(report.ToAdd) > 0 {
		results, err := client.AddToImportList(context.Background(), report.ToAdd)
		if err != nil {
			return err
		}
		report.Results = results
	}

	if g.json {
		snapshot := stats.Snapshot()
		report.RequestSummary = &snapshot
		return writeJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "Mode: %s\n", report.Mode)
	fmt.Fprintf(stdout, "Already in import list: %s\n", joinInts(report.AlreadyListed))
	fmt.Fprintf(stdout, "To add: %s\n", joinInts(report.ToAdd))
	if *dryRun {
		fmt.Fprintln(stdout, "\nDry run only. No Hertwill changes were made.")
		return nil
	}
	if len(report.Results) > 0 {
		fmt.Fprintln(stdout)
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tRESULT\tVARIANTS")
		for _, r := range report.Results {
			fmt.Fprintf(tw, "%d\t%s\t%d\n", r.ProductID, r.Status, len(r.Variations))
		}
		return tw.Flush()
	}
	return nil
}

func runHertwillSync(args []string, stdout io.Writer, g globals, client hertwill.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("hertwill sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.Int("id", 0, "")
	price := fs.Float64("price", 0, "")
	file := fs.String("file", "", "")
	currency := fs.String("currency", "EUR", "")
	delayMS := fs.Int("delay-ms", 1000, "")
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
			return fmt.Errorf("--price must be the absolute selling price, e.g. 40.95")
		}
		targets = []priceTarget{{ID: *id, Price: *price}}
	} else {
		parsed, err := readPriceFile(*file)
		if err != nil {
			return err
		}
		targets = parsed
	}

	items, err := client.ImportList(context.Background(), "")
	if err != nil {
		return err
	}
	byID := map[int]hertwill.ImportListItem{}
	for _, item := range items {
		byID[item.ID] = item
	}

	report := syncReport{Mode: "dry-run", Currency: *currency}
	if *confirm {
		report.Mode = "confirm"
	}
	for _, target := range targets {
		entry := syncPlanEntry{ID: target.ID, Price: target.Price}
		item, ok := byID[target.ID]
		switch {
		case !ok:
			entry.Refused = "not in the unsynced import list (already synced, or run hwd hertwill import first)"
		case target.Price < item.Price:
			entry.Refused = fmt.Sprintf("price %.2f is below cost %.2f", target.Price, item.Price)
		}
		if ok {
			entry.Name, entry.Status, entry.Cost, entry.Variations = item.Name, item.Status, item.Price, len(item.Variations)
		}
		if entry.Refused != "" {
			report.Refused++
		}
		report.Entries = append(report.Entries, entry)
	}

	if *confirm {
		sent := 0
		for i := range report.Entries {
			entry := &report.Entries[i]
			if entry.Refused != "" {
				continue
			}
			if sent > 0 && *delayMS > 0 {
				sleep(time.Duration(*delayMS) * time.Millisecond)
			}
			sent++
			result, err := client.SyncProduct(context.Background(), hertwill.BuildSyncRequest(byID[entry.ID], entry.Price, *currency))
			if err != nil {
				entry.Error = err.Error()
				report.Failed++
				continue
			}
			entry.Result = result.Status
			report.Started++
		}
	}

	if g.json {
		snapshot := stats.Snapshot()
		report.RequestSummary = &snapshot
		if err := writeJSON(stdout, report); err != nil {
			return err
		}
	} else if err := printSyncReport(stdout, report); err != nil {
		return err
	}
	if report.Failed > 0 || report.Refused > 0 {
		return fmt.Errorf("%d failed, %d refused", report.Failed, report.Refused)
	}
	return nil
}

func printSyncReport(w io.Writer, report syncReport) error {
	fmt.Fprintf(w, "Mode: %s (prices are absolute selling prices in %s)\n\n", report.Mode, report.Currency)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tCOST\tPRICE\tVARIANTS\tRESULT")
	for _, e := range report.Entries {
		result := "would sync"
		switch {
		case e.Refused != "":
			result = "refused: " + e.Refused
		case e.Error != "":
			result = "failed: " + e.Error
		case e.Result != "":
			result = e.Result
		}
		fmt.Fprintf(tw, "%d\t%s\t%.2f\t%.2f\t%d\t%s\n", e.ID, e.Name, e.Cost, e.Price, e.Variations, result)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w)
	if report.Mode == "dry-run" {
		fmt.Fprintln(w, "Dry run only. No Hertwill changes were made.")
		return nil
	}
	fmt.Fprintf(w, "Started: %d, failed: %d, refused: %d\n", report.Started, report.Failed, report.Refused)
	return nil
}

// readPriceFile reads "<hertwill id> <price>" lines. Fields may be separated
// by whitespace, tabs or a comma; blank lines and # comments are ignored.
func readPriceFile(path string) ([]priceTarget, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var targets []priceTarget
	seen := map[int]bool{}
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if i := strings.Index(text, "#"); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if text == "" {
			continue
		}
		fields := strings.Fields(strings.ReplaceAll(text, ",", " "))
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s line %d: expected \"<id> <price>\"", path, line)
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%s line %d: invalid product ID %q", path, line, fields[0])
		}
		price, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || price <= 0 {
			return nil, fmt.Errorf("%s line %d: invalid price %q", path, line, fields[1])
		}
		if seen[id] {
			return nil, fmt.Errorf("%s line %d: product %d listed twice", path, line, id)
		}
		seen[id] = true
		targets = append(targets, priceTarget{ID: id, Price: price})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New("price file has no entries")
	}
	return targets, nil
}

func parseIDList(raw string) ([]int, error) {
	var ids []int
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid product ID %q", part)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("--ids is required, e.g. --ids 8096,8097")
	}
	return ids, nil
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return "none"
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	parts := make([]string, len(sorted))
	for i, v := range sorted {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

func hertwillImportListHelp(w io.Writer) {
	fmt.Fprintln(w, "List the store's Hertwill import list, with variant counts.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill import-list [--status <STATUS>] [--contains <TEXT>] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --status text     synced, not-synced, approved, sync-failed, pending, ...")
	fmt.Fprintln(w, "                    Without --status, Hertwill returns only products not yet synced.")
	fmt.Fprintln(w, "  --contains text   Case-insensitive filter on name or SKU")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "COST is the Hertwill wholesale price. --json includes variation dropship IDs.")
}

func hertwillImportHelp(w io.Writer) {
	fmt.Fprintln(w, "Add Hertwill catalog products to the store's import list.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill import --ids <ID,ID,...> (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "IDs are Hertwill catalog product IDs (max 50). --dry-run shows which IDs are")
	fmt.Fprintln(w, "already listed and which would be added; --confirm adds them.")
}

func hertwillSyncHelp(w io.Writer) {
	fmt.Fprintln(w, "Sync import-list products to the connected store at a fixed selling price.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill sync (--id <ID> --price <PRICE> | --file <PATH>) (--dry-run | --confirm)")
	fmt.Fprintln(w, "                         [--currency EUR] [--delay-ms 1000] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "PRICE is the absolute selling price (e.g. 40.95), not a markup multiplier.")
	fmt.Fprintln(w, "Every variation gets the same price. The file has one \"<id> <price>\" per line;")
	fmt.Fprintln(w, "# comments and blank lines are ignored.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Products not in the import list or priced below cost are refused locally.")
	fmt.Fprintln(w, "--confirm keeps going after a failed product and exits non-zero if any failed.")
	fmt.Fprintln(w, "Synced products land in WooCommerce as private; this command never publishes.")
}
