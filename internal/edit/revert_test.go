package edit

import "testing"

// Undoing a folder takes what was staged inside it. The entry had nowhere to be
// created without it: it used to survive, re-parented to the source root in the
// review, and the save failed with "no folder".
func TestRevertCascadesThroughParents(t *testing.T) {
	var s Set
	s, folderSeq := s.Add(Op{Kind: CreateGroup, Source: "s", Target: "s:g:new", Parent: "s:g:root", Name: "Box"})
	s, _ = s.Add(Op{Kind: CreateEntry, Source: "s", Target: "s:e1", Parent: "s:g:new",
		After: &Draft{ID: "s:e1", GroupID: "s:g:new", Title: "inside"}})
	s, _ = s.Add(Op{Kind: CreateGroup, Source: "s", Target: "s:g:deeper", Parent: "s:g:new", Name: "Deeper"})
	s, _ = s.Add(Op{Kind: CreateEntry, Source: "s", Target: "s:e2", Parent: "s:g:deeper",
		After: &Draft{ID: "s:e2", GroupID: "s:g:deeper", Title: "deeper still"}})
	s, _ = s.Add(Op{Kind: EditEntry, Source: "s", Target: "s:untouched",
		Before: &Draft{ID: "s:untouched"}, After: &Draft{ID: "s:untouched", Title: "kept"}})

	if n := len(s.Cascade(folderSeq)); n != 3 {
		t.Errorf("reverting the folder should take 3 changes with it, it reports %d", n)
	}
	out, dropped := s.Revert(folderSeq)
	if len(dropped) != 3 {
		t.Errorf("dropped %d, want the entry, the sub-folder and its entry", len(dropped))
	}
	ops := out.Effective()
	if len(ops) != 1 || ops[0].Target != "s:untouched" {
		t.Errorf("only the unrelated edit should survive, got %+v", ops)
	}
}

// A folder created and then deleted never existed, and neither did what was
// staged inside it.
func TestCreatingAndDeletingAFolderTakesItsContents(t *testing.T) {
	var s Set
	s, _ = s.Add(Op{Kind: CreateGroup, Source: "s", Target: "s:g:doomed", Parent: "s:g:root", Name: "Doomed"})
	s, _ = s.Add(Op{Kind: CreateEntry, Source: "s", Target: "s:orphan", Parent: "s:g:doomed",
		After: &Draft{ID: "s:orphan", GroupID: "s:g:doomed", Title: "orphan"}})
	s, _ = s.Add(Op{Kind: DeleteGroup, Source: "s", Target: "s:g:doomed", Name: "Doomed"})

	if ops := s.Effective(); len(ops) != 0 {
		t.Errorf("the file never saw any of it, but the set still holds %+v", ops)
	}
}
