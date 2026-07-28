package vault

import (
	"strings"
	"testing"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// stageCreate is the sequence the TUI will perform: mint the target's identity by
// creating it, then throw the creation away and stage it instead. The point is
// that the ID exists before the file does, so everything staged afterwards can
// name it.
func stageCreate(t *testing.T, h *Handle, set edit.Set, parent string, d edit.Draft) (edit.Set, string) {
	t.Helper()
	id, err := h.CreateEntry(parent, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.DeleteEntry(id, true); err != nil {
		t.Fatal(err)
	}
	h.db.Content.Root.DeletedObjects = nil // the staging probe leaves no trace

	d.ID = id
	set, _ = set.Add(edit.Op{
		Kind: edit.CreateEntry, Source: h.source, Target: id, Parent: parent, After: &d,
	})
	return set, id
}

// Applying a reduced set is what makes the file record what the user meant
// rather than the path they took to mean it.
func TestApplyCreateThenEditWritesOneEntryAndNoHistory(t *testing.T) {
	h, p := openFixture(t)
	folder := folderID(t, h)

	var set edit.Set
	set, id := stageCreate(t, h, set, folder, edit.Draft{Title: "first", Password: secret.New("pw1")})

	final := edit.Draft{ID: id, Title: "final", Password: secret.New("pw2")}
	set, _ = set.Add(edit.Op{
		Kind: edit.EditEntry, Source: h.source, Target: id,
		Before: &edit.Draft{ID: id, Title: "first"}, After: &final,
	})

	if err := h.Apply(set); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	v, err := Open(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	var got *Entry
	for i := range v.Entries {
		if v.Entries[i].ID == id {
			got = &v.Entries[i]
		}
	}
	if got == nil {
		t.Fatalf("the created entry is missing; ids: %v", ids(v.Entries))
	}
	if got.Title != "final" {
		t.Errorf("title = %q, want the final value", got.Title)
	}
	if got.Password.Reveal() != "pw2" {
		t.Error("the final password should have been written")
	}

	_, e, err := h.findEntry(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Histories) != 0 {
		t.Error("an entry created this session has no previous state, so no history record")
	}
}

// Created and then deleted: the file must never have heard of it, and nothing
// should land in the recycle bin either.
func TestApplyCreateThenDeleteTouchesNothing(t *testing.T) {
	h, p := openFixture(t, vaulttest.RecycleBin())
	folder := folderID(t, h)
	before := len(h.Snapshot().Entries)

	var set edit.Set
	set, id := stageCreate(t, h, set, folder, edit.Draft{Title: "ephemeral"})
	set, _ = set.Add(edit.Op{Kind: edit.DeleteEntry, Source: h.source, Target: id})

	if n := len(set.Effective()); n != 0 {
		t.Fatalf("effective operations = %d, want none", n)
	}
	if err := h.Apply(set); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	v, err := Open(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Entries) != before {
		t.Errorf("entries = %d, want the original %d", len(v.Entries), before)
	}
	for _, e := range v.Entries {
		if strings.Contains(e.Path, recycleBinName) {
			t.Error("an entry that never existed should not reach the recycle bin")
		}
	}
	if len(h.db.Content.Root.DeletedObjects) != 0 {
		t.Error("and should leave no tombstone")
	}
}

// Several edits collapse to one, which is what keeps the KeePass history to one
// record per save instead of one per keystroke-batch.
func TestApplyManyEditsWriteOneHistoryRecord(t *testing.T) {
	h, _ := openFixture(t)
	id := h.Snapshot().Entries[0].ID

	original, err := h.EntryDraft(id)
	if err != nil {
		t.Fatal(err)
	}

	var set edit.Set
	prev := original
	for _, title := range []string{"one", "two", "three"} {
		next := prev
		next.Title = title
		set, _ = set.Add(edit.Op{
			Kind: edit.EditEntry, Source: h.source, Target: id,
			Before: &prev, After: &next,
		})
		prev = next
	}

	if err := h.Apply(set); err != nil {
		t.Fatal(err)
	}

	_, e, err := h.findEntry(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Histories) != 1 || len(e.Histories[0].Entries) != 1 {
		t.Fatalf("history should hold exactly one record for the session, got %d element(s)", len(e.Histories))
	}
	if got := e.Histories[0].Entries[0].GetTitle(); got != original.Title {
		t.Errorf("the record holds %q, want the state before the session began (%q)", got, original.Title)
	}
	if e.GetTitle() != "three" {
		t.Errorf("live title = %q, want the last value", e.GetTitle())
	}
}

// A folder created in the same set must exist before an entry is placed in it.
func TestApplyOrdersDependencies(t *testing.T) {
	h, p := openFixture(t)
	parent := folderID(t, h)

	// Mint both identities against the live tree, then take them back out so
	// that applying the set is what really creates them. This mirrors what the
	// TUI does: the ID is handed out when the change is staged, long before the
	// file has heard of it.
	gid, err := h.CreateGroup(parent, "Staged")
	if err != nil {
		t.Fatal(err)
	}
	entryDraft := edit.Draft{Title: "inside"}
	eid, err := h.CreateEntry(gid, entryDraft)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.DeleteGroup(gid, true); err != nil { // takes the entry with it
		t.Fatal(err)
	}
	h.db.Content.Root.DeletedObjects = nil
	entryDraft.ID = eid

	var set edit.Set
	set, _ = set.Add(edit.Op{
		Kind: edit.CreateGroup, Source: h.source, Target: gid, Parent: parent, Name: "Staged",
	})
	set, _ = set.Add(edit.Op{
		Kind: edit.CreateEntry, Source: h.source, Target: eid, Parent: gid, After: &entryDraft,
	})

	if err := h.Apply(set); err != nil {
		t.Fatalf("applying a folder and its contents together: %v", err)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	v, err := Open(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range v.Entries {
		if e.ID == eid {
			if e.Path != "Infra/Staged" {
				t.Errorf("path = %q, want Infra/Staged", e.Path)
			}
			return
		}
	}
	t.Errorf("the entry is not in the reopened vault; ids: %v", ids(v.Entries))
}

// Only this source's changes are ours to make; another source's are somebody
// else's file.
func TestApplyIgnoresOtherSources(t *testing.T) {
	h, _ := openFixture(t)
	before := len(h.Snapshot().Entries)

	var set edit.Set
	set, _ = set.Add(edit.Op{
		Kind: edit.CreateEntry, Source: "somewhere-else", Target: "somewhere-else:AAAA",
		Parent: "somewhere-else:g:AAAA", After: &edit.Draft{Title: "not ours"},
	})

	if err := h.Apply(set); err != nil {
		t.Fatalf("another source's changes should be skipped, not fail: %v", err)
	}
	if got := len(h.Snapshot().Entries); got != before {
		t.Errorf("entries %d -> %d", before, got)
	}
}

func TestApplyRefusedOnAnUnwritableSource(t *testing.T) {
	h, _ := openFixture(t, vaulttest.KDBX31())

	var set edit.Set
	set, _ = set.Add(edit.Op{
		Kind: edit.EditEntry, Source: h.source, Target: "x",
		Before: &edit.Draft{}, After: &edit.Draft{Title: "no"},
	})
	if err := h.Apply(set); err == nil {
		t.Error("applying to a source harmos will not write should be refused")
	}
}

// Applying an empty set is a no-op, not an error — the save path will call it
// whether or not anything is staged for this particular file.
func TestApplyEmptySet(t *testing.T) {
	h, _ := openFixture(t)
	before := len(h.Snapshot().Entries)
	if err := h.Apply(edit.Set{}); err != nil {
		t.Fatal(err)
	}
	if got := len(h.Snapshot().Entries); got != before {
		t.Errorf("entries %d -> %d", before, got)
	}
}

func ids(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}
