package vault

import (
	"fmt"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// recycleBinIconID is KeePass's trash icon. Other clients recognise the bin by
// Meta.RecycleBinUUID, not by the icon, but a bin that looks wrong in KeePassXC
// would still be a bug worth avoiding.
const recycleBinIconID = 43

// recycleBinName is what KeePass calls the group it creates.
const recycleBinName = "Recycle Bin"

// RecycleBinEnabled reports whether this database keeps deleted items in a bin.
//
// It matters to the caller, not just to us: when it is off, a "move to the bin"
// delete is really a permanent one, and a confirmation prompt that does not say
// so is lying to the user.
func (h *Handle) RecycleBinEnabled() bool {
	return h.db.Content != nil &&
		h.db.Content.Meta != nil &&
		h.db.Content.Meta.RecycleBinEnabled.Bool
}

// recycleBin returns the bin group, creating it if the database wants one and
// has not got one yet — which is what KeePass does on the first deletion.
func (h *Handle) recycleBin() (*gokeepasslib.Group, error) {
	meta := h.db.Content.Meta
	if meta == nil {
		return nil, fmt.Errorf("database has no metadata")
	}

	// An existing, still-present bin.
	if !isZeroUUID(meta.RecycleBinUUID) {
		if _, g, err := h.findGroup(groupID(h.source, meta.RecycleBinUUID, 0)); err == nil {
			return g, nil
		}
		// The UUID points at a group that is no longer there. Fall through and
		// make a new one rather than refusing to delete anything ever again.
	}

	root := &h.db.Content.Root.Groups[0]

	bin := gokeepasslib.NewGroup()
	bin.Name = recycleBinName
	bin.IconID = recycleBinIconID
	bin.Times = freshTimes()
	bin.EnableAutoType = w.NewNullableBoolWrapper(false)
	bin.EnableSearching = w.NewNullableBoolWrapper(false)

	root.Groups = append(root.Groups, bin)
	touchGroup(root)

	meta.RecycleBinUUID = bin.UUID
	meta.RecycleBinChanged = nowPtr()

	return &root.Groups[len(root.Groups)-1], nil
}

// tombstone records that a UUID was deleted, so a client synchronising against
// an older copy knows the item was removed rather than that it is missing from
// our side.
//
// DeletionTime must never be nil: gokeepasslib's format-version walk
// dereferences it without checking.
func (h *Handle) tombstone(u gokeepasslib.UUID) {
	h.db.Content.Root.DeletedObjects = append(
		h.db.Content.Root.DeletedObjects,
		gokeepasslib.DeletedObjectData{UUID: u, DeletionTime: nowPtr()},
	)
}

// tombstoneSubtree records the group and everything beneath it. A group's
// descendants disappear with it, so each one needs its own record.
func (h *Handle) tombstoneSubtree(g *gokeepasslib.Group) {
	h.tombstone(g.UUID)
	for i := range g.Entries {
		h.tombstone(g.Entries[i].UUID)
	}
	for i := range g.Groups {
		h.tombstoneSubtree(&g.Groups[i])
	}
}

func isZeroUUID(u gokeepasslib.UUID) bool {
	return u == gokeepasslib.UUID{}
}
