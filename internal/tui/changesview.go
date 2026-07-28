package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/theme"
)

// The Changes tab's body.
//
// Two things are being read at once here, and the layout has to serve both. The
// first is "where in my vault did I touch something", which is a tree — the same
// source → folder → item shape the Vault tab shows, because that is the shape
// the user's memory of their own vault has. The second is "what exactly did I
// change", which is a diff, and a diff has an established visual language: a
// marker column, a colour per kind, removals above additions, unchanged context
// kept thin.
//
// So: the tree provides the headings and the indentation, and underneath each
// changed item sits a hunk in git's own idiom. Folding (z) hides a hunk or a
// whole folder, because on a big session the shape matters more than the detail
// until you go looking for it.

// rowKind is what a rendered row is, which decides whether the cursor can land
// on it and what the keys do there.
type rowKind uint8

const (
	rowSource rowKind = iota // a source heading
	rowFolder                // a folder path heading under a source
	rowChange                // one staged change: the cursor's real target
	rowHunk                  // a line of a change's diff
)

// changeRow is one row of the tab, already rendered.
type changeRow struct {
	kind   rowKind
	text   string
	seq    int    // rowChange: the revert handle
	target string // rowChange / rowFolder: what folding and jumping act on
}

// selectable reports whether the cursor stops on this row.
func (r changeRow) selectable() bool { return r.kind == rowChange || r.kind == rowFolder }

// changeGroup is the changes that live in one folder of one source.
type changeGroup struct {
	source string
	path   string // folder path within the source, "" for the source root
	items  []edit.Change
}

// groupChanges arranges the staged set into the vault's own hierarchy.
//
// The path comes from the model rather than from the change set, because
// internal/edit deliberately knows nothing about where anything lives — it holds
// identities and operations. Resolving one to a path is a question about the
// vault, and the vault is here.
func (m Model) groupChanges() []changeGroup {
	byKey := map[string]*changeGroup{}
	var order []string

	for _, c := range m.chg.Diff() {
		path := m.pathOfChange(c)
		key := c.Source + "\x00" + path
		g, ok := byKey[key]
		if !ok {
			g = &changeGroup{source: c.Source, path: path}
			byKey[key] = g
			order = append(order, key)
		}
		g.items = append(g.items, c)
	}

	sort.SliceStable(order, func(i, j int) bool {
		a, b := byKey[order[i]], byKey[order[j]]
		if a.source != b.source {
			return a.source < b.source
		}
		return a.path < b.path
	})

	out := make([]changeGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// pathOfChange is the folder a change should be filed under: for an entry, the
// folder holding it; for a folder, the folder holding *that*, so the folder
// itself reads as the item rather than as its own heading.
func (m Model) pathOfChange(c edit.Change) string {
	if c.Kind == edit.CreateGroup || c.Kind == edit.DeleteGroup || c.Kind == edit.RenameGroup || c.Kind == edit.MoveGroup {
		if f, ok := m.folderByID(c.Target); ok {
			return parentPath(f.Path)
		}
		if f, ok := m.folderByID(c.Parent); ok {
			return f.Path
		}
		return ""
	}
	for _, e := range m.mergedEntries {
		if e.ID == c.Target {
			return e.Path
		}
	}
	if f, ok := m.folderByID(c.Parent); ok {
		return f.Path
	}
	return ""
}

func (m Model) folderByID(id string) (folder, bool) {
	if id == "" {
		return folder{}, false
	}
	for _, f := range m.mergedFolders {
		if f.ID == id {
			return folder{Path: f.Path, Name: f.Name}, true
		}
	}
	return folder{}, false
}

// folder is the little of vault.Folder this file needs.
type folder struct{ Path, Name string }

func parentPath(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// foldKey identifies what a fold applies to: a change's hunk, or a folder group.
func foldKey(kind rowKind, id string) string {
	if kind == rowFolder {
		return "g\x00" + id
	}
	return "c\x00" + id
}

// changeRows builds every row of the tab, in order.
func (m Model) changeRows(w int) []changeRow {
	// One column of air on each side: rows that end on the border read as
	// clipped even when they are not.
	w = max(1, w-2)

	var out []changeRow
	lastSource := ""

	for _, g := range m.groupChanges() {
		if g.source != lastSource {
			if lastSource != "" {
				out = append(out, changeRow{kind: rowHunk, text: ""})
			}
			out = append(out, changeRow{kind: rowSource, text: " " + m.sourceHeading(g.source, w)})
			lastSource = g.source
		}

		groupID := g.source + "\x00" + g.path
		folded := m.chgFold[foldKey(rowFolder, groupID)]
		out = append(out, changeRow{
			kind:   rowFolder,
			target: groupID,
			text:   " " + folderHeading(g, folded, w),
		})
		if folded {
			continue
		}

		for _, c := range g.items {
			hunkFolded := m.chgFold[foldKey(rowChange, c.Target)]
			out = append(out, changeRow{
				kind:   rowChange,
				seq:    c.Seq,
				target: c.Target,
				text:   " " + changeHeading(c, m.alsoGoing(c), hunkFolded, w),
			})
			if hunkFolded {
				continue
			}
			for _, l := range c.Lines {
				for _, hl := range hunkLines(l, w) {
					out = append(out, changeRow{kind: rowHunk, text: " " + hl})
				}
			}
		}
	}
	return out
}

// sourceHeading is the top of a source's section: its icon, its name, and the
// tally of what is staged in it.
func (m Model) sourceHeading(source string, w int) string {
	counts := map[edit.State]int{}
	for _, c := range m.chg.Diff() {
		if c.Source == source {
			counts[c.State]++
		}
	}
	icon := ""
	if si, ok := m.sourceIcon(source); ok {
		icon = si + " "
	}
	left := theme.Strong.Render(icon + source)
	return spread(left, statBar(counts), max(1, w))
}

// statBar is the +2 ~1 -2 tally, in the three change colours.
//
// Reading the shape of a session before reading its detail is the first thing
// anyone does with a diff, so it is on the source heading and on the panel's
// border, and nowhere does it appear as a number without its marker.
func statBar(counts map[edit.State]int) string {
	var parts []string
	for _, st := range []edit.State{edit.New, edit.Modified, edit.Moved, edit.Deleted} {
		n := counts[st]
		if n == 0 {
			continue
		}
		// Literal +/~/- here rather than the row icons: this is a tally in a
		// border, where a glyph a font may not have would read as a blank.
		parts = append(parts, changeStyleOf(st).Render(fmt.Sprintf("%s%d", statMarker(st), n)))
	}
	return strings.Join(parts, theme.Faded.Render(" "))
}

// changeStyleOf is changeStyle without its glyph, for places that supply their
// own marker.
func changeStyleOf(st edit.State) lipgloss.Style {
	style, _ := changeStyle(st)
	return style
}

// statMarker is the diff alphabet: what git would print in the first column.
func statMarker(st edit.State) string {
	switch st {
	case edit.New:
		return "+"
	case edit.Deleted:
		return "-"
	default:
		return "~"
	}
}

// folderHeading is the breadcrumb a group of changes sits under.
func folderHeading(g changeGroup, folded bool, w int) string {
	crumb := strings.ReplaceAll(g.path, "/", " › ")
	if crumb == "" {
		crumb = "/" // the source's own root group
	}
	marker := "▾"
	if folded {
		marker = "▸"
	}
	line := "  " + theme.Faded.Render(marker) + " " + theme.Dimmed.Render(crumb)
	if folded {
		line += theme.Faded.Render("  " + plural(len(g.items), "change", "changes"))
	}
	return trunc(line, max(1, w))
}

// changeHeading is one staged item: its marker, its name, and what will happen.
func changeHeading(c edit.Change, extra string, folded bool, w int) string {
	style, marker := changeStyle(c.State)
	name := style.Render(marker + " " + c.Title)

	detail := c.Detail
	if detail == "" && len(c.Lines) > 0 {
		detail = plural(len(c.Lines), "field", "fields")
		if folded {
			detail += ", folded"
		}
	}
	left := "    " + name
	right := theme.Faded.Render(detail + extra)
	return spread(trunc(left, max(1, w-dw(right)-1)), right, max(1, w))
}

// hunkLines renders one field's change in git's shape: the field named once,
// then what left and what arrived.
func hunkLines(l edit.Line, w int) []string {
	const indent = "      "
	inner := max(8, w-len(indent)-4)

	head := indent + theme.Dimmed.Render(l.Field)
	if len(l.Text) > 0 {
		out := []string{head}
		for _, t := range l.Text {
			out = append(out, indent+"  "+textLine(t, inner))
		}
		return out
	}

	// A protected value is masked to a constant, so before and after render
	// identically: two lines of the same bullets say nothing. One line says the
	// only true thing there is to say about a secret in a diff.
	if l.Old == l.New && l.New != "" {
		return []string{head + "  " + theme.Noted.Render("changed")}
	}

	var out []string
	if l.Op != edit.LineAdded && l.Old != "" {
		out = append(out, indent+"  "+theme.Bad.Render("- ")+theme.Dimmed.Render(trunc(l.Old, inner)))
	}
	if l.Op != edit.LineRemoved && l.New != "" {
		out = append(out, indent+"  "+theme.Ok.Render("+ ")+theme.Strong.Render(trunc(l.New, inner)))
	}
	if len(out) == 0 { // a field emptied or filled with nothing visible
		out = append(out, indent+"  "+theme.Faded.Render("(empty)"))
	}
	return append([]string{head}, out...)
}

// textLine is one line of a multi-line field's diff.
func textLine(t edit.TextLine, w int) string {
	switch t.Op {
	case edit.LineAdded:
		return theme.Ok.Render("+ ") + theme.Ok.Render(trunc(t.Text, w))
	case edit.LineRemoved:
		return theme.Bad.Render("- ") + theme.Bad.Render(trunc(t.Text, w))
	default:
		if strings.HasPrefix(t.Text, "⋯") {
			return theme.Faded.Render("  " + trunc(t.Text, w))
		}
		return theme.Faded.Render("  ") + theme.Dimmed.Render(trunc(t.Text, w))
	}
}

// selectableRows are the indices of the rows the cursor can stop on.
func selectableRows(rows []changeRow) []int {
	var out []int
	for i, r := range rows {
		if r.selectable() {
			out = append(out, i)
		}
	}
	return out
}

// changesPanelInfo is the tally shown on the panel border.
func (m Model) changesPanelInfo() string {
	counts := map[edit.State]int{}
	sources := map[string]bool{}
	for _, c := range m.chg.Diff() {
		counts[c.State]++
		sources[c.Source] = true
	}
	if len(counts) == 0 {
		return m.chg.Summary() // "nothing pending" — the border says so even when empty
	}
	bar := statBar(counts)
	if len(sources) > 1 {
		bar += theme.Faded.Render(fmt.Sprintf(" · %d sources", len(sources)))
	}
	return bar
}

// renderChangeRows draws the visible window, marking the selected row.
func (m Model) renderChangeRows(rows []changeRow, w, start, count int) []string {
	var out []string
	for i := start; i < min(start+count, len(rows)); i++ {
		r := rows[i]
		if i == m.chgCursor(rows) {
			st := theme.SelRow.Width(w)
			if r.kind == rowChange {
				st = selRowStyle(st, m.chg.StateOf(r.target))
			}
			out = append(out, st.Render(trunc(ansiStrip(r.text), w)))
			continue
		}
		out = append(out, r.text)
	}
	return out
}

// firstChangeSel is where the cursor starts: the first actual change, not the
// folder heading above it. The headings are selectable so they can be folded,
// but nobody opens this tab to look at a heading.
func (m Model) firstChangeSel(rows []changeRow) int {
	for i, idx := range selectableRows(rows) {
		if rows[idx].kind == rowChange {
			return i
		}
	}
	return 0
}

// chgCursor is the row index the cursor is on, given m.chgSel counts only
// selectable rows.
func (m Model) chgCursor(rows []changeRow) int {
	sel := selectableRows(rows)
	if len(sel) == 0 {
		return -1
	}
	return sel[clampIndex(m.chgSel, len(sel))]
}

// groupOf is the folder heading a change row belongs to — the nearest heading
// above it, which is how the rows were built in the first place.
func (m Model) groupOf(rows []changeRow, target changeRow) string {
	group := ""
	for _, r := range rows {
		if r.kind == rowFolder {
			group = r.target
		}
		if r.kind == rowChange && r.target == target.target {
			return group
		}
	}
	return group
}

// contentW is the width a full-width panel gives its content. The key handlers
// build the same rows the view does, so they must measure them the same way.
func (m Model) contentW() int { return max(1, m.w-2) }

// What a folder deletion takes with it.
//
// Deleting a folder stages one operation, deliberately: one thing was decided,
// so there is one thing to undo, and exploding it into an operation per child
// would make reverting it a scavenger hunt. But the contents are going, and a
// screen that shows the folder struck through while its entries sit there in
// ordinary type is telling the reader something false.
//
// So the deletion is one operation and the whole subtree wears it. Only the
// folder carries the marker, because only the folder is staged.

// doomedPrefixes is, per source, the folder paths staged for deletion.
func (m Model) doomedPrefixes() map[string][]string {
	if m.chg.Empty() {
		return nil
	}
	out := map[string][]string{}
	for _, c := range m.chg.Diff() {
		if c.State != edit.Deleted || !isFolderKind(c.Kind) {
			continue
		}
		if f, ok := m.folderByID(c.Target); ok {
			out[c.Source] = append(out[c.Source], f.Path)
		}
	}
	return out
}

func isFolderKind(k edit.Kind) bool {
	return k == edit.CreateGroup || k == edit.DeleteGroup || k == edit.RenameGroup || k == edit.MoveGroup
}

// underDoomedFolder reports whether a path in a source sits inside one of the
// folders staged for deletion — the folder itself excluded, since it carries its
// own state.
func underDoomedFolder(doomed map[string][]string, source, path string) bool {
	for _, p := range doomed[source] {
		if strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// atOrUnderDoomedFolder is the same question including the folder itself, which
// is what an entry filed directly in it needs to ask.
func atOrUnderDoomedFolder(doomed map[string][]string, source, path string) bool {
	for _, p := range doomed[source] {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// goesWithIt counts what a folder deletion takes along, so the change can say so
// rather than leaving the reader to work it out from the tree.
func (m Model) goesWithIt(folderID string) (entries, folders int) {
	f, ok := m.folderByID(folderID)
	if !ok {
		return 0, 0
	}
	source := ""
	for _, mf := range m.mergedFolders {
		if mf.ID == folderID {
			source = mf.Source
			break
		}
	}
	for _, e := range m.mergedEntries {
		if e.Source == source && (e.Path == f.Path || strings.HasPrefix(e.Path, f.Path+"/")) {
			entries++
		}
	}
	for _, mf := range m.mergedFolders {
		if mf.Source == source && strings.HasPrefix(mf.Path, f.Path+"/") {
			folders++
		}
	}
	return entries, folders
}

// alsoGoing is the phrase appended to a folder deletion's summary.
func (m Model) alsoGoing(c edit.Change) string {
	if c.State != edit.Deleted || !isFolderKind(c.Kind) {
		return ""
	}
	entries, folders := m.goesWithIt(c.Target)
	if entries == 0 && folders == 0 {
		return " · empty"
	}
	var parts []string
	if entries > 0 {
		parts = append(parts, plural(entries, "entry", "entries"))
	}
	if folders > 0 {
		parts = append(parts, plural(folders, "folder", "folders"))
	}
	return " · with " + strings.Join(parts, " and ")
}

// pathOfNode is a tree node's folder path within its source, rebuilt from the
// folder it stands for. A source root has no path of its own.
func (m Model) pathOfNode(n *node) string {
	if n == nil || n.id == "" {
		return ""
	}
	if f, ok := m.folderByID(n.id); ok {
		return f.Path
	}
	return ""
}
