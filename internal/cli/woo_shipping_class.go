package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/antoniaksander/hertwill-doctor/internal/httpstats"
	"github.com/antoniaksander/hertwill-doctor/internal/woocommerce"
)

type setShippingClassReport struct {
	Mode           string           `json:"mode"`
	ProductID      string           `json:"product_id"`
	Name           string           `json:"name,omitempty"`
	Before         string           `json:"before"`
	Requested      string           `json:"requested"`
	After          *string          `json:"after,omitempty"`
	RequestSummary *httpstats.Stats `json:"request_summary,omitempty"`
}

func runWooSetShippingClass(args []string, stdout io.Writer, g globals, client woocommerce.Client, stats *httpstats.Stats) error {
	fs := flag.NewFlagSet("woo set-shipping-class", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "")
	class := fs.String("class", "", "")
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
	requested := strings.TrimSpace(*class)
	if requested == "" {
		return fmt.Errorf("--class is required (shipping class slug, or \"none\" to remove it)")
	}
	if requested == "none" {
		requested = ""
	}

	fields, err := client.ProductFields(context.Background(), *id)
	if err != nil {
		return err
	}
	report := setShippingClassReport{Mode: "dry-run", ProductID: *id, Name: fields["name"], Before: fields["shipping_class"], Requested: requested}
	if *confirm {
		report.Mode = "confirm"
		updated, err := client.UpdateProductField(context.Background(), *id, "shipping_class", requested)
		if err != nil {
			return err
		}
		after := updated["shipping_class"]
		report.After = &after
		if after != requested {
			return fmt.Errorf("WooCommerce kept shipping class %q instead of %q; check that the class slug exists", after, requested)
		}
	}

	if g.json {
		snapshot := stats.Snapshot()
		report.RequestSummary = &snapshot
		return writeJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "Product: %s %s\n", report.ProductID, report.Name)
	fmt.Fprintf(stdout, "Before:  shipping class %s\n", fallbackNone(report.Before))
	fmt.Fprintf(stdout, "Set to:  shipping class %s\n", fallbackNone(report.Requested))
	if report.After == nil {
		fmt.Fprintln(stdout, "\nDry run only. No WooCommerce changes were made.")
		return nil
	}
	fmt.Fprintf(stdout, "After:   shipping class %s\n", fallbackNone(*report.After))
	return nil
}

func wooSetShippingClassHelp(w io.Writer) {
	fmt.Fprintln(w, "Set a WooCommerce product's shipping class.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo set-shipping-class --id <ID> --class <SLUG|none> (--dry-run | --confirm) [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Only shipping_class is sent; variations without their own class inherit it.")
	fmt.Fprintln(w, "\"none\" removes the class. With --confirm the command fails if WooCommerce")
	fmt.Fprintln(w, "didn't apply the slug (e.g. the class doesn't exist).")
}
