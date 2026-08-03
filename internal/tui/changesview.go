package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
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
// its delete colour under the selection, and — just as important — that colour
// has to stay on the name of the thing being deleted and off the sentence
// describing what will happen to it. A summary dressed as deleted says the
// deletion is cancelled, which is the opposite of the truth.
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
//
// Every row with something on it. The cursor used to stop only on changes and
// headings, which meant ↑↓ jumped over the hunks and over everything a folder
// deletion takes with it — the reader could reach those rows with the wheel and
// the page keys but not with the arrows, which is not a distinction anybody
// expects to have to know. What the keys act on is a separate question: see
// contextRow.
func (r changeRow) selectable() bool { return strings.TrimSpace(r.text()) != "" }

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
	for _, e := range m.viewEntries {
		if e.ID == c.Target {
			return e.Path
		}
	}
	if f, ok := m.folderByID(c.Parent); ok {
		return f.Path
	}
	return ""
}

// readablePath is a folder's path as a reader should see it, by ID.
func (m Model) readablePath(id string) string {
	if f, ok := m.folderByID(id); ok {
		return readable(f.Path)
	}
	return ""
}

func (m Model) folderByID(id string) (folder, bool) {
	if id == "" {
		return folder{}, false
	}
	for _, f := range m.viewFolders {
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
				segs:   changeHeading(m.resolve(c), hunkFolded, m.alsoGoing(c), w),
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

// resolve fills a change's name and summary from the vault, so the row says
// what was staged rather than what the operation happened to record.
func (m Model) resolve(c edit.Change) edit.Change {
	c.Title, c.Detail = m.nameOf(c), m.detailOf(c)
	return c
}

// typeIcon says what kind of thing a change is about.
func typeIcon(k edit.Kind) string {
	i := ic()
	if isFolderKind(k) {
		return i.folder
	}
	return i.entry
}

// statMarker is the diff alphabet: what git would print in the first column.
func statMarker(st edit.State) string {
	switch st {
	case edit.New:
		return "+"
	case edit.Deleted:
		return "-"
	case edit.Moved:
		return "→" // not "~": a move and an edit are different things
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
	// The tree's own open/closed folder glyphs, not a hardcoded triangle: in the
	// plain icon set the triangle *is* the folder glyph, so a literal one here
	// meant two different things wore the same mark on the same screen.
	i := ic()
	marker := i.folderOpen
	if folded {
		marker = i.folder
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

// nameOf is what to call a change on screen: the name the vault will show for
// it, not the one the operation happened to record.
//
// The two can differ. Rename a folder, then delete it, and the rename no longer
// happens — it is reduced away — but the delete operation still carries the name
// that was on screen when it was staged. The tree would say "Net" and the review
// "Network" about the same folder. The projection is the one that is going to be
// true, so it wins; the operation's own name is the fallback for anything the
// projection cannot see.
func (m Model) nameOf(c edit.Change) string {
	if n := m.nameOfTarget(c.Target, isFolderKind(c.Kind)); n != "" {
		return n
	}
	if c.Title != "" && c.Title != "(untitled)" {
		return c.Title
	}
	return c.Title
}

// nameOfTarget looks up an entry's title or a folder's name by ID.
func (m Model) nameOfTarget(id string, isFolder bool) string {
	if isFolder {
		if f, ok := m.folderByID(id); ok {
			return f.Name
		}
		return ""
	}
	for _, e := range m.viewEntries {
		if e.ID == id {
			return e.Title
		}
	}
	return ""
}

// detailOf is the sentence beside a change. A move says where from and where to:
// "moved" alone is the half of it the reader already knew, and either half on
// its own leaves them to work out the other.
func (m Model) detailOf(c edit.Change) string {
	if c.Kind == edit.MoveEntry || c.Kind == edit.MoveGroup {
		if f, ok := m.folderByID(c.Parent); ok {
			to := readable(f.Path)
			if from := readable(c.Was); from != "" {
				return from + " → " + to
			}
			return "moved to " + to
		}
	}
	return c.Detail
}

// readable is a stored path as a reader should see it. The separator is a
// chevron everywhere a path is shown to somebody, so a folder whose own name
// contains a slash cannot pass for two folders.
func readable(path string) string { return strings.ReplaceAll(path, "/", " › ") }

// changeHeading is one staged item: its marker, its name, and what will happen.
func changeHeading(c edit.Change, folded bool, extra string, w int) []rowSeg {
	style, marker := changeStyle(c.State)

	was := renamedFrom(c)

	detail := c.Detail
	switch {
	case was != "":
		// The name half is about to say old → new. Saying it again on the right
		// is the row answering one question twice.
		detail = ""
	case len(c.Lines) == 1:
		// One changed field fits on the line, so say which and to what. "1
		// field" told the reader something changed and left the only part they
		// could not already see for them to go and unfold.
		detail = oneFieldSummary(c.Lines[0], max(8, w/2))
	case len(c.Lines) > 0:
		detail = plural(len(c.Lines), "field", "fields")
	}
	if folded {
		// Folded away, the contents cannot be read, so say how much is under
		// there. Open, the list itself says it, and a count as well would be two
		// answers to one question.
		detail += extra + " ▸"
	}

	// Two glyphs, answering two questions: the marker says what is happening,
	// the type icon says what it is happening to. Without the second, a folder
	// and an entry with the same name read identically — and they are very
	// different things to be deleting.
	//
	// The name wears the state, because the name is the thing being deleted. The
	// summary never does: it describes what will happen, and dressing it as
	// deleted says it will not.
	// The marker keeps the state's colour along with the name: it is the signal
	// a reader without colour has, and it is no use to them in the same grey as
	// the indentation.
	lead := "     "
	badge := marker + " " + typeIcon(c.Kind) + " "
	right := detailSegs(detail)
	room := max(6, w-dw(lead)-dw(badge)-dw(detail)-1) // what the name half has

	var left []rowSeg
	// A name change is written where the name is, not remarked on at the far
	// edge. It used to sit in the right margin in the faintest colour the theme
	// has, on top of the selection background — present, and unreadable. The one
	// thing a rename has to say is old → new, so the row says it, in the place
	// the eye already is. A folder's name and an entry's title are the same
	// thing to a reader, so they read the same way.
	if was != "" {
		// The old name stays legible — it is half of what the reader is here to
		// compare — and the new one takes the state's colour, so which is which
		// needs no explaining.
		from := trunc(was, max(3, room/2))
		left = []rowSeg{
			{from, theme.Dimmed},
			{" → ", theme.Noted},
			{trunc(c.Title, max(1, room-dw(from)-3)), style},
		}
	} else {
		left = []rowSeg{{trunc(c.Title, room), style}}
	}

	out := []rowSeg{{lead, theme.Faded}, {badge, style}}
	out = append(out, left...)
	used := dw(lead) + dw(badge)
	for _, s := range left {
		used += dw(s.text)
	}
	out = append(out, rowSeg{strings.Repeat(" ", max(1, w-used-dw(detail))), theme.Faded})
	return append(out, right...)
}

// renamedFrom is what this change used to be called, when that is the thing it
// mostly is. A folder rename says so outright; an entry whose only changed field
// is its Title is the same act by another name, and the reader should not have to
// know which of the two they are looking at.
func renamedFrom(c edit.Change) string {
	if c.Kind == edit.RenameGroup {
		return c.Was
	}
	if len(c.Lines) == 1 && c.Lines[0].Field == "Title" {
		return c.Lines[0].Old
	}
	return ""
}

// detailSegs styles the sentence beside a change.
//
// Dimmed, not faded: this row can be under the cursor, and the faintest colour
// in the palette on the selection background is text that is technically on
// screen. What it changed *to* takes the amber, so the outcome is the part that
// catches the eye.
func detailSegs(detail string) []rowSeg {
	base := theme.Dimmed.Strikethrough(false)
	if before, after, found := strings.Cut(detail, " → "); found {
		return []rowSeg{
			{before, base},
			{" → ", theme.Noted},
			{after, theme.Noted},
		}
	}
	return []rowSeg{{detail, base}}
}

// oneFieldSummary is a single changed field on one line: what it is, and what it
// went from and to.
//
// The values are whatever the diff put in the Line, which means a protected one
// arrives already masked — the summary can no more leak a password than the hunk
// below it can.
func oneFieldSummary(l edit.Line, w int) string {
	half := max(3, (w-dw(l.Field)-5)/2)
	old, next := trunc(l.Old, half), trunc(l.New, half)
	switch {
	case old == next && next != "":
		return l.Field + " changed" // two identical masks say nothing
	case old == "":
		return l.Field + " + " + next
	case next == "":
		return l.Field + " − " + old
	}
	return l.Field + " " + old + " → " + next
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

// contextRow is the change or heading the cursor is inside — what z, x and ↵
// act on, whether the cursor is on the change itself or on one of its lines.
func (m Model) contextRow(rows []changeRow) changeRow {
	i := m.chgCursor(rows)
	for ; i >= 0; i-- {
		if rows[i].kind == rowChange || rows[i].kind == rowFolder {
			return rows[i]
		}
	}
	return changeRow{}
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

// impactTally is the session in as few cells as it can honestly be put: a
// marker, a count, and the glyph for what is being counted.
//
// It lives on the panel border, which is where a tally belongs and where it
// costs no room. Spelled out it grows with every kind of change — "2 new
// things · 3 entries changed · 9 folders and 26 entries removed" is a sentence
// nobody re-reads, and it was the first thing to be truncated.
func (m Model) impactTally() string {
	var total writeImpact
	for _, src := range m.chg.Sources() {
		im := m.impactOf(src)
		total.newFolders += im.newFolders
		total.newEntries += im.newEntries
		total.changedFolders += im.changedFolders
		total.changedEntries += im.changedEntries
		total.movedFolders += im.movedFolders
		total.movedEntries += im.movedEntries
		total.folders += im.folders
		total.entries += im.entries
		total.permanent += im.permanent
	}

	i := ic()
	var parts []string
	count := func(st edit.State, pairs ...string) {
		style := changeStyleOf(st).Strikethrough(false)
		parts = append(parts, style.Render(statMarker(st)+strings.Join(pairs, " ")))
	}
	if n := total.newFolders + total.newEntries; n > 0 {
		count(edit.New, itoa(n))
	}
	if n := total.changedFolders + total.changedEntries; n > 0 {
		count(edit.Modified, itoa(n))
	}
	if n := total.movedFolders + total.movedEntries; n > 0 {
		count(edit.Moved, itoa(n))
	}
	if total.folders > 0 || total.entries > 0 {
		var what []string
		if total.folders > 0 {
			what = append(what, itoa(total.folders)+i.folder)
		}
		if total.entries > 0 {
			what = append(what, itoa(total.entries)+i.entry)
		}
		count(edit.Deleted, what...)
	}
	return strings.Join(parts, theme.Faded.Render(" "))
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
	out := m.impactTally()
	if len(sources) > 1 {
		out += theme.Faded.Render(fmt.Sprintf(" · %d sources", len(sources)))
	}
	return out
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
// screen that marks the folder while its entries sit there in ordinary type is
// telling the reader something false.
//
// So the deletion is one operation and the whole subtree wears it. Only the
// folder carries the marker, because only the folder is staged.

// What is going, and what is going with it.
//
// All of this works on identities. It used to work on paths, and paths are not
// identities: two folders can legitimately share one — a recycle bin routinely
// holds two folders of the same name — and then a guard keyed on the path
// refused to stage the second because it looked like it was inside the first.
// That is the same mistake the tree itself used to make.

// isFolderKind reports whether an operation is about a folder.
func isFolderKind(k edit.Kind) bool {
	return k == edit.CreateGroup || k == edit.DeleteGroup || k == edit.RenameGroup || k == edit.MoveGroup
}

// doomedFolders is the set of folder IDs staged for deletion.
func (m Model) doomedFolders() map[string]bool {
	if m.chg.Empty() {
		return nil
	}
	out := map[string]bool{}
	for _, c := range m.chg.Diff() {
		if c.State == edit.Deleted && isFolderKind(c.Kind) {
			out[c.Target] = true
		}
	}
	return out
}

// folderParents maps a folder to the folder above it, for walking upward.
//
// The link is the parent's identity. Where a caller has not supplied one — the
// same case buildTree covers, a list that carries paths and nothing else — the
// path is used to find it, which is right for data with no duplicates and no
// worse than what such a caller can express.
func (m Model) folderParents() map[string]string {
	byPath := make(map[string]string, len(m.viewFolders))
	for _, f := range m.viewFolders {
		byPath[f.Source+"\x00"+f.Path] = f.ID
	}
	out := make(map[string]string, len(m.viewFolders))
	for _, f := range m.viewFolders {
		if f.ParentID != "" {
			out[f.ID] = f.ParentID
			continue
		}
		if up := parentPath(f.Path); up != "" {
			out[f.ID] = byPath[f.Source+"\x00"+up]
		}
	}
	return out
}

// ancestorDoomed walks up from a folder and reports the first one on the way
// that is staged for deletion. The folder itself is not considered: it carries
// its own state, and "already going with itself" is not a sentence.
func (m Model) ancestorDoomed(folderID string) (string, bool) {
	doomed := m.doomedFolders()
	if len(doomed) == 0 || folderID == "" {
		return "", false
	}
	parents := m.folderParents()
	seen := map[string]bool{}
	for id := parents[folderID]; id != "" && !seen[id]; id = parents[id] {
		seen[id] = true
		if doomed[id] {
			return m.nameOfTarget(id, true), true
		}
	}
	return "", false
}

// inDoomedFolder reports whether something sits inside a folder that is staged
// for deletion, and names that folder.
//
// The design has always said a change staged underneath an already-deleted
// folder should be refused; nothing implemented it, and the results were quiet
// and bad — an entry moved into a deleted folder was silently binned with it, a
// new entry was created inside the recycle bin, and deleting something inside a
// folder already staged for deletion produced a set that could not be applied.
func (m Model) inDoomedFolder(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	// An entry hangs off its group; a folder hangs off its parent.
	for _, e := range m.viewEntries {
		if e.ID == id {
			if m.doomedFolders()[e.GroupID] {
				return m.nameOfTarget(e.GroupID, true), true
			}
			return m.ancestorDoomed(e.GroupID)
		}
	}
	return m.ancestorDoomed(id)
}

// descendantsOf is every folder and entry under a folder, by identity.
func (m Model) descendantsOf(folderID string) (folders []vault.Folder, entries []vault.Entry) {
	if folderID == "" {
		return nil, nil
	}
	parents := m.folderParents()
	under := func(id string) bool {
		seen := map[string]bool{}
		for p := parents[id]; p != "" && !seen[p]; p = parents[p] {
			seen[p] = true
			if p == folderID {
				return true
			}
		}
		return false
	}
	inside := map[string]bool{folderID: true}
	paths := map[string]bool{} // for entries that carry no group of their own
	for _, f := range m.viewFolders {
		if f.ID == folderID || under(f.ID) {
			if f.ID != folderID {
				folders = append(folders, f)
			}
			inside[f.ID] = true
			paths[f.Source+"\x00"+f.Path] = true
		}
	}
	for _, e := range m.viewEntries {
		switch {
		case e.GroupID != "" && inside[e.GroupID]:
		case e.GroupID == "" && paths[e.Source+"\x00"+e.Path]:
		default:
			continue
		}
		entries = append(entries, e)
	}
	return folders, entries
}

// goesWithIt counts what a folder deletion takes along, so the change can say so
// rather than leaving the reader to work it out from the tree.
func (m Model) goesWithIt(folderID string) (entries, folders int) {
	f, e := m.descendantsOf(folderID)
	return len(e), len(f)
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
	if folders > 0 {
		parts = append(parts, plural(folders, "folder", "folders"))
	}
	if entries > 0 {
		parts = append(parts, plural(entries, "entry", "entries"))
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

// contentsGoing lists what a folder deletion removes, as the tree it is.
//
// A count is a promise the reader cannot check, and they are about to approve
// it — so everything under the folder is named. Flat, it was useless in a
// different way: every sub-folder in one block and every entry in another told
// you what was going but not what was inside what. It is a tree, so it is drawn
// as one.
//
// The rows are not selectable targets and carry a faded marker: they are
// consequences of one staged decision, not decisions of their own, and there is
// nothing here to revert separately.
func (m Model) contentsGoing(c edit.Change, w int) [][]rowSeg {
	if c.State != edit.Deleted || !isFolderKind(c.Kind) {
		return nil
	}
	f, ok := m.folderByID(c.Target)
	if !ok {
		return nil
	}

	const indent = "       "
	del, mark := changeStyle(edit.Deleted)
	i := ic()

	var out [][]rowSeg
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		pad := strings.Repeat("  ", depth)

		// Sub-folders first, then the entries filed here — the order the vault
		// tree uses, so the same vault reads the same way in both places.
		var subs []string
		for _, mf := range m.viewFolders {
			if mf.Source == c.Source && parentPath(mf.Path) == path && mf.Path != path {
				subs = append(subs, mf.Path)
			}
		}
		sort.Strings(subs)

		var titles []string
		for _, e := range m.viewEntries {
			if e.Source == c.Source && e.Path == path {
				titles = append(titles, e.Title)
			}
		}
		sort.Strings(titles)

		for _, sub := range subs {
			name := sub
			if j := strings.LastIndex(sub, "/"); j >= 0 {
				name = sub[j+1:]
			}
			out = append(out, []rowSeg{
				{indent + "  ", theme.Faded},
				{mark + " ", theme.Faded},
				{pad + i.folder + " ", del},
				{trunc(name, max(8, w-len(indent)-len(pad)-8)), del},
			})
			walk(sub, depth+1)
		}
		for _, t := range titles {
			out = append(out, []rowSeg{
				{indent + "  ", theme.Faded},
				{mark + " ", theme.Faded},
				{pad + i.entry + " ", del},
				{trunc(t, max(8, w-len(indent)-len(pad)-8)), del},
			})
		}
	}
	walk(f.Path, 0)

	if len(out) == 0 {
		return [][]rowSeg{{{indent + "  (empty)", theme.Faded}}}
	}
	return out
}

// changesVisibleRows is how many rows the panel shows at once — the page a
// PageUp/PageDown moves by, and the window the cursor has to stay inside.
func (m Model) changesVisibleRows() int { return max(1, max(3, m.h-3)-2) }

// scrollToShow pulls the window just far enough for row to be inside it. It
// moves as little as possible: a list that re-centres itself under the cursor
// loses the reader's place on every keystroke.
func scrollToShow(offset, row, visible, total int) int {
	if row < 0 {
		return 0
	}
	if row < offset {
		offset = row
	}
	if row >= offset+visible {
		offset = row - visible + 1
	}
	return clampScroll(offset, total, visible)
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

// Folding, worked by the vault tree's keys.
//
// The Changes tab is the same shape as the tree — sources, folders, the things
// inside them — so it takes the same keys rather than inventing a second way to
// open and close the same idea. z toggles what the cursor is on, Z the lot,
// ←/→ close and open one step, ⇧←/⇧→ a whole branch. Everything starts open:
// the reader is here to see what is staged, and hiding it by default would be
// answering a question they came to ask.

func (m Model) folded(r changeRow) bool {
	return r.target != "" && m.chgFold[foldKey(r.kind, r.target)]
}

func (m Model) setFold(r changeRow, fold bool) Model {
	if r.target == "" {
		return m
	}
	if m.chgFold == nil {
		m.chgFold = map[string]bool{}
	}
	m.chgFold[foldKey(r.kind, r.target)] = fold
	return m
}

// foldBranch folds a heading and everything filed under it, or opens the lot.
//
// The members come from the change set rather than from the visible rows: a
// folded group has no visible members, so a version that read the rows could
// close a branch but never fully open it again.
func (m Model) foldBranch(_ []changeRow, cur changeRow, fold bool) Model {
	if cur.kind == rowChange {
		return m.setFold(cur, fold)
	}
	m = m.setFold(cur, fold)
	for _, g := range m.groupChanges() {
		if g.source+"\x00"+g.path != cur.target {
			continue
		}
		for _, c := range g.items {
			m = m.setFold(changeRow{kind: rowChange, target: c.Target}, fold)
		}
	}
	return m
}

// selectHeadingOf moves the cursor to the folder heading a change sits under.
func (m Model) selectHeadingOf(rows []changeRow, cur changeRow) Model {
	if cur.kind != rowChange {
		return m
	}
	group := m.groupOf(rows, cur)
	for i, idx := range selectableRows(rows) {
		if rows[idx].kind == rowFolder && rows[idx].target == group {
			m.chgSel = i
			return m
		}
	}
	return m
}

// selectFirstChangeIn steps from a heading into the first change under it.
func (m Model) selectFirstChangeIn(rows []changeRow, cur changeRow) Model {
	for i, idx := range selectableRows(rows) {
		if rows[idx].kind == rowChange && m.groupOf(rows, rows[idx]) == cur.target {
			m.chgSel = i
			return m
		}
	}
	return m
}

// writeImpact is what a save will do to one source, counted in the things a
// vault is made of rather than in operations.
//
// "5 changes" is a number about the program. The reader is about to approve
// something else entirely: that 4 folders and 47 entries stop existing. One of
// those deletions was a single keystroke on a folder, and this is the only place
// the total ever appears.
type writeImpact struct {
	newFolders, newEntries         int
	changedFolders, changedEntries int
	movedFolders, movedEntries     int
	folders, entries               int // removed
	permanent                      int // of the removed, those skipping the recycle bin
}

func (m Model) impactOf(source string) writeImpact {
	var im writeImpact

	// A folder inside another staged folder is already counted by it.
	var deleted []string
	for _, c := range m.chg.Diff() {
		if c.Source == source && c.State == edit.Deleted && isFolderKind(c.Kind) {
			if f, ok := m.folderByID(c.Target); ok {
				deleted = append(deleted, f.Path)
			}
		}
	}
	outermost := func(path string) bool {
		for _, other := range deleted {
			if other != path && strings.HasPrefix(path, other+"/") {
				return false
			}
		}
		return true
	}

	for _, c := range m.chg.Diff() {
		if c.Source != source {
			continue
		}
		switch c.State {
		case edit.New:
			if isFolderKind(c.Kind) {
				im.newFolders++
			} else {
				im.newEntries++
			}
		case edit.Modified:
			if isFolderKind(c.Kind) {
				im.changedFolders++
			} else {
				im.changedEntries++
			}
		case edit.Moved:
			if isFolderKind(c.Kind) {
				im.movedFolders++
			} else {
				im.movedEntries++
			}
		case edit.Deleted:
			if !isFolderKind(c.Kind) {
				im.entries++
				if isPermanent(m.chg, c.Target) {
					im.permanent++
				}
				continue
			}
			f, ok := m.folderByID(c.Target)
			if !ok || !outermost(f.Path) {
				continue
			}
			entries, folders := m.goesWithIt(c.Target)
			im.folders += 1 + folders
			im.entries += entries
			if isPermanent(m.chg, c.Target) {
				im.permanent += 1 + folders + entries
			}
		}
	}
	return im
}

func isPermanent(set edit.Set, target string) bool {
	for _, op := range set.Effective() {
		if op.Target == target {
			return op.Perm
		}
	}
	return false
}

// permanentlyRemoved is how many things, across every source, will stop
// existing with no recycle bin to fish them out of.
func (m Model) permanentlyRemoved() int {
	n := 0
	for _, src := range m.chg.Sources() {
		n += m.impactOf(src).permanent
	}
	return n
}

// lines renders the impact as the sentences a reader has to agree with.
func (im writeImpact) lines() []string {
	var out []string
	add := func(st edit.State, text string) {
		style, marker := changeStyle(st)
		out = append(out, "    "+style.Render(strings.TrimSpace(marker)+" "+text))
	}
	if n := im.newFolders + im.newEntries; n > 0 {
		add(edit.New, countOf(im.newFolders, im.newEntries)+" added")
	}
	if im.changedFolders+im.changedEntries > 0 {
		add(edit.Modified, countOf(im.changedFolders, im.changedEntries)+" changed")
	}
	if im.movedFolders+im.movedEntries > 0 {
		add(edit.Moved, countOf(im.movedFolders, im.movedEntries)+" moved")
	}
	if total := im.folders + im.entries; total > 0 {
		text := countOf(im.folders, im.entries) + " removed"
		switch {
		case im.permanent == total:
			text += ", permanently"
		case im.permanent > 0:
			text += ", " + itoa(im.permanent) + " of them permanently"
		}
		add(edit.Deleted, text)
	}
	return out
}

// countOf writes a folder count and an entry count as one phrase, folders
// first — the order the review lists them in, so the two agree.
func countOf(folders, entries int) string {
	var parts []string
	if folders > 0 {
		parts = append(parts, plural(folders, "folder", "folders"))
	}
	if entries > 0 {
		parts = append(parts, plural(entries, "entry", "entries"))
	}
	return strings.Join(parts, " and ")
}

// doomedParent reports whether a folder is staged for deletion or sits inside
// one, so nothing new is put somewhere that is about to stop existing.
func (m Model) doomedParent(folderID string) (string, bool) {
	if folderID == "" {
		return "", false
	}
	if m.chg.StateOf(folderID) == edit.Deleted {
		if f, ok := m.folderByID(folderID); ok {
			return f.Name, true
		}
		return "that folder", true
	}
	if folder, yes := m.inDoomedFolder(folderID); yes {
		return lastSegment(folder), true
	}
	return "", false
}

// lastSegment is a path's own name.
func lastSegment(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// dropDeletionsUnder takes back deletions staged against things inside a folder
// that is now being deleted as a whole. They are the same decision, and staging
// them twice made the write do two different things to one item.
func (m Model) dropDeletionsUnder(folderID string) Model {
	folders, entries := m.descendantsOf(folderID)
	inside := map[string]bool{}
	for _, f := range folders {
		inside[f.ID] = true
	}
	for _, e := range entries {
		inside[e.ID] = true
	}
	if len(inside) == 0 {
		return m
	}
	for _, op := range m.chg.Effective() {
		if op.Kind != edit.DeleteEntry && op.Kind != edit.DeleteGroup {
			continue
		}
		if inside[op.Target] {
			m.chg, _ = m.chg.Revert(op.Seq)
		}
	}
	return m
}
