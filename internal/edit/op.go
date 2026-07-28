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
	Before *Draft // the entry as it was; nil for a creation
	After  *Draft // the entry as it should be; nil for a deletion
	Perm   bool   // delete: permanently, rather than to the recycle bin
	At     time.Time
}

// State is what has happened to one target, as far as the UI is concerned.
type State uint8

const (
	Unchanged State = iota
	New
	Modified
	Moved
	Deleted
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
	}
	return ""
}

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
	}
	return " "
}
