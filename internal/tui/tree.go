package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

// node is a folder in the browse tree — the left pane mirrors the Pleasant
// folder tree: folders only. Entries hang off the folder they live in and are
// shown in the right table. A KeePass group may hold both sub-folders and
// entries, so a node can have both children and entries.
type node struct {
	name string
	// id is the folder's stable ID, or "" for a source root (which is not a
	// folder in the file — it is the source itself). Carrying it is what lets a
	// staged change be attributed to the row that shows it.
	id       string
	source   string
	children []*node
	entries  []vault.Entry
	expanded bool
}

// treeLine is one flattened, visible row of the tree with its indent depth.
type treeLine struct {
	node  *node
	depth int
}

// buildTree assembles the per-source folder tree: a root node per source, then
// the folders as the file relates them, with entries hanging off the folder they
// live in. Everything starts closed.
//
// It is built from identities, not from path strings. Splitting Path on "/" was
// shorter and wrong twice over: two sibling folders with the same name collapsed
// into one row — legal in KeePass, and produced by any merge — where the last
// one silently won every operation aimed at either; and a folder whose name
// contains a "/" grew a phantom parent row that belonged to no folder at all,
// whose ID was empty, which the writer reads as "the vault root". Creating
// something on that row put it at the top of the vault.
//
// Folders come in as their own list rather than being inferred from entry paths.
// Inference cannot see a folder with nothing in it — so an empty one was
// invisible, and a folder the user had just created would not appear until
// something was put in it.
func buildTree(entries []vault.Entry, folders []vault.Folder) []*node {
	var roots []*node
	rootOf := map[string]*node{}
	byID := map[string]*node{}

	sourceRoot := func(source string) *node {
		if n, ok := rootOf[source]; ok {
			return n
		}
		// Closed on arrival, sources included. A vault with a few hundred
		// folders opens as a wall of rows otherwise, and the reader's first act
		// is to close it again; z, Z and the arrows open exactly as much as
		// somebody wants.
		n := &node{name: source, source: source}
		rootOf[source] = n
		roots = append(roots, n)
		return n
	}

	byPath := map[string]*node{} // source\x00path — for entries no folder claims

	for _, f := range folders {
		sourceRoot(f.Source)
		n := &node{name: f.Name, id: f.ID, source: f.Source}
		byID[f.ID] = n
		byPath[f.Source+"\x00"+f.Path] = n
	}
	for _, f := range folders {
		n := byID[f.ID]
		// A parent that is not itself a browsable folder is the file's root
		// container, and the source stands in for it.
		if p, ok := byID[f.ParentID]; ok && f.ParentID != f.ID {
			p.children = append(p.children, n)
			continue
		}
		sourceRoot(f.Source).children = append(sourceRoot(f.Source).children, n)
	}

	for _, e := range entries {
		sourceRoot(e.Source)
		if n, ok := byID[e.GroupID]; ok {
			n.entries = append(n.entries, e)
			continue
		}
		// No folder claims it. It may sit in the file's root container, or the
		// caller may have passed entries without folders at all — the empty
		// vault does, and so does anything that has only a search index. Fall
		// back to the path, which is the only structure left.
		home := sourceRoot(e.Source)
		if e.Path != "" {
			if n, ok := byPath[e.Source+"\x00"+e.Path]; ok {
				home = n
			} else {
				// The same key shape the folders registered, so a synthesised
				// node and a real one never split a branch in two.
				cur, prefix := home, ""
				for _, seg := range strings.Split(e.Path, "/") {
					if prefix != "" {
						prefix += "/"
					}
					prefix += seg
					key := e.Source + "\x00" + prefix
					n, ok := byPath[key]
					if !ok {
						n = &node{name: seg, source: e.Source}
						byPath[key] = n
						cur.children = append(cur.children, n)
					}
					cur = n
				}
				home = cur
			}
		}
		home.entries = append(home.entries, e)
	}

	sortNodes(roots)
	return roots
}

func sortNodes(ns []*node) {
	sort.Slice(ns, func(i, j int) bool { return ns[i].name < ns[j].name })
	for _, n := range ns {
		sortNodes(n.children)
		sort.SliceStable(n.entries, func(i, j int) bool { return n.entries[i].Title < n.entries[j].Title })
	}
}

// visibleTree flattens the tree to the rows that are currently visible (a node's
// children appear only while it is expanded).
func visibleTree(roots []*node) []treeLine {
	var out []treeLine
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		out = append(out, treeLine{n, depth})
		if n.expanded {
			for _, c := range n.children {
				walk(c, depth+1)
			}
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

// setExpanded opens or closes every node in the tree, source roots included.
//
// Roots are not exempt: collapsing one has always been possible with ← on the
// root row, so a "collapse all" that left them open would be collapsing all but
// the rows the user can already see.
func setExpanded(ns []*node, open bool) {
	for _, n := range ns {
		n.expanded = open
		setExpanded(n.children, open)
	}
}

// parentOf maps every node to its parent; roots are absent, so a lookup on one
// yields nil and ends an upward walk.
func parentOf(roots []*node) map[*node]*node {
	out := map[*node]*node{}
	var walk func(n *node)
	walk = func(n *node) {
		for _, c := range n.children {
			out[c] = n
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return out
}

// expandSubtree opens or closes the selected folder and everything under it.
//
// The cursor does not move: the selected row is above whatever appears or
// disappears, so its index and its own entries are unchanged. Standing on a
// source root, this is that whole source — which is the usual reach for it.
//
// Closing takes the descendants with it, so a following → opens exactly one
// level. Leaving their flags set would mean the next → restored however deep
// the tree happened to be, which reads as "→ opened everything".
func (m Model) expandSubtree(open bool) Model {
	if cur := m.currentFolder(); cur != nil {
		setExpanded([]*node{cur}, open)
	}
	return m
}

// anyOpen reports whether any folder that could be open is open. It is what
// makes the toggles decide the same way a reader would: if there is something to
// close, close it.
func anyOpen(ns []*node) bool {
	for _, n := range ns {
		if n.expanded && len(n.children) > 0 {
			return true
		}
		if anyOpen(n.children) {
			return true
		}
	}
	return false
}

// toggleSubtree is one key for both directions on the selected branch: open it
// all the way down, or close it all the way down.
func (m Model) toggleSubtree() Model {
	cur := m.currentFolder()
	if cur == nil {
		return m
	}
	return m.expandSubtree(!anyOpen([]*node{cur}))
}

// toggleAll is the same decision over every source at once.
func (m Model) toggleAll() Model { return m.expandAll(!anyOpen(m.roots)) }

// expandAll opens or closes every tree and keeps the cursor somewhere it can
// still see.
//
// Collapsing hides the row the cursor was on, so it climbs to the nearest
// ancestor that survived — the folder the user was in, seen from further out,
// rather than whatever row happens to fall at that index. Expanding keeps the
// same row and only its index moves.
func (m Model) expandAll(open bool) Model {
	cur := m.currentFolder()
	parents := parentOf(m.roots)
	setExpanded(m.roots, open)

	flat := visibleTree(m.roots)
	for n := cur; n != nil; n = parents[n] {
		for i, tl := range flat {
			if tl.node != n {
				continue
			}
			m.tsel = i
			if n != cur {
				// A different folder means a different table; staying on row
				// esel of it would be an arbitrary selection.
				m.focus, m.esel = 0, 0
			}
			return m
		}
	}
	m.tsel, m.esel, m.focus = 0, 0, 0
	return m
}

// matchCounts returns, per tree node, how many search hits it contains (its own
// entries plus every descendant's), or nil when there's no active query. Used to
// highlight folders with results and show a count in the tree.
func (m Model) matchCounts() map[*node]int {
	if !m.showResults() {
		return nil
	}
	hit := make(map[string]struct{}, len(m.results))
	for _, r := range m.results {
		hit[entryKey(r.Entry)] = struct{}{}
	}
	counts := map[*node]int{}
	var walk func(n *node) int
	walk = func(n *node) int {
		c := 0
		for _, e := range n.entries {
			if _, ok := hit[entryKey(e)]; ok {
				c++
			}
		}
		for _, ch := range n.children {
			c += walk(ch)
		}
		counts[n] = c
		return c
	}
	for _, r := range m.roots {
		walk(r)
	}
	return counts
}

// entryKey identifies an entry for the per-node match counts.
//
// It is the entry's stable ID now. It used to be Source+Path+Title+Username,
// which is not unique — this repo's own fixtures contain a title collision — so
// two different entries counted as one.
func entryKey(e vault.Entry) string { return e.ID }

// gotoResultFolder leaves the search and reveals the selected result's folder in
// the tree — expanding the source and every folder on the way — then selects the
// entry there. It's the "g" (goto folder) action from the results list.
func (m Model) gotoResultFolder() Model {
	if m.sel < 0 || m.sel >= len(m.results) {
		return m
	}
	e := m.results[m.sel].Entry

	m.searchMode = false
	m.input.Blur()
	m.input.SetValue("")
	m.results = nil
	m.sel = 0

	var cur *node
	for _, r := range m.roots {
		if r.name == e.Source {
			cur = r
			break
		}
	}
	if cur == nil {
		return m
	}
	cur.expanded = true
	for _, seg := range strings.Split(e.Path, "/") {
		if seg == "" {
			continue
		}
		var next *node
		for _, c := range cur.children {
			if c.name == seg {
				next = c
				break
			}
		}
		if next == nil {
			break
		}
		next.expanded = true
		cur = next
	}

	for i, tl := range visibleTree(m.roots) {
		if tl.node == cur {
			m.tsel = i
			break
		}
	}
	m.esel = 0
	for i := range cur.entries {
		if cur.entries[i].Title == e.Title && cur.entries[i].Username == e.Username {
			m.esel = i
			break
		}
	}
	if len(cur.entries) > 0 {
		m.focus = 1 // land on the entry in the table
	}
	return m
}

// folderCrumb builds the "source › … › folder" trail for the row at sel by
// walking back through the flattened tree: a node's parent is the nearest
// earlier row one level shallower. The source (first crumb) gets its type icon.
func (m Model) folderCrumb(flat []treeLine, sel int) string {
	if sel < 0 || sel >= len(flat) {
		return ""
	}
	depth := flat[sel].depth
	crumbs := []string{flat[sel].node.name}
	for i := sel - 1; i >= 0 && depth > 0; i-- {
		if flat[i].depth == depth-1 {
			crumbs = append([]string{flat[i].node.name}, crumbs...)
			depth--
		}
	}
	trail := strings.Join(crumbs, " › ")
	if si, ok := m.sourceIcon(crumbs[0]); ok {
		return si + " " + trail
	}
	return trail
}

// nodeChange is what a tree row has to say about staged work, split in two
// because the two mean different things to whoever is reading the row.
//
// own is the folder itself: it is being deleted, created or renamed, so the row
// carries the state — the delete colour and marker. inside is only that
// something below it changed. Conflating them made a folder holding one deleted
// entry look exactly like a folder that was itself about to be deleted.
type nodeChange struct {
	own    edit.State
	inside edit.State
	doomed bool // going because a folder above it is
}

// changeStates maps each node to how it and its contents have been staged, so a
// collapsed folder still shows that something inside it is pending.
//
// Same shape as matchCounts, and consumed the same way: the decoration and the
// diff are both derived from one change set, so a row's colour and the Changes
// tab cannot disagree.
func (m Model) changeStates() map[*node]nodeChange {
	if m.chg.Empty() {
		return nil
	}
	byFolder := m.chg.Parents() // where a changed target lives
	byTarget := m.chg.State()   // what a target itself is
	doomed := m.doomedFolders()
	parents := m.folderParents()
	goingWith := func(id string) bool {
		seen := map[string]bool{}
		for p := parents[id]; p != "" && !seen[p]; p = parents[p] {
			seen[p] = true
			if doomed[p] {
				return true
			}
		}
		return false
	}
	out := map[*node]nodeChange{}

	var walk func(ns []*node) edit.State
	walk = func(ns []*node) edit.State {
		var strongest edit.State
		for _, n := range ns {
			c := nodeChange{
				own:    byTarget[n.id],
				inside: byFolder[n.id],
				doomed: n.id != "" && goingWith(n.id),
			}
			if below := walk(n.children); below > c.inside {
				c.inside = below
			}
			if c.own != 0 || c.inside != 0 || c.doomed {
				out[n] = c
			}
			// A deleted folder takes its contents with it, so it reads upward as
			// the strongest thing under its parent.
			if c.own > strongest {
				strongest = c.own
			}
			if c.inside > strongest {
				strongest = c.inside
			}
		}
		return strongest
	}
	walk(m.roots)
	return out
}

// treeRowStyle is how one tree row reads: the style for its name, the style for
// its icon, and a trailing marker.
//
// A named decision rather than an inline condition, because under `go test`
// there is no terminal — lipgloss renders every style as plain text, so a colour
// can only be tested by comparing the style, not the output.
func (m Model) treeRowStyle(n *node, c nodeChange) (name, icon, markerStyle lipgloss.Style, marker string) {
	if c.own == 0 && c.doomed {
		// Going because a folder above it is going. It wears the deletion — that
		// is true — and carries a faded marker: something has to say so without
		// relying on colour, but the fading says this is a consequence of a
		// decision made elsewhere, not one to be undone here.
		st, mk := changeStyle(edit.Deleted)
		return st, st, theme.Faded, mk
	}
	if c.own != 0 {
		// The row is the thing that changed: colour and marker, icon included —
		// an icon left in the writable colour reads as "this is fine" next to a
		// name marked for deletion.
		st, mk := changeStyle(c.own)
		return st, st, st, mk
	}
	if c.inside != 0 {
		// Something below changed. The marker says which kind, in that colour,
		// but the name stays as it was: this folder is not going anywhere.
		st, mk := changeStyle(c.inside)
		return theme.Strong, m.iconStyleFor(n), st, mk
	}
	return theme.Strong, m.iconStyleFor(n), theme.Faded, ""
}

// reveal puts the cursor on a target and opens every folder on the way to it.
//
// Creating something and leaving the cursor where it was is how a new folder
// ends up invisible: it is a child of a folder that happens to be closed, so
// nothing on screen changes and the next key acts on the old selection. What was
// just made is what the reader is looking for.
func (m Model) revealTarget(id string, isFolder bool) Model {
	if id == "" {
		return m
	}

	// Open the ancestors first, so the row exists in the flattened tree.
	var open func(ns []*node) bool
	open = func(ns []*node) bool {
		for _, n := range ns {
			if n.id == id {
				return true
			}
			for _, e := range n.entries {
				if e.ID == id {
					return true
				}
			}
			if open(n.children) {
				n.expanded = true
				return true
			}
		}
		return false
	}
	open(m.roots)

	for i, tl := range m.visible() {
		if isFolder && tl.node.id == id {
			m.tsel, m.esel, m.focus = i, 0, 0
			return m
		}
		for j, e := range tl.node.entries {
			if e.ID == id {
				m.tsel, m.esel, m.focus = i, j, 1
				return m
			}
		}
	}
	return m
}
