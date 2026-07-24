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
	name     string
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
func buildTree(entries []vault.Entry) []*node {
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

	for _, e := range entries {
		key := e.Source
		cur := child(nil, key, e.Source)
		if e.Path != "" {
			for _, seg := range strings.Split(e.Path, "/") {
				key += "/" + seg
				cur = child(cur, key, seg)
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
