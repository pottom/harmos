package vault

import (
	"fmt"

	"github.com/pottom/harmos/internal/edit"
)

// Apply performs a staged change set against this handle's database.
//
// It applies the *reduced* set, not the log: several edits to one entry become
// one edit, an entry created and then deleted becomes nothing at all, and a move
// collapses to its final destination. So the file records what the user meant,
// not the path they took to mean it — one history record per entry per save
// rather than one per keystroke-batch, and no entry that briefly existed.
//
// Nothing reaches the file here either. Apply changes the decoded database in
// memory; Save is still the only writer, so a caller can apply, look at the
// result, and walk away.
//
// Failure is not rolled back, because there is nothing to roll back to: the
// database is in memory, and the caller's recourse is to reload rather than to
// unpick a half-applied set. The error names the operation that stopped it.
func (h *Handle) Apply(set edit.Set) error {
	if err := h.mutable(); err != nil {
		return err
	}

	for _, op := range set.Effective() {
		if op.Source != h.source {
			continue // another source's changes are not ours to make
		}
		if err := h.applyOne(op); err != nil {
			return fmt.Errorf("%s %q: %w", op.Kind, op.Target, err)
		}
	}
	return nil
}

func (h *Handle) applyOne(op edit.Op) error {
	switch op.Kind {
	case edit.CreateEntry:
		if op.After == nil {
			return fmt.Errorf("a creation needs the entry to create")
		}
		return h.createEntryWithID(op.Target, op.Parent, *op.After)

	case edit.EditEntry:
		if op.After == nil {
			return fmt.Errorf("an edit needs the new values")
		}
		return h.UpdateEntry(op.Target, *op.After)

	case edit.MoveEntry:
		return h.MoveEntry(op.Target, op.Parent)

	case edit.DeleteEntry:
		return h.DeleteEntry(op.Target, op.Perm)

	case edit.CreateGroup:
		return h.createGroupWithID(op.Target, op.Parent, op.Name)

	case edit.RenameGroup:
		return h.RenameGroup(op.Target, op.Name)

	case edit.MoveGroup:
		return h.MoveGroup(op.Target, op.Parent)

	case edit.DeleteGroup:
		return h.DeleteGroup(op.Target, op.Perm)
	}
	return fmt.Errorf("unknown operation")
}

// createEntryWithID creates an entry that must end up with a specific ID.
//
// The ID was handed out when the change was staged, and everything staged after
// it — an edit, a move, a delete — refers to it. Letting the creation mint a
// fresh one here would strand all of those.
func (h *Handle) createEntryWithID(id, parentGroupID string, d edit.Draft) error {
	newID, err := h.CreateEntry(parentGroupID, d)
	if err != nil {
		return err
	}
	if newID == id {
		return nil
	}
	return h.forceID(newID, id)
}

func (h *Handle) createGroupWithID(id, parentGroupID, name string) error {
	newID, err := h.CreateGroup(parentGroupID, name)
	if err != nil {
		return err
	}
	if newID == id {
		return nil
	}
	return h.forceID(newID, id)
}

// forceID rewrites a freshly created item's UUID to the one its ID promised.
//
// Only ever applied to something created moments ago in this same call, so there
// is no existing reference to invalidate — and the alternative, renumbering the
// staged set after the fact, would mean maintaining a second identity space for
// exactly the situation the up-front UUID assignment exists to avoid.
func (h *Handle) forceID(have, want string) error {
	_, wantUUID, _, wantGroup, err := parseID(want)
	if err != nil {
		return err
	}
	_, haveUUID, _, haveGroup, err := parseID(have)
	if err != nil {
		return err
	}
	if wantGroup != haveGroup {
		return fmt.Errorf("cannot turn an entry into a folder")
	}

	if wantGroup {
		_, g, err := h.findGroupByUUID(haveUUID)
		if err != nil {
			return err
		}
		g.UUID = wantUUID
		return nil
	}

	_, e, err := h.findEntry(have)
	if err != nil {
		return err
	}
	e.UUID = wantUUID
	return nil
}
