package edit

import (
	"strings"
	"testing"
)

// render is the diff as a reader sees it: one line per entry, marker first.
func render(lines []TextLine) string {
	var b strings.Builder
	for _, l := range lines {
		switch l.Op {
		case LineAdded:
			b.WriteString("+ ")
		case LineRemoved:
			b.WriteString("- ")
		default:
			b.WriteString("  ")
		}
		b.WriteString(l.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func TestDiffLinesShowsWhatMoved(t *testing.T) {
	before := "rotated 2026-05-01\ncontact: alice@example\nticket INF-4412"
	after := "rotated 2026-05-01\ncontact: bob@example\nticket INF-4412\nexpires end of Q3"

	got := render(diffLines(before, after))
	want := "" +
		"  rotated 2026-05-01\n" +
		"- contact: alice@example\n" +
		"+ contact: bob@example\n" +
		"  ticket INF-4412\n" +
		"+ expires end of Q3\n"
	if got != want {
		t.Errorf("diff:\n%s\nwant:\n%s", got, want)
	}
}

// Long stretches nobody touched are the bulk of a note and the least
// interesting part of its diff.
func TestDiffLinesElidesUnchangedRuns(t *testing.T) {
	var before, after []string
	before = append(before, "head")
	after = append(after, "head")
	for i := range 10 {
		line := strings.Repeat("x", i+1)
		before = append(before, line)
		after = append(after, line)
	}
	before = append(before, "tail")
	after = append(after, "tail changed")

	got := render(diffLines(strings.Join(before, "\n"), strings.Join(after, "\n")))
	// Everything above the two context lines collapses into one: the head plus
	// the eight untouched middle lines.
	if !strings.Contains(got, "⋯ 9 unchanged lines") {
		t.Errorf("the untouched middle should be elided:\n%s", got)
	}
	if !strings.Contains(got, "- tail\n") || !strings.Contains(got, "+ tail changed\n") {
		t.Errorf("the change itself must survive:\n%s", got)
	}
	// Two lines of context either side of the change, and no more.
	if strings.Count(got, "\n") > 8 {
		t.Errorf("too much context kept:\n%s", got)
	}
}

func TestDiffLinesDeclinesWhenItWouldNotHelp(t *testing.T) {
	if got := diffLines("one line", "another line"); got != nil {
		t.Errorf("a single line reads better as old → new, got %v", got)
	}
	if got := diffLines("same\ntext", "same\ntext"); got != nil {
		t.Errorf("identical values have no diff, got %v", got)
	}
	huge := strings.Repeat("line\n", diffLineLimit+1)
	if got := diffLines(huge, huge+"more"); got != nil {
		t.Error("a pasted block past the limit must be summarised, not diffed on the update loop")
	}
}

// The Notes field is where multi-line values actually live, so the wiring from
// a draft to a hunk is worth pinning.
func TestNotesDiffReachesTheChange(t *testing.T) {
	before := Draft{Title: "t", Notes: "alpha\nbeta\ngamma"}
	after := Draft{Title: "t", Notes: "alpha\nBETA\ngamma"}

	var notes *Line
	for i, l := range diffDrafts(&before, &after) {
		if l.Field == "Notes" {
			notes = &diffDrafts(&before, &after)[i]
		}
	}
	if notes == nil {
		t.Fatal("a changed Notes field should produce a line")
	}
	if len(notes.Text) == 0 {
		t.Fatal("a multi-line Notes change should carry a line-level diff")
	}
	if !strings.Contains(render(notes.Text), "- beta\n+ BETA\n") {
		t.Errorf("the changed line should be there:\n%s", render(notes.Text))
	}
	if notes.Old == "" || notes.New == "" {
		t.Error("the one-line summary must survive for callers with no room for a hunk")
	}
}
