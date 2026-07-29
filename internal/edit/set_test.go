package edit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func draft(title string) *Draft { return &Draft{Title: title} }

func stage(ops ...Op) Set {
	var s Set
	for _, op := range ops {
		if op.Source == "" {
			op.Source = "src"
		}
		s, _ = s.Add(op)
	}
	return s
}

func kinds(ops []Op) []Kind {
	out := make([]Kind, len(ops))
	for i, op := range ops {
		out[i] = op.Kind
	}
	return out
}

func sameKinds(got []Kind, want ...Kind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The composition rules, every pairing that has a rule of its own. These are
// statements about intent — what the user meant — not about bookkeeping, so
// getting one wrong writes something they did not ask for.
func TestComposition(t *testing.T) {
	cases := []struct {
		name  string
		set   Set
		want  []Kind
		state State
		why   string
	}{
		{
			name: "create then edit is one creation with the final values",
			set: stage(
				Op{Kind: CreateEntry, Target: "e1", Parent: "g1", After: draft("first")},
				Op{Kind: EditEntry, Target: "e1", Before: draft("first"), After: draft("final")},
			),
			want:  []Kind{CreateEntry},
			state: New,
			why:   "the entry never existed in its intermediate form",
		},
		{
			name: "create then delete is nothing at all",
			set: stage(
				Op{Kind: CreateEntry, Target: "e1", Parent: "g1", After: draft("gone")},
				Op{Kind: DeleteEntry, Target: "e1"},
			),
			want: nil,
			why:  "the file was never touched, so there is nothing to do or to bin",
		},
		{
			name: "several edits are one edit",
			set: stage(
				Op{Kind: EditEntry, Target: "e1", Before: draft("a"), After: draft("b")},
				Op{Kind: EditEntry, Target: "e1", Before: draft("b"), After: draft("c")},
				Op{Kind: EditEntry, Target: "e1", Before: draft("c"), After: draft("d")},
			),
			want:  []Kind{EditEntry},
			state: Modified,
			why:   "one history record per save, not one per keystroke-batch",
		},
		{
			name: "edit then permanent delete is just the delete",
			set: stage(
				Op{Kind: EditEntry, Target: "e1", Before: draft("a"), After: draft("b")},
				Op{Kind: DeleteEntry, Target: "e1", Perm: true},
			),
			want:  []Kind{DeleteEntry},
			state: Deleted,
			why:   "a history record for an entry about to cease existing is pointless",
		},
		{
			name: "edit then bin keeps both",
			set: stage(
				Op{Kind: EditEntry, Target: "e1", Before: draft("a"), After: draft("b")},
				Op{Kind: DeleteEntry, Target: "e1", Perm: false},
			),
			want:  []Kind{EditEntry, DeleteEntry},
			state: Deleted,
			why:   "what lands in the bin should be what the user last saw",
		},
		{
			name: "moves collapse to the final destination",
			set: stage(
				Op{Kind: MoveEntry, Target: "e1", Parent: "g2"},
				Op{Kind: MoveEntry, Target: "e1", Parent: "g3"},
			),
			want:  []Kind{MoveEntry},
			state: Moved,
		},
		{
			name: "a folder rename and move both survive",
			set: stage(
				Op{Kind: RenameGroup, Target: "g1", Name: "renamed"},
				Op{Kind: MoveGroup, Target: "g1", Parent: "g2"},
			),
			want:  []Kind{RenameGroup, MoveGroup},
			state: Modified,
			why:   "they touch different fields and neither implies the other",
		},
		{
			name: "create folder then rename is one creation with the final name",
			set: stage(
				Op{Kind: CreateGroup, Target: "g9", Parent: "g1", Name: "first"},
				Op{Kind: RenameGroup, Target: "g9", Name: "final"},
			),
			want:  []Kind{CreateGroup},
			state: New,
		},
		{
			name: "edit then move keeps both",
			set: stage(
				Op{Kind: EditEntry, Target: "e1", Before: draft("a"), After: draft("b")},
				Op{Kind: MoveEntry, Target: "e1", Parent: "g2"},
			),
			want:  []Kind{EditEntry, MoveEntry},
			state: Modified,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.set.Effective()
			if !sameKinds(kinds(got), c.want...) {
				t.Errorf("effective = %v, want %v\n%s", kinds(got), c.want, c.why)
			}
			if c.state != Unchanged {
				if st := c.set.StateOf("e1"); st != c.state && c.set.StateOf("g1") != c.state && c.set.StateOf("g9") != c.state {
					t.Errorf("state = %v, want %v", st, c.state)
				}
			}
		})
	}
}

// A create-then-edit must carry the last values written, not the first.
func TestCreateThenEditKeepsTheFinalValues(t *testing.T) {
	s := stage(
		Op{Kind: CreateEntry, Target: "e1", Parent: "g1", After: draft("first")},
		Op{Kind: EditEntry, Target: "e1", Before: draft("first"), After: draft("final")},
	)
	eff := s.Effective()
	if len(eff) != 1 {
		t.Fatalf("effective = %v", kinds(eff))
	}
	if eff[0].After.Title != "final" {
		t.Errorf("creation carries %q, want the final title", eff[0].After.Title)
	}
}

// Several edits must span from the earliest Before to the latest After, so the
// single history record holds the state before the session began.
func TestEditsSpanTheWholeSession(t *testing.T) {
	s := stage(
		Op{Kind: EditEntry, Target: "e1", Before: draft("original"), After: draft("b")},
		Op{Kind: EditEntry, Target: "e1", Before: draft("b"), After: draft("final")},
	)
	eff := s.Effective()
	if len(eff) != 1 {
		t.Fatalf("effective = %v", kinds(eff))
	}
	if eff[0].Before.Title != "original" {
		t.Errorf("before = %q, want the state before the session", eff[0].Before.Title)
	}
	if eff[0].After.Title != "final" {
		t.Errorf("after = %q, want the last values", eff[0].After.Title)
	}
}

// A folder has to be created before anything is put inside it, whatever order
// the reduction happens to visit targets in.
func TestOrderingRespectsDependencies(t *testing.T) {
	s := stage(
		Op{Kind: CreateGroup, Target: "g9", Parent: "g1", Name: "New"},
		Op{Kind: CreateEntry, Target: "e9", Parent: "g9", After: draft("inside")},
		Op{Kind: EditEntry, Target: "e9", Before: draft("inside"), After: draft("edited")},
	)
	eff := s.Effective()
	if len(eff) != 2 {
		t.Fatalf("effective = %v", kinds(eff))
	}
	if eff[0].Kind != CreateGroup {
		t.Errorf("the folder must be created first, got %v", kinds(eff))
	}
}

// Reverting drops an operation and re-derives; there is no inverse to get wrong.
func TestRevert(t *testing.T) {
	var s Set
	s, _ = s.Add(Op{Kind: EditEntry, Source: "src", Target: "e1", Before: draft("a"), After: draft("b")})
	s, second := s.Add(Op{Kind: MoveEntry, Source: "src", Target: "e1", Parent: "g2"})

	s, dropped := s.Revert(second)
	if len(dropped) != 0 {
		t.Errorf("reverting a move should cascade to nothing, dropped %v", kinds(dropped))
	}
	if !sameKinds(kinds(s.Effective()), EditEntry) {
		t.Errorf("effective after revert = %v, want just the edit", kinds(s.Effective()))
	}
	if s.StateOf("e1") != Modified {
		t.Errorf("state = %v, want Modified", s.StateOf("e1"))
	}
}

// Reverting a creation cascades: everything staged against something that will
// no longer exist has to go too, and the caller needs to be told so it can say so.
func TestRevertingACreationCascades(t *testing.T) {
	var s Set
	s, created := s.Add(Op{Kind: CreateEntry, Source: "src", Target: "e1", Parent: "g1", After: draft("a")})
	s, _ = s.Add(Op{Kind: EditEntry, Source: "src", Target: "e1", Before: draft("a"), After: draft("b")})
	s, _ = s.Add(Op{Kind: MoveEntry, Source: "src", Target: "e1", Parent: "g2"})
	s, _ = s.Add(Op{Kind: EditEntry, Source: "src", Target: "other", Before: draft("x"), After: draft("y")})

	if pending := s.Cascade(created); len(pending) != 2 {
		t.Errorf("Cascade should report the 2 dependent ops before doing anything, got %d", len(pending))
	}

	s, dropped := s.Revert(created)
	if len(dropped) != 2 {
		t.Errorf("dropped %d dependent operations, want 2", len(dropped))
	}
	if s.StateOf("e1") != Unchanged {
		t.Errorf("the created entry should be gone from the set, state = %v", s.StateOf("e1"))
	}
	if s.StateOf("other") != Modified {
		t.Error("an unrelated target should be untouched by the cascade")
	}
}

// Reverting from the middle of a chain has to leave a consistent set — which is
// exactly what an inverse-operation undo tends to get wrong.
func TestRevertFromTheMiddle(t *testing.T) {
	var s Set
	s, _ = s.Add(Op{Kind: EditEntry, Source: "src", Target: "e1", Before: draft("a"), After: draft("b")})
	s, middle := s.Add(Op{Kind: MoveEntry, Source: "src", Target: "e1", Parent: "g2"})
	s, _ = s.Add(Op{Kind: EditEntry, Source: "src", Target: "e1", Before: draft("b"), After: draft("c")})

	s, _ = s.Revert(middle)
	eff := s.Effective()
	if !sameKinds(kinds(eff), EditEntry) {
		t.Fatalf("effective = %v, want one edit", kinds(eff))
	}
	if eff[0].Before.Title != "a" || eff[0].After.Title != "c" {
		t.Errorf("edit spans %q → %q, want a → c", eff[0].Before.Title, eff[0].After.Title)
	}
}

func TestRevertUnknownSequenceIsANoOp(t *testing.T) {
	s := stage(Op{Kind: EditEntry, Target: "e1", Before: draft("a"), After: draft("b")})
	after, dropped := s.Revert(999)
	if after.Len() != s.Len() || len(dropped) != 0 {
		t.Error("reverting a sequence that is not staged should change nothing")
	}
}

func TestPerSourceSlicing(t *testing.T) {
	s := stage(
		Op{Kind: EditEntry, Source: "work", Target: "w1", Before: draft("a"), After: draft("b")},
		Op{Kind: EditEntry, Source: "home", Target: "h1", Before: draft("a"), After: draft("b")},
		Op{Kind: EditEntry, Source: "work", Target: "w2", Before: draft("a"), After: draft("b")},
	)

	counts := s.Counts()
	if counts["work"] != 2 || counts["home"] != 1 {
		t.Errorf("counts = %v", counts)
	}
	if got := s.ForSource("work").Len(); got != 2 {
		t.Errorf("ForSource(work) has %d ops, want 2", got)
	}
	after := s.DropSource("work")
	if after.Len() != 1 || after.Counts()["work"] != 0 {
		t.Errorf("DropSource(work) left %v", after.Counts())
	}
	if !strings.Contains(s.Summary(), "work: 2") {
		t.Errorf("summary = %q", s.Summary())
	}
}

// A folder holding a changed entry should be decorated too, or a collapsed tree
// hides the fact that anything is pending inside it.
func TestParentsCarryTheStrongestState(t *testing.T) {
	s := stage(
		Op{Kind: EditEntry, Target: "e1", Before: &Draft{Title: "a", GroupID: "g1"}, After: &Draft{Title: "b", GroupID: "g1"}},
		Op{Kind: DeleteEntry, Target: "e2", Before: &Draft{Title: "c", GroupID: "g1"}},
	)
	if got := s.Parents()["g1"]; got != Deleted {
		t.Errorf("folder state = %v, want the strongest of its children (Deleted)", got)
	}
}

// Every colour-coded state needs a glyph and a word as well, so meaning survives
// NO_COLOR, a mono terminal and colour blindness.
func TestStatesHaveNonColourCues(t *testing.T) {
	for _, st := range []State{New, Modified, Moved, Deleted} {
		if st.Marker() == "" || strings.TrimSpace(st.Marker()) == "" {
			t.Errorf("state %v has no marker glyph", st)
		}
		if st.String() == "" {
			t.Errorf("state %v has no word", st)
		}
	}
	if Unchanged.String() != "" {
		t.Error("the unchanged state should render as nothing")
	}
}

// The review is drawn several times per keystroke, so it cannot re-reduce the
// log per change. It used to: changeOf asked StateOf, which asked State, which
// reduced everything again — quadratic in the number of staged changes.
func TestDiffReducesOncePerCall(t *testing.T) {
	var s Set
	for i := range 400 {
		id := fmt.Sprintf("s:e%03d", i)
		s, _ = s.Add(Op{Kind: EditEntry, Source: "s", Target: id,
			Before: &Draft{ID: id, Title: "before"}, After: &Draft{ID: id, Title: "after"}})
	}

	start := time.Now()
	changes := s.Diff()
	took := time.Since(start)

	if len(changes) != 400 {
		t.Fatalf("expected 400 changes, got %d", len(changes))
	}
	// Generous by two orders of magnitude against the quadratic version, which
	// took ~60ms here, so this fails on the shape rather than on the machine.
	if took > 20*time.Millisecond {
		t.Errorf("reviewing 400 staged changes took %v — the reduction is being repeated", took)
	}
	// And it still says the right thing.
	if changes[0].State != Modified || changes[0].Title != "after" {
		t.Errorf("first change reads wrong: %+v", changes[0])
	}
}
