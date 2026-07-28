package vault

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
)

// Identity, and why it is a plain string.
//
// The read projection could not name the entry it came from: no UUID, no group
// identity, and FullPath is not unique — this repo's own fixtures contain a
// title collision. Anything that edits an entry has to be able to say which one,
// so Entry and Folder now carry an opaque ID.
//
// It is a string, not a named type, so nothing downstream — search, the TUI's
// tree, the staged change set — needs to import this package to hold one, and
// there is no import cycle between vault and edit.
//
// Pointers into the tree are deliberately not used as identity: every mutation
// reallocates the Entries and Groups slices, so a pointer taken before an edit
// is a pointer to nothing afterwards. Resolution walks the tree afresh instead.
// At ~10k entries that costs microseconds and is always right.

const (
	groupIDMarker = ":g:"
	entryIDMarker = ":"
	dupMarker     = "#"
)

// entryID is source:base64(uuid), plus #n for the n-th duplicate.
//
// Duplicate UUIDs exist in the wild — bad merges produce them — so the encoding
// has to survive them rather than assume they cannot happen.
func entryID(source string, u gokeepasslib.UUID, dup int) string {
	id := source + entryIDMarker + base64.RawURLEncoding.EncodeToString(u[:])
	if dup > 0 {
		id += dupMarker + strconv.Itoa(dup)
	}
	return id
}

// groupID is source:g:base64(uuid), plus #n for the n-th duplicate.
func groupID(source string, u gokeepasslib.UUID, dup int) string {
	id := source + groupIDMarker + base64.RawURLEncoding.EncodeToString(u[:])
	if dup > 0 {
		id += dupMarker + strconv.Itoa(dup)
	}
	return id
}

// parseID splits an ID back into its parts. isGroup distinguishes the two kinds,
// so a group ID handed to an entry operation is rejected rather than silently
// matching nothing.
func parseID(id string) (source string, u gokeepasslib.UUID, dup int, isGroup bool, err error) {
	rest := id
	if i := strings.LastIndex(rest, dupMarker); i >= 0 {
		n, cerr := strconv.Atoi(rest[i+1:])
		if cerr != nil || n < 1 {
			return "", u, 0, false, fmt.Errorf("malformed id %q", id)
		}
		dup, rest = n, rest[:i]
	}

	var encoded string
	if i := strings.Index(rest, groupIDMarker); i >= 0 {
		source, encoded, isGroup = rest[:i], rest[i+len(groupIDMarker):], true
	} else if i := strings.Index(rest, entryIDMarker); i >= 0 {
		source, encoded = rest[:i], rest[i+len(entryIDMarker):]
	} else {
		return "", u, 0, false, fmt.Errorf("malformed id %q", id)
	}

	raw, derr := base64.RawURLEncoding.DecodeString(encoded)
	if derr != nil || len(raw) != len(u) {
		return "", u, 0, false, fmt.Errorf("malformed id %q", id)
	}
	copy(u[:], raw)
	return source, u, dup, isGroup, nil
}

// walkTree visits every group and entry depth-first in file order, handing each
// its stable ID. Groups are visited before their contents, so a parent always
// has an ID by the time its children need one.
//
// The traversal order is the file's order, which is what makes the duplicate
// counter stable: the same file always numbers the same entry the same way.
func (h *Handle) walkTree(
	onGroup func(g *gokeepasslib.Group, id, parentID, path string),
	onEntry func(e *gokeepasslib.Entry, id, groupID, path string),
) {
	if h.db.Content == nil || h.db.Content.Root == nil {
		return
	}
	seen := map[gokeepasslib.UUID]int{}

	var visit func(g *gokeepasslib.Group, parentID, prefix string)
	visit = func(g *gokeepasslib.Group, parentID, prefix string) {
		gid := groupID(h.source, g.UUID, seen[g.UUID])
		seen[g.UUID]++

		if onGroup != nil {
			onGroup(g, gid, parentID, prefix)
		}
		for i := range g.Entries {
			e := &g.Entries[i]
			eid := entryID(h.source, e.UUID, seen[e.UUID])
			seen[e.UUID]++
			if onEntry != nil {
				onEntry(e, eid, gid, prefix)
			}
		}
		for i := range g.Groups {
			sub := &g.Groups[i]
			child := oneline(sub.Name)
			if prefix != "" {
				child = prefix + "/" + child
			}
			visit(sub, gid, child)
		}
	}

	// The top-level group is the database's root container; its own name is not
	// part of entry paths, so it contributes an empty prefix.
	for i := range h.db.Content.Root.Groups {
		visit(&h.db.Content.Root.Groups[i], "", "")
	}
}

// findEntry resolves an entry ID to the entry and the group holding it. Both are
// live pointers into the database, valid only until the next mutation.
func (h *Handle) findEntry(id string) (*gokeepasslib.Group, *gokeepasslib.Entry, error) {
	if _, _, _, isGroup, err := parseID(id); err != nil {
		return nil, nil, err
	} else if isGroup {
		return nil, nil, fmt.Errorf("%q is a folder, not an entry", id)
	}

	var (
		foundGroup *gokeepasslib.Group
		foundEntry *gokeepasslib.Entry
	)
	// The group pointer comes from the enclosing walk rather than the callback's
	// own argument, because the entry callback is handed the group it belongs to
	// by ID only.
	var currentGroup *gokeepasslib.Group
	h.walkTree(
		func(g *gokeepasslib.Group, _, _, _ string) { currentGroup = g },
		func(e *gokeepasslib.Entry, eid, _, _ string) {
			if eid == id && foundEntry == nil {
				foundGroup, foundEntry = currentGroup, e
			}
		},
	)
	if foundEntry == nil {
		return nil, nil, fmt.Errorf("no entry %q", id)
	}
	return foundGroup, foundEntry, nil
}

// findGroup resolves a group ID to the group and its parent. The parent is nil
// for a root group, which is what makes "you cannot move or delete the root"
// checkable.
func (h *Handle) findGroup(id string) (parent, group *gokeepasslib.Group, err error) {
	if _, _, _, isGroup, perr := parseID(id); perr != nil {
		return nil, nil, perr
	} else if !isGroup {
		return nil, nil, fmt.Errorf("%q is an entry, not a folder", id)
	}

	parents := map[string]*gokeepasslib.Group{}
	byID := map[string]*gokeepasslib.Group{}
	h.walkTree(func(g *gokeepasslib.Group, gid, parentID, _ string) {
		byID[gid] = g
		if p, ok := byID[parentID]; ok {
			parents[gid] = p
		}
	}, nil)

	g, ok := byID[id]
	if !ok {
		return nil, nil, fmt.Errorf("no folder %q", id)
	}
	return parents[id], g, nil
}
