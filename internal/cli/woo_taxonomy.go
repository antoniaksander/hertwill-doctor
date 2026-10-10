package cli

import (
	"context"
	"flag"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"

	"github.com/antoniaksander/hertwill-doctor/internal/woocommerce"
)

type termEntry struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Parent int    `json:"parent"`
	Count  int    `json:"count"`
	// Path is the names from the top-level term down to this one.
	Path  string `json:"path"`
	Depth int    `json:"-"`
}

type termReport struct {
	Kind     string      `json:"kind"`
	Contains string      `json:"contains,omitempty"`
	Terms    []termEntry `json:"terms"`
	Count    int         `json:"count"`
}

// runWooTerms lists categories or brands as a tree. kind is "categories" or
// "brands". Read-only.
func runWooTerms(kind string, args []string, stdout io.Writer, g globals, client woocommerce.Client) error {
	fs := flag.NewFlagSet("woo "+kind, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	contains := fs.String("contains", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var terms []woocommerce.Category
	var err error
	if kind == "brands" {
		terms, err = client.ListBrands(context.Background())
	} else {
		terms, err = client.ListCategories(context.Background())
	}
	if err != nil {
		return err
	}
	entries := termTree(terms, strings.TrimSpace(*contains))
	report := termReport{Kind: kind, Contains: strings.TrimSpace(*contains), Terms: entries, Count: len(entries)}
	if g.json {
		return writeJSON(stdout, report)
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "No results")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%s%-5d %s (%d)\n", strings.Repeat("  ", e.Depth), e.ID, e.Name, e.Count)
	}
	fmt.Fprintf(stdout, "\n%d %s\n", len(entries), kind)
	return nil
}

// termTree orders terms depth-first by name. With a filter, it keeps the
// matching terms plus their ancestors, so each match shows its full path.
func termTree(terms []woocommerce.Category, contains string) []termEntry {
	byID := map[int]woocommerce.Category{}
	children := map[int][]woocommerce.Category{}
	for _, t := range terms {
		t.Name = html.UnescapeString(t.Name)
		byID[t.ID] = t
	}
	for _, t := range byID {
		parent := t.Parent
		if _, ok := byID[parent]; !ok {
			parent = 0
		}
		children[parent] = append(children[parent], t)
	}
	for _, list := range children {
		sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	}

	keep := map[int]bool{}
	if contains != "" {
		needle := strings.ToLower(contains)
		for _, t := range byID {
			if !strings.Contains(strings.ToLower(t.Name), needle) {
				continue
			}
			for id, seen := t.ID, 0; id != 0 && seen <= len(byID); seen++ {
				keep[id] = true
				id = byID[id].Parent
			}
		}
	}

	var out []termEntry
	var walk func(parent, depth int, path []string)
	walk = func(parent, depth int, path []string) {
		for _, t := range children[parent] {
			p := append(append([]string{}, path...), t.Name)
			if contains == "" || keep[t.ID] {
				out = append(out, termEntry{ID: t.ID, Name: t.Name, Slug: t.Slug, Parent: t.Parent, Count: t.Count, Path: strings.Join(p, " > "), Depth: depth})
			}
			if depth < len(byID) {
				walk(t.ID, depth+1, p)
			}
		}
	}
	walk(0, 0, nil)
	return out
}

func wooCategoriesHelp(w io.Writer) {
	fmt.Fprintln(w, "List WooCommerce product categories as a tree with IDs and product counts.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo categories [--contains <TEXT>] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "--contains keeps matching categories and their parents. Read-only.")
	fmt.Fprintln(w, "Use the IDs with hwd woo set-terms --categories.")
}

func wooBrandsHelp(w io.Writer) {
	fmt.Fprintln(w, "List WooCommerce product brands with IDs and product counts.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: hwd woo brands [--contains <TEXT>] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Read-only. Use the IDs with hwd woo set-terms --brands.")
}
