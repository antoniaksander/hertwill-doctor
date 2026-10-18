package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/antoniaksander/hertwill-doctor/internal/woocommerce"
)

var orderDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type ordersReport struct {
	After    string              `json:"after,omitempty"`
	Statuses []string            `json:"statuses"`
	Count    int                 `json:"count"`
	Total    float64             `json:"total"`
	Orders   []woocommerce.Order `json:"orders"`
}

// runWooOrders implements `hwd woo orders`, a read-only order listing for
// sales reports. It never changes an order.
func runWooOrders(args []string, stdout io.Writer, g globals, client woocommerce.Client) error {
	fs := flag.NewFlagSet("woo orders", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	after := fs.String("after", "", "")
	status := fs.String("status", "completed,processing,on-hold", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *after != "" && !orderDatePattern.MatchString(*after) {
		return fmt.Errorf("--after must be a date like 2026-10-01")
	}
	var statuses []string
	for _, s := range strings.Split(*status, ",") {
		if s = strings.TrimSpace(s); s != "" {
			statuses = append(statuses, s)
		}
	}

	orders, err := client.ListOrders(context.Background(), *after, statuses)
	if err != nil {
		return err
	}
	report := ordersReport{After: *after, Statuses: statuses, Count: len(orders), Orders: orders}
	for _, o := range orders {
		if v, err := strconv.ParseFloat(o.Total, 64); err == nil {
			report.Total += v
		}
	}
	if report.Orders == nil {
		report.Orders = []woocommerce.Order{}
	}
	if g.json {
		return writeJSON(stdout, report)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DATE\tORDER\tSTATUS\tCOUNTRY\tTOTAL\tCOUPON\tITEMS")
	for _, o := range orders {
		var items []string
		for _, it := range o.Items {
			items = append(items, fmt.Sprintf("%dx %s", it.Quantity, it.Name))
		}
		date := o.DateCreated
		if len(date) >= 10 {
			date = date[:10]
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\n", date, o.ID, o.Status, o.Country, o.Total,
			strings.Join(o.Coupons, "+"), strings.Join(items, "; "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n%d order(s), total %.2f\n", report.Count, report.Total)
	return nil
}

func wooOrdersHelp(w io.Writer) {
	fmt.Fprintln(w, "List WooCommerce orders for sales reports (read-only).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo orders [--after <YYYY-MM-DD>] [--status <s1,s2,...>] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Default statuses: completed,processing,on-hold. --json includes line items")
	fmt.Fprintln(w, "(product/variation IDs, SKU, quantity, line total excl. tax) and coupon codes.")
	fmt.Fprintln(w, "No order is ever changed.")
}
