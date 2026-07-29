package edit

// State reports what has happened to each target, for the in-place decoration in
// the tree and the entry list.
//
// One source of truth, two surfaces: this map and Diff are both derived from the
// same reduced log, so the colour on a row and the line in the Changes tab can
// never disagree about what is staged.
func (s Set) State() map[string]State { return statesOf(s.Effective()) }

// statesOf is State over an already-reduced set, so a caller that has one does
// not pay for the reduction again.
func statesOf(effective []Op) map[string]State {
	out := map[string]State{}
	for _, op := range effective {
		switch op.Kind {
		case CreateEntry, CreateGroup:
			out[op.Target] = New
		case DeleteEntry, DeleteGroup:
			out[op.Target] = Deleted
		case MoveEntry, MoveGroup:
			// A move is only a move if nothing stronger already applies: an entry
			// that was created this session and then moved is still just new.
			if out[op.Target] == Unchanged {
				out[op.Target] = Moved
			}
		case EditEntry, RenameGroup:
			if out[op.Target] == Unchanged {
				out[op.Target] = Modified
			}
		}
	}
	return out
}

// StateOf is State for a single target.
func (s Set) StateOf(target string) State { return s.State()[target] }

// Parents returns, for every changed target, the folder it should be decorated
// under — so a collapsed folder can still show that something inside it changed.
//
// Deletion wins over creation wins over modification: a folder containing both a
// deleted and an edited entry reads as the more consequential of the two.
func (s Set) Parents() map[string]State {
	effective := s.Effective()
	states := statesOf(effective)

	out := map[string]State{}
	for _, op := range effective {
		parent := op.Parent
		if parent == "" && op.Before != nil {
			parent = op.Before.GroupID
		}
		if parent == "" && op.After != nil {
			parent = op.After.GroupID
		}
		if parent == "" {
			continue
		}
		st := states[op.Target]
		if st > out[parent] {
			out[parent] = st
		}
	}
	return out
}
