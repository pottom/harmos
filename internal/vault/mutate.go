package vault

import (
	"fmt"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/edit"
)

// The mutation primitives. Each changes the decoded database in memory and
// nothing else — Save is still the only thing that touches the file, so a caller
// can stage a whole session's worth of edits and then decline to write them.
//
// Every one of them refuses on a source harmos will not write. Checking at the
// point of change rather than only at the point of save means the user finds out
// while they can still do something about it, instead of after typing.

// CreateEntry adds an empty entry to a folder and returns its ID.
//
// The ID is real from this moment: the kdbx UUID is assigned now, not at save
// time, so everything that follows — edits, a move, a delete, an undo — can name
// the same entry without a second identity scheme to translate between.
func (h *Handle) CreateEntry(parentGroupID string, d edit.Draft) (string, error) {
	if err := h.mutable(); err != nil {
		return "", err
	}
	_, parent, err := h.findGroup(parentGroupID)
	if err != nil {
		return "", err
	}

	e := gokeepasslib.NewEntry()
	e.Times = freshTimes()
	applyDraft(&e, d)

	parent.Entries = append(parent.Entries, e)
	touchGroup(parent)

	return entryID(h.source, e.UUID, h.dupIndexOf(e.UUID)-1), nil
}

// UpdateEntry writes a draft over an existing entry, pushing the previous state
// onto the KeePass history stack first.
func (h *Handle) UpdateEntry(id string, d edit.Draft) error {
	if err := h.mutable(); err != nil {
		return err
	}
	_, e, err := h.findEntry(id)
	if err != nil {
		return err
	}

	pushHistory(h.db, e)
	applyDraft(e, d)
	touch(e)
	return nil
}

// MoveEntry moves an entry to another folder, recording where it came from so a
// client can put it back.
func (h *Handle) MoveEntry(id, destGroupID string) error {
	if err := h.mutable(); err != nil {
		return err
	}
	from, e, err := h.findEntry(id)
	if err != nil {
		return err
	}
	if _, dest, derr := h.findGroup(destGroupID); derr != nil {
		return derr
	} else if from == dest {
		return nil
	}

	moved := *e
	prev := from.UUID
	moved.PreviousParentGroup = &prev
	moved.Times.LocationChanged = nowPtr()
	fromID := from.UUID

	if err := removeEntry(from, e.UUID); err != nil {
		return err
	}

	// Re-resolve after the removal. Deleting from a slice shifts everything
	// after it, so a pointer taken before the removal can now be aimed at a
	// different group — or at nothing.
	_, dest, err := h.findGroup(destGroupID)
	if err != nil {
		return err
	}
	dest.Entries = append(dest.Entries, moved)
	touchGroup(dest)
	if _, origin, ferr := h.findGroupByUUID(fromID); ferr == nil {
		touchGroup(origin)
	}
	return nil
}

// DeleteEntry removes an entry: to the recycle bin, or permanently when perm is
// set or the database has no bin.
//
// Whether it really went to the bin is the caller's business — a confirmation
// that promises a recoverable delete on a database with the bin switched off
// would be a lie — so RecycleBinEnabled is exported for the prompt to consult.
func (h *Handle) DeleteEntry(id string, perm bool) error {
	if err := h.mutable(); err != nil {
		return err
	}
	from, e, err := h.findEntry(id)
	if err != nil {
		return err
	}

	if perm || !h.RecycleBinEnabled() {
		uuid := e.UUID
		if err := removeEntry(from, uuid); err != nil {
			return err
		}
		touchGroup(from)
		h.tombstone(uuid)
		return nil
	}

	bin, err := h.recycleBin()
	if err != nil {
		return err
	}
	// findEntry's pointers can be stale now: creating the bin may have grown the
	// root group's slice and moved everything in it.
	from, e, err = h.findEntry(id)
	if err != nil {
		return err
	}
	if from == bin {
		return nil // already there
	}

	binned := *e
	prev := from.UUID
	binned.PreviousParentGroup = &prev
	binned.Times.LocationChanged = nowPtr()
	binUUID := bin.UUID

	if err := removeEntry(from, e.UUID); err != nil {
		return err
	}
	touchGroup(from)

	_, bin, err = h.findGroupByUUID(binUUID) // re-resolve after the removal
	if err != nil {
		return err
	}
	bin.Entries = append(bin.Entries, binned)
	touchGroup(bin)
	return nil
}

// CreateGroup adds a folder and returns its ID.
func (h *Handle) CreateGroup(parentGroupID, name string) (string, error) {
	if err := h.mutable(); err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("a folder needs a name")
	}
	_, parent, err := h.findGroup(parentGroupID)
	if err != nil {
		return "", err
	}

	g := gokeepasslib.NewGroup()
	g.Name = name
	g.Times = freshTimes()

	parent.Groups = append(parent.Groups, g)
	touchGroup(parent)

	return groupID(h.source, g.UUID, h.dupIndexOf(g.UUID)-1), nil
}

// RenameGroup renames a folder.
func (h *Handle) RenameGroup(id, name string) error {
	if err := h.mutable(); err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("a folder needs a name")
	}
	_, g, err := h.findGroup(id)
	if err != nil {
		return err
	}
	g.Name = name
	touchGroup(g)
	return nil
}

// MoveGroup moves a folder, with its whole subtree, under another folder.
func (h *Handle) MoveGroup(id, destGroupID string) error {
	if err := h.mutable(); err != nil {
		return err
	}
	if id == destGroupID {
		return fmt.Errorf("a folder cannot be moved into itself")
	}
	parent, g, err := h.findGroup(id)
	if err != nil {
		return err
	}
	if parent == nil {
		return fmt.Errorf("the root folder cannot be moved")
	}
	_, dest, err := h.findGroup(destGroupID)
	if err != nil {
		return err
	}
	if containsGroup(g, dest.UUID) {
		return fmt.Errorf("a folder cannot be moved inside itself")
	}

	moved := *g
	prev := parent.UUID
	moved.PreviousParentGroup = &prev
	moved.Times.LocationChanged = nowPtr()
	parentUUID := parent.UUID

	if err := removeGroup(parent, g.UUID); err != nil {
		return err
	}

	// Re-resolve after the removal — see MoveEntry. Moving between siblings is
	// the case that bites: removing the source shifts the parent's slice, and
	// the destination pointer taken beforehand no longer refers to it.
	_, dest, err = h.findGroup(destGroupID)
	if err != nil {
		return err
	}
	dest.Groups = append(dest.Groups, moved)
	touchGroup(dest)
	if _, origin, ferr := h.findGroupByUUID(parentUUID); ferr == nil {
		touchGroup(origin)
	}
	return nil
}

// DeleteGroup removes a folder and everything under it.
func (h *Handle) DeleteGroup(id string, perm bool) error {
	if err := h.mutable(); err != nil {
		return err
	}
	parent, g, err := h.findGroup(id)
	if err != nil {
		return err
	}
	if parent == nil {
		return fmt.Errorf("the root folder cannot be deleted")
	}

	if perm || !h.RecycleBinEnabled() {
		// Everything beneath it disappears too, so everything beneath it needs
		// its own record.
		h.tombstoneSubtree(g)
		uuid := g.UUID
		if err := removeGroup(parent, uuid); err != nil {
			return err
		}
		touchGroup(parent)
		return nil
	}

	bin, err := h.recycleBin()
	if err != nil {
		return err
	}
	parent, g, err = h.findGroup(id) // the bin may have moved things
	if err != nil {
		return err
	}
	if parent == bin || containsGroup(g, bin.UUID) {
		return fmt.Errorf("the recycle bin cannot be moved into itself")
	}

	binned := *g
	prev := parent.UUID
	binned.PreviousParentGroup = &prev
	binned.Times.LocationChanged = nowPtr()
	binUUID := bin.UUID

	if err := removeGroup(parent, g.UUID); err != nil {
		return err
	}
	touchGroup(parent)

	_, bin, err = h.findGroupByUUID(binUUID) // re-resolve after the removal
	if err != nil {
		return err
	}
	bin.Groups = append(bin.Groups, binned)
	touchGroup(bin)
	return nil
}

// mutable refuses on a source harmos will not write, so an edit cannot be staged
// against a file that could never receive it.
func (h *Handle) mutable() error {
	if ok, why := h.Writable(); !ok {
		return fmt.Errorf("%w: %s", ErrNotWritable, why)
	}
	return nil
}

// dupIndexOf counts how many items already carry this UUID. A fresh UUID gives
// 1, so the caller subtracts one to get the zero-based duplicate index the ID
// encoding uses.
func (h *Handle) dupIndexOf(u gokeepasslib.UUID) int {
	n := 0
	h.walkTree(
		func(g *gokeepasslib.Group, _, _, _ string) {
			if g.UUID == u {
				n++
			}
		},
		func(e *gokeepasslib.Entry, _, _, _ string) {
			if e.UUID == u {
				n++
			}
		},
	)
	return n
}

func removeEntry(g *gokeepasslib.Group, u gokeepasslib.UUID) error {
	for i := range g.Entries {
		if g.Entries[i].UUID == u {
			g.Entries = append(g.Entries[:i], g.Entries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("entry not found in its folder")
}

func removeGroup(parent *gokeepasslib.Group, u gokeepasslib.UUID) error {
	for i := range parent.Groups {
		if parent.Groups[i].UUID == u {
			parent.Groups = append(parent.Groups[:i], parent.Groups[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("folder not found in its parent")
}

// containsGroup reports whether u is g or lives beneath it — the check that stops
// a folder being moved inside its own subtree, which would detach the branch
// from the tree entirely.
func containsGroup(g *gokeepasslib.Group, u gokeepasslib.UUID) bool {
	if g.UUID == u {
		return true
	}
	for i := range g.Groups {
		if containsGroup(&g.Groups[i], u) {
			return true
		}
	}
	return false
}

// findGroupByUUID resolves a group by its raw UUID, for the moments when an ID
// is not to hand — after a removal has shifted the tree, for instance.
func (h *Handle) findGroupByUUID(u gokeepasslib.UUID) (parent, group *gokeepasslib.Group, err error) {
	var found *gokeepasslib.Group
	var foundParent *gokeepasslib.Group
	byID := map[string]*gokeepasslib.Group{}

	h.walkTree(func(g *gokeepasslib.Group, gid, parentID, _ string) {
		byID[gid] = g
		if g.UUID == u && found == nil {
			found = g
			foundParent = byID[parentID]
		}
	}, nil)

	if found == nil {
		return nil, nil, fmt.Errorf("no folder with that uuid")
	}
	return foundParent, found, nil
}
