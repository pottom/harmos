package edit

import "time"

// Kind is what an operation does.
type Kind uint8

const (
	CreateEntry Kind = iota + 1
	EditEntry
	MoveEntry
	DeleteEntry
	CreateGroup
	RenameGroup
	MoveGroup
	DeleteGroup
)

func (k Kind) String() string {
	switch k {
	case CreateEntry:
		return "create entry"
	case EditEntry:
		return "edit entry"
	case MoveEntry:
		return "move entry"
	case DeleteEntry:
		return "delete entry"
	case CreateGroup:
		return "create folder"
	case RenameGroup:
		return "rename folder"
	case MoveGroup:
		return "move folder"
	case DeleteGroup:
		return "delete folder"
	}
	return "unknown"
}

// Op is one staged change.
//
// Target is set from the moment the operation is staged, creations included: the
// kdbx UUID is assigned at create time rather than at save time, so an entry that
// does not exist yet still has a stable identity. Everything that follows — an
// edit, a move, a delete, an undo — names the same target, and there is no second
// identity space to translate between when the change set is finally applied.
// That one decision is what makes composition simple enough to reason about.
type Op struct {
	Seq    int // monotonic within a Set; the handle for reverting
	Kind   Kind
	Source string // which source the target lives in
	Target string // entry or folder ID
	Parent string // destination folder, for a create or a move
	Name   string // folder name, for a create or a rename
	// Was is what the target was called, or where it lived, before this
	// operation — the readable form, because the review has to say what changed
	// and "renamed" without the old name only tells the reader half of it. An
	// entry carries its before-state in Before; a folder has no Draft, and a
	// move's origin is not in the op at all, so both need this.
	Was    string
	Before *Draft // the entry as it was; nil for a creation
	After  *Draft // the entry as it should be; nil for a deletion
	Perm   bool   // delete: permanently, rather than to the recycle bin
	At     time.Time
}

// State is what has happened to one target, as far as the UI is concerned.
type State uint8

// The order is a precedence: Parents keeps the highest state of anything inside
// a folder, so a folder holding an edit and a deletion reads as the more
// consequential of the two. Purged is last because a deletion nothing can undo
// is the most consequential thing a session can be holding.
const (
	Unchanged State = iota
	New
	Modified
	Moved
	Deleted // to the recycle bin: recoverable afterwards
	Purged  // gone: no bin, no undo once written
)

func (s State) String() string {
	switch s {
	case New:
		return "new"
	case Modified:
		return "mod"
	case Moved:
		return "moved"
	case Deleted:
		return "del"
	case Purged:
		return "gone"
	}
	return ""
}

// Deleting is "this is on its way out", either way.
//
// A predicate rather than an equality test at every call site: the two deletions
// differ in what they leave behind, not in whether the thing is going, and a
// surface that answered "is this being deleted" with == Deleted would quietly
// say no about the deletion that cannot be undone.
func (s State) Deleting() bool { return s == Deleted || s == Purged }

// Marker is the non-colour cue for a state.
//
// Colour alone is not a signal: it disappears under NO_COLOR, in a mono
// terminal, and for a substantial share of readers. Every state carries a glyph
// and a word as well.
func (s State) Marker() string {
	switch s {
	case New:
		return "+"
	case Modified, Moved:
		return "~"
	case Deleted:
		return "-"
	case Purged:
		// Not another "-". The two deletions are different acts — one leaves the
		// thing in the bin, the other leaves nothing — and a reader who cannot
		// see colour has only this column to tell them apart.
		return "✕"
	}
	return " "
}
