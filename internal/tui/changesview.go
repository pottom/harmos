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

// rowSeg is one styled run of a row.
//
// Rows are kept as segments rather than as one rendered string because the
// cursor must not change what a row means: a row staged for deletion has to keep
// its strikethrough under the selection, and — just as important — the
// strikethrough has to stay on the name of the thing being deleted and off the
// sentence describing what will happen to it. Striking "moved to the recycle
// bin" says the move is cancelled, which is the opposite of the truth.
type rowSeg struct {
	text  string
	style lipgloss.Style
}

// changeRow is one row of the tab.
type changeRow struct {
	kind   rowKind
	segs   []rowSeg
	seq    int    // rowChange: the revert handle
	target string // rowChange / rowFolder: what folding and jumping act on
}

// text is the row's plain content, for measuring and for tests.
func (r changeRow) text() string {
	var b strings.Builder
	for _, s := range r.segs {
		b.WriteString(s.text)
	}
	return b.String()
}

// render draws the row, optionally under the cursor. The selection is a
// background: it sits behind whatever each segment already says.
func (r changeRow) render(w int, selected bool) string {
	var b strings.Builder
	used := 0
	for _, s := range r.segs {
		t := trunc(s.text, max(0, w-used))
		if t == "" {
			continue
		}
		used += dw(t)
		st := s.style
		if selected {
			st = st.Background(theme.SelBg)
		}
		b.WriteString(st.Render(t))
	}
	if selected && used < w {
		b.WriteString(lipgloss.NewStyle().Background(theme.SelBg).Render(strings.Repeat(" ", w-used)))
	}
	return b.String()
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
				out = append(out, changeRow{kind: rowHunk})
			}
			out = append(out, changeRow{kind: rowSource, segs: m.sourceHeading(g.source, w)})
			lastSource = g.source
		}

		groupID := g.source + "\x00" + g.path
		folded := m.chgFold[foldKey(rowFolder, groupID)]
		out = append(out, changeRow{
			kind:   rowFolder,
			target: groupID,
			segs:   folderHeading(g, folded, w),
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
				segs:   changeHeading(c, hunkFolded, m.alsoGoing(c), w),
			})
			if hunkFolded {
				continue
			}
			for _, l := range c.Lines {
				for _, hl := range hunkLines(l, w) {
					out = append(out, changeRow{kind: rowHunk, segs: hl})
				}
			}
			// A folder deletion takes its contents with it, and the reviewer is
			// about to approve that. One operation, but every thing it removes
			// is named here — a count is a promise the reader cannot check.
			for _, hl := range m.contentsGoing(c, w) {
				out = append(out, changeRow{kind: rowHunk, segs: hl})
			}
		}
	}
	return out
}

// sourceHeading is the top of a source's section: its icon, its name, and the
// tally of what is staged in it.
func (m Model) sourceHeading(source string, w int) []rowSeg {
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
	stat := statBar(counts)
	gap := max(1, w-dw(icon+source)-dw(plainStat(counts))-1)
	return append([]rowSeg{
		{" " + icon + source, theme.Strong},
		{strings.Repeat(" ", gap), theme.Faded},
	}, stat...)
}

// statBar is the +2 ~1 -2 tally, in the three change colours.
//
// Reading the shape of a session before reading its detail is the first thing
// anyone does with a diff, so it is on the source heading and on the panel's
// border, and nowhere does it appear as a number without its marker.
func statBar(counts map[edit.State]int) []rowSeg {
	var out []rowSeg
	for _, st := range []edit.State{edit.New, edit.Modified, edit.Moved, edit.Deleted} {
		n := counts[st]
		if n == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, rowSeg{" ", theme.Faded})
		}
		// Literal +/~/- here rather than the row icons: this is a tally, and a
		// glyph a font may not have would read as a blank.
		out = append(out, rowSeg{fmt.Sprintf("%s%d", statMarker(st), n), changeStyleOf(st).Strikethrough(false)})
	}
	return out
}

// plainStat is the tally's width, for laying a row out before rendering it.
func plainStat(counts map[edit.State]int) string {
	var b strings.Builder
	for _, s := range statBar(counts) {
		b.WriteString(s.text)
	}
	return b.String()
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
func folderHeading(g changeGroup, folded bool, w int) []rowSeg {
	crumb := strings.ReplaceAll(g.path, "/", " › ")
	if crumb == "" {
		crumb = "/" // the source's own root group
	}
	marker := "▾"
	if folded {
		marker = "▸"
	}
	out := []rowSeg{
		{"   " + marker + " ", theme.Faded},
		{trunc(crumb, max(1, w-6)), theme.Dimmed},
	}
	if folded {
		out = append(out, rowSeg{"  " + plural(len(g.items), "change", "changes"), theme.Faded})
	}
	return out
}

// changeHeading is one staged item: its marker, its name, and what will happen.
func changeHeading(c edit.Change, folded bool, extra string, w int) []rowSeg {
	style, marker := changeStyle(c.State)

	detail := c.Detail
	if detail == "" && len(c.Lines) > 0 {
		detail = plural(len(c.Lines), "field", "fields")
	}
	if folded {
		// Folded away, the contents cannot be read, so say how much is under
		// there. Open, the list itself says it, and a count as well would be two
		// answers to one question.
		detail += extra + " ▸"
	}

	// The name wears the state — struck through for a deletion, because the name
	// is the thing being deleted. The summary never does: it describes what will
	// happen, and striking it through says it will not.
	name := marker + " " + c.Title
	lead := "     "
	gap := max(1, w-dw(lead)-dw(name)-dw(detail))
	return []rowSeg{
		{lead, theme.Faded},
		{trunc(name, max(1, w-dw(lead)-dw(detail)-1)), style},
		{strings.Repeat(" ", gap), theme.Faded},
		{detail, theme.Faded.Strikethrough(false)},
	}
}

// hunkLines renders one field's change in git's shape: the field named once,
// then what left and what arrived.
func hunkLines(l edit.Line, w int) [][]rowSeg {
	const indent = "       "
	inner := max(8, w-len(indent)-4)

	head := []rowSeg{{indent, theme.Faded}, {l.Field, theme.Dimmed}}
	if len(l.Text) > 0 {
		out := [][]rowSeg{head}
		for _, t := range l.Text {
			out = append(out, append([]rowSeg{{indent + "  ", theme.Faded}}, textLine(t, inner)...))
		}
		return out
	}

	// A protected value is masked to a constant, so before and after render
	// identically: two lines of the same bullets say nothing. One line says the
	// only true thing there is to say about a secret in a diff.
	if l.Old == l.New && l.New != "" {
		return [][]rowSeg{append(head, rowSeg{"  changed", theme.Noted})}
	}

	var out [][]rowSeg
	if l.Op != edit.LineAdded && l.Old != "" {
		out = append(out, []rowSeg{{indent + "  ", theme.Faded}, {"- ", theme.Bad}, {trunc(l.Old, inner), theme.Dimmed}})
	}
	if l.Op != edit.LineRemoved && l.New != "" {
		out = append(out, []rowSeg{{indent + "  ", theme.Faded}, {"+ ", theme.Ok}, {trunc(l.New, inner), theme.Strong}})
	}
	if len(out) == 0 { // a field emptied or filled with nothing visible
		out = append(out, []rowSeg{{indent + "  (empty)", theme.Faded}})
	}
	return append([][]rowSeg{head}, out...)
}

// textLine is one line of a multi-line field's diff.
func textLine(t edit.TextLine, w int) []rowSeg {
	switch t.Op {
	case edit.LineAdded:
		return []rowSeg{{"+ ", theme.Ok}, {trunc(t.Text, w), theme.Ok}}
	case edit.LineRemoved:
		return []rowSeg{{"- ", theme.Bad}, {trunc(t.Text, w), theme.Bad}}
	default:
		if strings.HasPrefix(t.Text, "⋯") {
			return []rowSeg{{"  " + trunc(t.Text, w), theme.Faded}}
		}
		return []rowSeg{{"  ", theme.Faded}, {trunc(t.Text, w), theme.Dimmed}}
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
	var b strings.Builder
	for _, seg := range statBar(counts) {
		b.WriteString(seg.style.Render(seg.text))
	}
	if len(sources) > 1 {
		b.WriteString(theme.Faded.Render(fmt.Sprintf(" · %d sources", len(sources))))
	}
	return b.String()
}

// renderChangeRows draws the visible window, marking the selected row.
func (m Model) renderChangeRows(rows []changeRow, w, start, count int) []string {
	cursor := m.chgCursor(rows)
	var out []string
	for i := start; i < min(start+count, len(rows)); i++ {
		out = append(out, rows[i].render(w, i == cursor))
	}
	return out
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

// contentsGoing lists what a folder deletion removes, so the write confirmation
// is approving something the reader has actually seen.
//
// A count is a promise the reader cannot check. These rows are not selectable
// and carry no marker: they are consequences of one staged decision, not
// decisions of their own, and there is nothing here to revert separately.
func (m Model) contentsGoing(c edit.Change, w int) [][]rowSeg {
	if c.State != edit.Deleted || !isFolderKind(c.Kind) {
		return nil
	}
	f, ok := m.folderByID(c.Target)
	if !ok {
		return nil
	}

	const indent = "       "
	del, _ := changeStyle(edit.Deleted)
	inner := max(8, w-len(indent)-6)

	var items []string
	for _, mf := range m.mergedFolders {
		if mf.Source == c.Source && strings.HasPrefix(mf.Path, f.Path+"/") {
			items = append(items, strings.TrimPrefix(mf.Path, f.Path+"/")+"/")
		}
	}
	sort.Strings(items)

	var entries []string
	for _, e := range m.mergedEntries {
		if e.Source != c.Source {
			continue
		}
		if e.Path == f.Path || strings.HasPrefix(e.Path, f.Path+"/") {
			label := e.Title
			if rel := strings.TrimPrefix(e.Path, f.Path); rel != "" {
				label = strings.TrimPrefix(rel, "/") + " › " + e.Title
			}
			entries = append(entries, label)
		}
	}
	sort.Strings(entries)
	items = append(items, entries...)

	if len(items) == 0 {
		return [][]rowSeg{{{indent + "  (empty)", theme.Faded}}}
	}

	// Long folders are listed up to a point and then counted: past a screenful
	// the list stops informing and starts hiding the rest of the review.
	const most = 12
	var out [][]rowSeg
	for i, it := range items {
		if i == most && len(items) > most+1 {
			out = append(out, []rowSeg{{indent + "  ⋯ and " + plural(len(items)-most, "more thing", "more things"), theme.Faded}})
			break
		}
		out = append(out, []rowSeg{
			{indent + "  ", theme.Faded},
			{trunc(it, inner), del},
		})
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
