package edit

import (
	"fmt"
	"strings"
)

// Line-level diffing for the fields that hold more than one line — Notes,
// usually, and any custom field somebody has pasted a block into.
//
// A field like that is where the interesting edits live: a note gains a line,
// loses a contact, keeps twelve lines nobody touched. Reporting that as
// "Notes: 12 lines → 14 lines" is technically true and useless. What a reviewer
// needs is the two lines that moved, which is exactly what a diff is for.

// TextLine is one line of a multi-line field's diff: added, removed, or context.
type TextLine struct {
	Op   LineOp // LineAdded, LineRemoved, or zero for an unchanged line
	Text string
}

// diffContext is how many unchanged lines are kept either side of a change.
// Two is enough to place a change in a note; more is just the note again.
const diffContext = 2

// diffLineLimit is the point past which a field is summarised rather than
// diffed. The comparison below is quadratic in the line count, and this runs on
// the update loop every time the tab is drawn; a pasted certificate is not worth
// stalling the interface over.
const diffLineLimit = 400

// splitLines splits a field's value the way a reader sees it, tolerating CRLF.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// diffLines is the line-level comparison, with unchanged runs collapsed.
//
// It returns nil when there is nothing worth showing line by line — either side
// too large, or no line-level difference at all — and the caller falls back to
// the one-line summary.
func diffLines(before, after string) []TextLine {
	a, b := splitLines(before), splitLines(after)
	if len(a) > diffLineLimit || len(b) > diffLineLimit {
		return nil
	}
	if len(a) <= 1 && len(b) <= 1 {
		return nil // a single line reads better as old → new
	}
	return withContext(lcsDiff(a, b))
}

// lcsDiff is the classic longest-common-subsequence diff: the lines both sides
// share, in order, are context; everything else is a removal or an addition.
func lcsDiff(a, b []string) []TextLine {
	// lengths[i][j] is the LCS length of a[i:] and b[j:].
	lengths := make([][]int, len(a)+1)
	for i := range lengths {
		lengths[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lengths[i][j] = lengths[i+1][j+1] + 1
			} else {
				lengths[i][j] = max(lengths[i+1][j], lengths[i][j+1])
			}
		}
	}

	var out []TextLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, TextLine{Text: a[i]})
			i, j = i+1, j+1
		case lengths[i+1][j] >= lengths[i][j+1]:
			out = append(out, TextLine{Op: LineRemoved, Text: a[i]})
			i++
		default:
			out = append(out, TextLine{Op: LineAdded, Text: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, TextLine{Op: LineRemoved, Text: a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, TextLine{Op: LineAdded, Text: b[j]})
	}
	return out
}

// withContext keeps the changed lines and a little of what surrounds them,
// replacing every longer unchanged run with a line saying how much was skipped.
//
// Returns nil when nothing changed, so the caller can tell "identical" from
// "changed but every line is context".
func withContext(all []TextLine) []TextLine {
	keep := make([]bool, len(all))
	changed := false
	for i, l := range all {
		if l.Op == 0 {
			continue
		}
		changed = true
		for j := max(0, i-diffContext); j <= min(len(all)-1, i+diffContext); j++ {
			keep[j] = true
		}
	}
	if !changed {
		return nil
	}

	var out []TextLine
	for i := 0; i < len(all); i++ {
		if keep[i] {
			out = append(out, all[i])
			continue
		}
		run := 0
		for i+run < len(all) && !keep[i+run] {
			run++
		}
		word := "lines"
		if run == 1 {
			word = "line"
		}
		out = append(out, TextLine{Text: fmt.Sprintf("⋯ %d unchanged %s", run, word)})
		i += run - 1
	}
	return out
}
