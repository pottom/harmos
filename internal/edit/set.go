package edit

import (
	"sort"
	"time"
)

// Set is the staged change set for a session.
//
// Every operation stays in the log. The Changes tab renders the log, and
// reverting is "remove this one and work it out again" — which needs the
// originals. What gets applied to the file is *derived* from the log rather than
// accumulated into it, so there is no partially-merged state to get wrong.
type Set struct {
	ops []Op
	seq int
}

// Len is the number of staged operations, before reduction.
func (s Set) Len() int { return len(s.ops) }

// Ops returns the log, oldest first.
func (s Set) Ops() []Op {
	out := make([]Op, len(s.ops))
	copy(out, s.ops)
	return out
}

// Empty reports whether anything is staged.
func (s Set) Empty() bool { return len(s.ops) == 0 }

// Add stages an operation and returns the updated set and the sequence number to
// revert it by. Set is a value type — like the TUI model it will live in — so the
// caller keeps the result.
func (s Set) Add(op Op) (Set, int) {
	s.seq++
	op.Seq = s.seq
	if op.At.IsZero() {
		op.At = time.Now()
	}
	// The set owns what it is holding. Before and After are pointers into the
	// caller's world, and a caller that keeps editing the value it passed —
	// carrying one draft forward through a sequence of edits, say — would be
	// rewriting history that has already been staged. The reduction compares the
	// two to spot a change that came back to where it started, so an aliased
	// Before would make every such edit look like a no-op.
	op.Before, op.After = op.Before.clone(), op.After.clone()
	s.ops = append(append([]Op(nil), s.ops...), op)
	return s, op.Seq
}

// Effective reduces the log to the operations that would actually be performed,
// in the order to perform them.
//
// The reduction is per target, and the rules are about intent rather than
// bookkeeping:
//
//   - create then edit is one creation carrying the final values — the entry
//     never existed in its intermediate forms, so there is nothing to record.
//   - create then delete is nothing at all. The file was never touched.
//   - several edits are one edit from the earliest Before to the latest After,
//     which is what makes the KeePass history hold one record per save rather
//     than one per keystroke-batch.
//   - an edit followed by a permanent delete is just the delete; writing a
//     history record for an entry about to cease existing is pointless.
//   - an edit followed by a recycle-bin delete keeps both, so what lands in the
//     bin is what the user last saw.
//   - moves collapse to the final destination.
//   - a rename and a move of the same folder both survive: they touch different
//     fields and neither implies the other.
//
// Ordering is by the earliest surviving operation for each target, so a folder
// is created before anything is put inside it.
func (s Set) Effective() []Op {
	type bucket struct {
		ops []Op
	}
	order := []string{}
	byTarget := map[string]*bucket{}

	for _, op := range s.ops {
		b, ok := byTarget[op.Target]
		if !ok {
			b = &bucket{}
			byTarget[op.Target] = b
			order = append(order, op.Target)
		}
		b.ops = append(b.ops, op)
	}

	var out []Op
	for _, target := range order {
		b := byTarget[target]
		out = append(out, reduceTarget(b.ops)...)
	}

	// A folder created and then deleted never existed, and neither did anything
	// staged inside it. Without this the children survive as creations pointing
	// at a parent that is not in the set, and the save fails with "no folder".
	if gone := cancelled(s.ops, out); len(gone) > 0 {
		var kept []Op
		for _, op := range out {
			if !gone[op.Target] && !gone[op.Parent] {
				kept = append(kept, op)
			}
		}
		out = kept
	}

	// A deletion inside a folder that is also being deleted is not a second
	// deletion — it is the same one. Applying both moved the child out of the
	// folder it went with: on disk the bin held the child at its root and the
	// folder beside it. Whichever order they were staged in.
	out = subsumeInsideDeletedFolders(out)

	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// subsumeInsideDeletedFolders drops entry deletions whose folder is going too.
//
// It can only see the folder an entry was in — the change set holds identities
// and operations, not the vault's shape — so this closes the direct-child case
// for every caller. Deeper nesting is the interface's to know, and it does.
func subsumeInsideDeletedFolders(ops []Op) []Op {
	doomed := map[string]bool{}
	for _, op := range ops {
		if op.Kind == DeleteGroup {
			doomed[op.Target] = true
		}
	}
	if len(doomed) == 0 {
		return ops
	}
	var kept []Op
	for _, op := range ops {
		if op.Kind == DeleteEntry && op.Before != nil && doomed[op.Before.GroupID] {
			continue
		}
		kept = append(kept, op)
	}
	return kept
}

// cancelled is the targets that were created and then un-created by the
// reduction, closed over what was staged inside them.
func cancelled(ops, effective []Op) map[string]bool {
	survives := map[string]bool{}
	for _, op := range effective {
		survives[op.Target] = true
	}
	gone := map[string]bool{}
	for _, op := range ops {
		if (op.Kind == CreateEntry || op.Kind == CreateGroup) && !survives[op.Target] {
			gone[op.Target] = true
		}
	}
	if len(gone) > 0 {
		grow(ops, gone)
	}
	return gone
}

// samePlace reports whether an edit ends where it started — every field back to
// the value the file has.
//
// It asks the same comparison the review does, deliberately: "the Changes tab
// shows no lines for this" and "there is nothing staged for this" have to be one
// answer, or a session sits there looking dirty with an empty diff under it.
func samePlace(before, after *Draft) bool {
	if before == nil || after == nil {
		return false // a creation or a deletion is never a no-op
	}
	return len(diffDrafts(before, after)) == 0
}

// reduceTarget collapses one target's operations. The input is in staging order.
func reduceTarget(ops []Op) []Op {
	var (
		created *Op
		edited  *Op
		moved   *Op
		renamed *Op
		deleted *Op
		madeDir *Op
	)

	for i := range ops {
		op := ops[i]
		switch op.Kind {
		case CreateEntry:
			c := op
			created = &c
		case CreateGroup:
			c := op
			madeDir = &c
		case EditEntry:
			if edited == nil {
				e := op
				edited = &e
			} else {
				// Keep the earliest Before and the latest After, so one history
				// record spans the whole session's editing of this entry.
				edited.After = op.After
				edited.Seq = min(edited.Seq, op.Seq)
			}
		case MoveEntry, MoveGroup:
			m := op
			if moved != nil {
				// The destination is the latest one, but "where it was" is where
				// the *file* has it — the first move's origin. Taking the last
				// op's Was named a folder the change never came from, because by
				// then the projection already showed it in the interim one.
				m.Was, m.Seq = moved.Was, min(moved.Seq, op.Seq)
			}
			moved = &m
		case RenameGroup:
			r := op
			if renamed != nil {
				r.Was, r.Seq = renamed.Was, min(renamed.Seq, op.Seq)
			}
			renamed = &r
		case DeleteEntry, DeleteGroup:
			d := op
			deleted = &d
		}
	}

	// Created and then deleted in the same session: the file never saw it.
	if (created != nil || madeDir != nil) && deleted != nil {
		return nil
	}

	var out []Op

	switch {
	case created != nil:
		// The creation carries the final values; the intermediate edits never
		// existed as far as the file is concerned.
		c := *created
		if edited != nil {
			c.After = edited.After
		}
		out = append(out, c)
		if moved != nil {
			out = append(out, *moved)
		}
	case madeDir != nil:
		c := *madeDir
		if renamed != nil {
			c.Name = renamed.Name
		}
		out = append(out, c)
		if moved != nil {
			out = append(out, *moved)
		}
	default:
		// An existing item.
		// An edit survives unless the item is about to cease existing: writing a
		// history record for something being permanently deleted is pointless.
		permanentlyGone := deleted != nil && deleted.Perm
		if edited != nil && !permanentlyGone && !samePlace(edited.Before, edited.After) {
			out = append(out, *edited)
		}
		// A rename back to the name the file already has is not a rename. The
		// staging guard cannot catch this: it compares against the name on the
		// row, which is the interim one. Only the reduction knows where the
		// round trip started.
		if renamed != nil && deleted == nil && renamed.Was != renamed.Name {
			out = append(out, *renamed)
		}
		// The same for a move that ends where it began.
		if moved != nil && deleted == nil && moved.Was != moved.Parent {
			out = append(out, *moved)
		}
		if deleted != nil {
			out = append(out, *deleted)
		}
	}
	return out
}

// Counts is the number of effective changes per source, for a dirty badge and
// for the save confirmation.
func (s Set) Counts() map[string]int {
	out := map[string]int{}
	seen := map[string]bool{}
	for _, op := range s.Effective() {
		if seen[op.Target] {
			continue
		}
		seen[op.Target] = true
		out[op.Source]++
	}
	return out
}

// Sources lists the sources with staged changes, alphabetically.
func (s Set) Sources() []string {
	var out []string
	for src := range s.Counts() {
		out = append(out, src)
	}
	sort.Strings(out)
	return out
}

// ForSource is the subset staged against one source, so a save can write one
// file without touching another's changes.
func (s Set) ForSource(source string) Set {
	var out Set
	out.seq = s.seq
	for _, op := range s.ops {
		if op.Source == source {
			out.ops = append(out.ops, op)
		}
	}
	return out
}

// DropSource removes one source's operations, for after that file is saved.
func (s Set) DropSource(source string) Set {
	var out Set
	out.seq = s.seq
	for _, op := range s.ops {
		if op.Source != source {
			out.ops = append(out.ops, op)
		}
	}
	return out
}
