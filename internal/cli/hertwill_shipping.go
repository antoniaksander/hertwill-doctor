package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/antoniaksander/hertwill-doctor/internal/hertwill"
)

type brandShippingResult struct {
	BrandID int                          `json:"brand_id"`
	Lists   []hertwill.ShippingPriceList `json:"lists,omitempty"`
	Error   string                       `json:"error,omitempty"`
}

func runHertwillBrandShipping(args []string, stdout io.Writer, g globals, client hertwill.Client) error {
	fs := flag.NewFlagSet("hertwill brand-shipping", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	brands := fs.String("brands", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids, err := parseBrandIDs(*brands)
	if err != nil {
		return err
	}
	results := make([]brandShippingResult, 0, len(ids))
	failed := 0
	for _, id := range ids {
		lists, err := client.BrandShipping(context.Background(), id)
		result := brandShippingResult{BrandID: id, Lists: lists}
		if err != nil {
			result.Error = err.Error()
			failed++
		}
		results = append(results, result)
	}
	if g.json {
		if err := writeJSON(stdout, results); err != nil {
			return err
		}
	} else {
		printBrandShipping(stdout, results)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d brands failed", failed, len(ids))
	}
	return nil
}

func parseBrandIDs(value string) ([]int, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("--brands is required (comma-separated Hertwill brand IDs)")
	}
	var ids []int
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid brand ID %q", part)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("--brands is required (comma-separated Hertwill brand IDs)")
	}
	return ids, nil
}

// printBrandShipping prints one line per price list: brand, list name,
// per-order/per-item and the destinations it ships to with their price.
func printBrandShipping(w io.Writer, results []brandShippingResult) {
	for _, result := range results {
		if result.Error != "" {
			fmt.Fprintf(w, "%d\terror: %s\n", result.BrandID, result.Error)
			continue
		}
		if len(result.Lists) == 0 {
			fmt.Fprintf(w, "%d\tno shipping price lists\n", result.BrandID)
			continue
		}
		for _, list := range result.Lists {
			mode := "per-order"
			if list.PerItem {
				mode = "per-item"
			}
			prices := make([]string, 0, len(list.Prices))
			for _, price := range list.Prices {
				if price.Price == nil || price.Price.Amount <= 0 {
					continue
				}
				prices = append(prices, fmt.Sprintf("%s=%.2f", price.Destination, price.Price.Amount))
			}
			sort.Strings(prices)
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", result.BrandID, list.Name, mode, strings.Join(prices, " "))
		}
	}
}

func hertwillBrandShippingHelp(w io.Writer) {
	fmt.Fprintln(w, "Show brands' shipping price lists (per-country delivery rates).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd hertwill brand-shipping --brands <ID,ID,...> [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Reads GET /v1/brands/{id}/shipping-price-lists for each Hertwill brand ID.")
	fmt.Fprintln(w, "One line per price list: brand ID, list name, per-order or per-item, and")
	fmt.Fprintln(w, "COUNTRY=price for every destination with a price (EUR, excl. VAT). Read-only.")
}
