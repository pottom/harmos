package tui

import (
	"sort"
	"strings"

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

// buildTree groups entries into a per-source folder tree: the top level is the
// set of sources, then the folder Path segments, and entries hang off their
// folder. Source roots start expanded so their folders are visible at a glance.
//
// Folders come in as their own list rather than being inferred from entry paths.
// Inference cannot see a folder with nothing in it — so an empty one was
// invisible, and a folder the user had just created would not appear until
// something was put in it.
func buildTree(entries []vault.Entry, folders []vault.Folder) []*node {
	var roots []*node
	index := map[string]*node{} // full path key → node

	child := func(parent *node, key, name string) *node {
		if n, ok := index[key]; ok {
			return n
		}
		n := &node{name: name}
		index[key] = n
		if parent == nil {
			n.expanded = true
			roots = append(roots, n)
		} else {
			parent.children = append(parent.children, n)
		}
		return n
	}

	// Materialise every folder first, so the empty ones exist too.
	for _, f := range folders {
		key := f.Source
		cur := child(nil, key, f.Source)
		cur.source = f.Source
		for _, seg := range strings.Split(f.Path, "/") {
			key += "/" + seg
			cur = child(cur, key, seg)
			cur.source = f.Source
		}
		cur.id = f.ID
	}

	for _, e := range entries {
		key := e.Source
		cur := child(nil, key, e.Source)
		cur.source = e.Source
		if e.Path != "" {
			for _, seg := range strings.Split(e.Path, "/") {
				key += "/" + seg
				cur = child(cur, key, seg)
				cur.source = e.Source
			}
		}
		cur.entries = append(cur.entries, e)
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
