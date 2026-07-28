package vault

import (
	"slices"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
)

// KeePass history: before an entry is changed, its previous state is pushed onto
// a stack inside the entry itself, so other clients can show and restore it.
// harmos writes it because not writing it would silently drop the history other
// clients have been keeping.
//
// Three details, each of which produces a subtly broken file if missed:
//
//   - The snapshot must keep the entry's UUID. gokeepasslib's Clone assigns a
//     new one, so it cannot be used here — a history record carries its parent's
//     UUID by definition.
//   - The snapshot must have its own Histories cleared, or each save nests the
//     previous history inside the new one and the file grows without bound.
//   - Entry.Histories is `[]History` where the schema expects a single <History>
//     element. Always append into Histories[0]; two elements marshal as two
//     siblings, which is not the shape a reader expects.

// pushHistory records the entry's current state before it is modified, and
// prunes the stack to the limits the file itself specifies.
//
// It is a no-op when the database has history disabled, which is the file
// owner's setting to make, not ours.
func pushHistory(db *gokeepasslib.Database, e *gokeepasslib.Entry) {
	maxItems, maxSize := historyLimits(db)
	if maxItems == 0 || maxSize == 0 {
		return // history is switched off for this database
	}

	snap := snapshotEntry(e)

	if len(e.Histories) == 0 {
		e.Histories = []gokeepasslib.History{{}}
	}
	e.Histories[0].Entries = append(e.Histories[0].Entries, snap)

	prune(&e.Histories[0], maxItems, maxSize)
}

// snapshotEntry deep-copies an entry for the history stack: same UUID, no nested
// history, and no slice or pointer left shared with the live entry — otherwise
// the later edit would reach through and rewrite the record meant to preserve
// what came before it.
func snapshotEntry(e *gokeepasslib.Entry) gokeepasslib.Entry {
	snap := *e
	snap.Histories = nil
	snap.Times = cloneTimes(e.Times)
	snap.Values = slices.Clone(e.Values)
	snap.Binaries = slices.Clone(e.Binaries)
	snap.CustomData = slices.Clone(e.CustomData)
	snap.AutoType.Associations = slices.Clone(e.AutoType.Associations)
	if e.PreviousParentGroup != nil {
		u := *e.PreviousParentGroup
		snap.PreviousParentGroup = &u
	}
	if e.QualityCheck != nil {
		q := *e.QualityCheck
		snap.QualityCheck = &q
	}
	return snap
}

// historyLimits reads the database's own limits. KeePass's convention: a
// negative value means unlimited, zero means keep no history at all.
func historyLimits(db *gokeepasslib.Database) (maxItems int64, maxSize int64) {
	maxItems, maxSize = 10, 6*1024*1024 // KeePass's defaults, if the file says nothing
	if db.Content != nil && db.Content.Meta != nil {
		maxItems = db.Content.Meta.HistoryMaxItems
		maxSize = db.Content.Meta.HistoryMaxSize
	}
	return maxItems, maxSize
}

// prune drops the oldest records until the stack fits. Records are appended in
// chronological order, so the oldest are at the front.
func prune(h *gokeepasslib.History, maxItems, maxSize int64) {
	if maxItems > 0 && int64(len(h.Entries)) > maxItems {
		h.Entries = slices.Delete(h.Entries, 0, len(h.Entries)-int(maxItems))
	}
	if maxSize <= 0 {
		return
	}
	// The size limit is on the serialised history, which we cannot measure
	// without encoding it. Approximating from the field contents errs towards
	// keeping records — losing history to a conservative estimate would be a
	// worse failure than a file marginally over its own advisory limit.
	for len(h.Entries) > 1 && historySize(h) > maxSize {
		h.Entries = slices.Delete(h.Entries, 0, 1)
	}
}

func historySize(h *gokeepasslib.History) int64 {
	var n int64
	for i := range h.Entries {
		// Values covers every field, Notes included — they are all entries in
		// the same list.
		for _, v := range h.Entries[i].Values {
			n += int64(len(v.Key) + len(v.Value.Content))
		}
	}
	return n
}
