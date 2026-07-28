package edit

// Revert removes one staged operation and re-derives everything from what is
// left.
//
// There is no inverse-operation machinery, and that is the point: an undo built
// from inverses has to know how to reverse every kind of change, and gets it
// subtly wrong somewhere in the middle of a chain. Dropping the operation from
// the log and reducing again is correct by construction, including for an
// operation in the middle of a sequence.
//
// Reverting a creation cascades: everything staged against something that will
// no longer exist goes with it. Cascaded reports what else was dropped, so the
// confirmation can say so rather than surprising the user.
func (s Set) Revert(seq int) (Set, []Op) {
	var target string
	var isCreate bool
	for _, op := range s.ops {
		if op.Seq == seq {
			target = op.Target
			isCreate = op.Kind == CreateEntry || op.Kind == CreateGroup
		}
	}
	if target == "" {
		return s, nil // nothing by that number; leave the set alone
	}

	var kept, dropped []Op
	for _, op := range s.ops {
		switch {
		case op.Seq == seq:
			continue // the one being reverted
		case isCreate && op.Target == target:
			dropped = append(dropped, op)
		default:
			kept = append(kept, op)
		}
	}

	out := Set{ops: kept, seq: s.seq}
	return out, dropped
}

// Cascade reports what reverting seq would also drop, without doing it — so a
// confirmation can be honest before the user commits to it.
func (s Set) Cascade(seq int) []Op {
	_, dropped := s.Revert(seq)
	return dropped
}

// RevertTarget removes every operation staged against one target, which is what
// "undo my changes to this entry" means.
func (s Set) RevertTarget(target string) Set {
	var kept []Op
	for _, op := range s.ops {
		if op.Target != target {
			kept = append(kept, op)
		}
	}
	return Set{ops: kept, seq: s.seq}
}
