package edit

import (
	"fmt"
	"sort"
	"strings"
)

// Change is one staged change, rendered for review.
type Change struct {
	Seq    int // revert handle
	Kind   Kind
	State  State
	Source string
	Target string
	Parent string // the folder it lives in, so a reviewer can be told where
	Title  string // what to call it in a list
	Detail string // a one-line summary, e.g. "Infra → Archive"
	Lines  []Line // field-level diff, empty for a move or a delete
}

// Line is one field's before and after.
//
// Text is filled for a field that holds more than one line: the line-level diff,
// with unchanged runs collapsed. Old and New still carry the one-line summary,
// so a caller that has no room for a hunk has something to show.
type Line struct {
	Op    LineOp
	Field string
	Old   string
	New   string
	Text  []TextLine
}

// LineOp is how a field changed.
type LineOp uint8

const (
	LineAdded LineOp = iota + 1
	LineRemoved
	LineChanged
)

func (o LineOp) Marker() string {
	switch o {
	case LineAdded:
		return "+"
	case LineRemoved:
		return "-"
	case LineChanged:
		return "~"
	}
	return " "
}

// masked is what a secret looks like in a diff.
//
// No secret ever reaches a Line. The Changes tab is on screen, in a scrollback
// buffer, and in whatever the terminal has been told to log — showing a password
// there would undo the care taken everywhere else in this program to keep one off
// a screen it was not explicitly copied to. What the reviewer needs is whether it
// changed, and that is all this says.
const masked = "•••••••"

// Diff renders the staged changes for review, newest last.
func (s Set) Diff() []Change {
	var out []Change
	for _, op := range s.Effective() {
		out = append(out, changeOf(s, op))
	}
	return out
}

func changeOf(s Set, op Op) Change {
	c := Change{
		Seq:    op.Seq,
		Kind:   op.Kind,
		State:  s.StateOf(op.Target),
		Source: op.Source,
		Target: op.Target,
		Parent: parentOf(op),
		Title:  titleOf(op),
	}

	switch op.Kind {
	case CreateEntry:
		c.Lines = diffDrafts(nil, op.After)
	case EditEntry:
		c.Lines = diffDrafts(op.Before, op.After)
	case MoveEntry, MoveGroup:
		c.Detail = "moved"
	case DeleteEntry, DeleteGroup:
		if op.Perm {
			c.Detail = "deleted permanently"
		} else {
			c.Detail = "moved to the recycle bin"
		}
	case CreateGroup:
		c.Detail = "new folder"
	case RenameGroup:
		c.Detail = "renamed to " + op.Name
	}
	return c
}

func titleOf(op Op) string {
	switch {
	case op.Name != "":
		return op.Name
	case op.After != nil && op.After.Title != "":
		return op.After.Title
	case op.Before != nil && op.Before.Title != "":
		return op.Before.Title
	}
	// Never the raw target: it is an opaque, base64 identity token, and a row
	// reading "own:g:5nhn6H1wWEyWa4KEebc2rQ" tells the reader nothing they can
	// act on. A caller that stages a change owes it a name.
	return "(untitled)"
}

// diffDrafts compares two drafts field by field. A nil before means a creation,
// so every non-empty field reads as added.
func diffDrafts(before, after *Draft) []Line {
	if after == nil {
		return nil
	}
	var b Draft
	if before != nil {
		b = *before
	}

	var lines []Line
	add := func(field, old, next string) {
		if old == next {
			return
		}
		lines = append(lines, Line{Op: lineOp(old, next), Field: field, Old: old, New: next})
	}
	// A field that holds more than one line gets the line-level treatment, and
	// keeps the one-line summary for callers with no room for a hunk.
	addText := func(field, old, next string) {
		if old == next {
			return
		}
		lines = append(lines, Line{
			Op:    lineOp(old, next),
			Field: field,
			Old:   oneLine(old),
			New:   oneLine(next),
			Text:  diffLines(old, next),
		})
	}

	add("Title", b.Title, after.Title)
	add("Username", b.Username, after.Username)
	add("URL", b.URL, after.URL)
	addText("Notes", b.Notes, after.Notes)
	add("Tags", b.Tags, after.Tags)

	// Passwords and other protected values are compared, never shown.
	if b.Password.Reveal() != after.Password.Reveal() {
		lines = append(lines, Line{
			Op:    lineOp(b.Password.Reveal(), after.Password.Reveal()),
			Field: "Password",
			Old:   maskIf(b.Password.Reveal()),
			New:   maskIf(after.Password.Reveal()),
		})
	}
	if b.TOTP != after.TOTP {
		lines = append(lines, Line{
			Op:    lineOp(b.TOTP, after.TOTP),
			Field: "TOTP",
			Old:   maskIf(b.TOTP),
			New:   maskIf(after.TOTP),
		})
	}

	lines = append(lines, diffFields(b, *after)...)

	if b.Expires != after.Expires || (after.Expires && !b.ExpiryTime.Equal(after.ExpiryTime)) {
		lines = append(lines, Line{
			Op:    LineChanged,
			Field: "Expiry",
			Old:   expiryText(b),
			New:   expiryText(*after),
		})
	}
	return lines
}

// diffFields compares the custom fields by key, in a stable order.
//
// The comparison is on the raw values; the masking happens only on the way out.
// Deciding on the masked forms would silently swallow a changed secret, because
// two different secrets render identically.
func diffFields(before, after Draft) []Line {
	keys := map[string]bool{}
	for _, f := range before.Fields {
		keys[f.Key] = true
	}
	for _, f := range after.Fields {
		keys[f.Key] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var lines []Line
	for _, key := range sorted {
		oldF, hadOld := before.field(key)
		newF, hasNew := after.field(key)

		if hadOld == hasNew && oldF.Value == newF.Value && oldF.Protected == newF.Protected {
			continue // genuinely unchanged
		}

		op := LineChanged
		switch {
		case !hadOld:
			op = LineAdded
		case !hasNew:
			op = LineRemoved
		}

		lines = append(lines, Line{
			Op:    op,
			Field: key,
			Old:   display(oldF, hadOld),
			New:   display(newF, hasNew),
		})
	}
	return lines
}

// display is a field's value as the reviewer may see it: masked when the field
// is protected, so a secondary secret is no more visible here than a password.
func display(f DraftField, present bool) string {
	if !present {
		return ""
	}
	if f.Protected {
		return maskIf(f.Value)
	}
	return f.Value
}

func lineOp(old, next string) LineOp {
	switch {
	case old == "":
		return LineAdded
	case next == "":
		return LineRemoved
	}
	return LineChanged
}

func maskIf(s string) string {
	if s == "" {
		return ""
	}
	return masked
}

func expiryText(d Draft) string {
	if !d.Expires {
		return "never"
	}
	return d.ExpiryTime.Format("2006-01-02")
}

// oneLine flattens a multi-line value for a single diff row, so a long note
// cannot break the layout. The full text is still in the editor.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	const limit = 120
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}

// Summary is a one-line description of the whole set, for a footer or a
// confirmation.
func (s Set) Summary() string {
	counts := s.Counts()
	if len(counts) == 0 {
		return "nothing pending"
	}
	var parts []string
	for _, src := range s.Sources() {
		parts = append(parts, fmt.Sprintf("%s: %d", src, counts[src]))
	}
	return strings.Join(parts, ", ")
}

// parentOf is the folder a change happened in — the destination for a move or a
// creation, otherwise wherever the item lived.
func parentOf(op Op) string {
	if op.Parent != "" {
		return op.Parent
	}
	if op.After != nil && op.After.GroupID != "" {
		return op.After.GroupID
	}
	if op.Before != nil {
		return op.Before.GroupID
	}
	return ""
}
