package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// printTable writes rows as an aligned, kubectl-style table: uppercase headers
// (unless suppressed) and columns padded to line up. Cells are joined by tabs
// and aligned by a tabwriter, so the on-screen separator is spaces.
func printTable(out io.Writer, headers []string, rows [][]string, showHeaders bool) {
	tw := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	if showHeaders {
		_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, r := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	_ = tw.Flush()
}
