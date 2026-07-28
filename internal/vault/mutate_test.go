package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// folderID returns the ID of the fixture's one folder, "Infra".
func folderID(t *testing.T, h *Handle) string {
	t.Helper()
	for _, f := range h.Snapshot().Folders {
		if f.Name == "Infra" {
			return f.ID
		}
	}
	t.Fatal("fixture should have an Infra folder")
	return ""
}

// An empty folder used to be invisible: the tree was inferred from entry paths,
// so a folder with nothing in it did not exist as far as the UI was concerned —
// including one the user had just created.
func TestSnapshotListsEmptyFolders(t *testing.T) {
	h, _ := openFixture(t)

	id, err := h.CreateGroup(folderID(t, h), "Empty")
	if err != nil {
		t.Fatal(err)
	}

	var found *Folder
	for _, f := range h.Snapshot().Folders {
		if f.ID == id {
			found = &f
		}
	}
	if found == nil {
		t.Fatal("a folder with no entries should still be listed")
	}
	if found.Name != "Empty" || found.Path != "Infra/Empty" {
		t.Errorf("folder = %+v, want name Empty at Infra/Empty", *found)
	}
}

// The ID has to survive a save and reload, or nothing can point at an entry
// across the moment its edits are written.
func TestEntryIDSurvivesSaveAndReopen(t *testing.T) {
	h, p := openFixture(t)
	before := h.Snapshot().Entries[0].ID

	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenHandle(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if after := reopened.Snapshot().Entries[0].ID; after != before {
		t.Errorf("id changed across a save: %q -> %q", before, after)
	}
}

func TestCreateEntry(t *testing.T) {
	h, p := openFixture(t)

	id, err := h.CreateEntry(folderID(t, h), Draft{
		Title:    "new-thing",
		Username: "someone",
		Password: secret.New("s3cret"),
		Tags:     "a;b",
		Fields:   []DraftField{{Key: "pps.cuf.Env", Value: "prod"}},
	})
	if err != nil {
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
		t.Fatalf("the new entry is not in the reopened vault: %+v", v.Entries)
	}
	if got.Title != "new-thing" || got.Username != "someone" {
		t.Errorf("fields wrong: %+v", *got)
	}
	if got.Password.Reveal() != "s3cret" {
		t.Error("password did not survive")
	}
	if got.Path != "Infra" {
		t.Errorf("path = %q, want Infra", got.Path)
	}
	// The prefixed key is stored raw and displayed trimmed.
	if len(got.Custom) != 1 || got.Custom[0].Name != "Env" || got.Custom[0].Value != "prod" {
		t.Errorf("custom field = %+v", got.Custom)
	}
}

// Editing pushes the previous state onto the KeePass history stack, which is how
// every other client shows and restores it. The record must carry the entry's
// own UUID — gokeepasslib's Clone assigns a new one, which is why the snapshot
// is hand-written.
func TestUpdateEntryWritesHistory(t *testing.T) {
	h, _ := openFixture(t)
	id := h.Snapshot().Entries[0].ID

	d, err := h.EntryDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	original := d.Title
	d.Title = "renamed"
	if err := h.UpdateEntry(id, d); err != nil {
		t.Fatal(err)
	}

	_, e, err := h.findEntry(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Histories) != 1 {
		t.Fatalf("history should be a single <History> element, got %d", len(e.Histories))
	}
	if n := len(e.Histories[0].Entries); n != 1 {
		t.Fatalf("history records = %d, want 1", n)
	}
	rec := e.Histories[0].Entries[0]
	if rec.UUID != e.UUID {
		t.Error("a history record must carry the entry's own UUID")
	}
	if rec.GetTitle() != original {
		t.Errorf("history holds %q, want the pre-edit title %q", rec.GetTitle(), original)
	}
	if len(rec.Histories) != 0 {
		t.Error("a history record must not nest its own history")
	}
	if e.GetTitle() != "renamed" {
		t.Errorf("live entry title = %q", e.GetTitle())
	}
}

// The record must keep its own timestamps. TimeData holds pointers, so a shallow
// copy would let the live entry's next edit rewrite the history's idea of when
// things happened.
func TestHistoryKeepsItsOwnTimestamps(t *testing.T) {
	h, _ := openFixture(t)
	id := h.Snapshot().Entries[0].ID

	d, _ := h.EntryDraft(id)
	d.Title = "first"
	if err := h.UpdateEntry(id, d); err != nil {
		t.Fatal(err)
	}
	_, e, _ := h.findEntry(id)
	recorded := *e.Histories[0].Entries[0].Times.LastModificationTime

	d.Title = "second"
	if err := h.UpdateEntry(id, d); err != nil {
		t.Fatal(err)
	}
	_, e, _ = h.findEntry(id)
	if got := *e.Histories[0].Entries[0].Times.LastModificationTime; got != recorded {
		t.Error("a later edit rewrote an existing history record's timestamp")
	}
	if len(e.Histories[0].Entries) != 2 {
		t.Errorf("history records = %d, want 2", len(e.Histories[0].Entries))
	}
}

// Editing must not backdate creation. NewTimeData aims four fields at one shared
// pointer, so touching "modified" in place would move "created" with it.
func TestEditDoesNotRewriteCreationTime(t *testing.T) {
	h, _ := openFixture(t)
	id := h.Snapshot().Entries[0].ID

	_, e, _ := h.findEntry(id)
	created := *e.Times.CreationTime

	d, _ := h.EntryDraft(id)
	d.Title = "touched"
	if err := h.UpdateEntry(id, d); err != nil {
		t.Fatal(err)
	}

	_, e, _ = h.findEntry(id)
	if *e.Times.CreationTime != created {
		t.Error("editing an entry rewrote its creation time")
	}
	if e.Times.LastModificationTime == e.Times.CreationTime {
		t.Error("creation and modification share a pointer; one edit writes both")
	}
}

func TestMoveEntryRecordsWhereItCameFrom(t *testing.T) {
	h, _ := openFixture(t)
	from := folderID(t, h)
	to, err := h.CreateGroup(from, "Elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	id := h.Snapshot().Entries[0].ID

	if err := h.MoveEntry(id, to); err != nil {
		t.Fatal(err)
	}

	var moved *Entry
	for _, e := range h.Snapshot().Entries {
		if e.ID == id {
			moved = &e
		}
	}
	if moved == nil {
		t.Fatal("the entry vanished")
	}
	if moved.Path != "Infra/Elsewhere" {
		t.Errorf("path = %q, want Infra/Elsewhere", moved.Path)
	}
	if moved.GroupID != to {
		t.Errorf("group = %q, want %q", moved.GroupID, to)
	}

	_, e, _ := h.findEntry(id)
	if e.PreviousParentGroup == nil {
		t.Error("a move should record the previous parent, so it can be undone")
	}
}

func TestDeleteEntryToRecycleBin(t *testing.T) {
	h, _ := openFixture(t, vaulttest.RecycleBin())
	if !h.RecycleBinEnabled() {
		t.Fatal("the fixture asked for a recycle bin and should have one")
	}
	id := h.Snapshot().Entries[0].ID

	if err := h.DeleteEntry(id, false); err != nil {
		t.Fatal(err)
	}

	var binned *Entry
	for _, e := range h.Snapshot().Entries {
		if e.ID == id {
			binned = &e
		}
	}
	if binned == nil {
		t.Fatal("a binned entry should still exist, in the bin")
	}
	if !strings.Contains(binned.Path, recycleBinName) {
		t.Errorf("path = %q, should be inside the recycle bin", binned.Path)
	}
	if isZeroUUID(h.db.Content.Meta.RecycleBinUUID) {
		t.Error("creating the bin should record it in the metadata")
	}
	if len(h.db.Content.Root.DeletedObjects) != 0 {
		t.Error("binning is not deleting; there should be no tombstone yet")
	}
}

func TestDeleteEntryPermanentlyLeavesATombstone(t *testing.T) {
	h, _ := openFixture(t)
	id := h.Snapshot().Entries[0].ID
	_, e, _ := h.findEntry(id)
	uuid := e.UUID

	if err := h.DeleteEntry(id, true); err != nil {
		t.Fatal(err)
	}

	if len(h.Snapshot().Entries) != 0 {
		t.Error("the entry should be gone")
	}
	found := false
	for _, d := range h.db.Content.Root.DeletedObjects {
		if d.UUID == uuid {
			found = true
			if d.DeletionTime == nil {
				t.Error("a tombstone with no deletion time panics the format walk")
			}
		}
	}
	if !found {
		t.Error("a permanent delete should leave a tombstone, so other clients learn of it")
	}
}

// With the bin switched off, "delete to bin" is really permanent. The caller has
// to be able to find that out, or a confirmation prompt promises a recoverable
// delete that is not one.
func TestDeleteIsPermanentWhenBinIsDisabled(t *testing.T) {
	h, _ := openFixture(t)
	h.db.Content.Meta.RecycleBinEnabled = boolWrapper(false)

	if h.RecycleBinEnabled() {
		t.Fatal("RecycleBinEnabled should report the database's setting")
	}
	id := h.Snapshot().Entries[0].ID
	if err := h.DeleteEntry(id, false); err != nil {
		t.Fatal(err)
	}
	if len(h.Snapshot().Entries) != 0 {
		t.Error("with no bin, a delete removes the entry outright")
	}
	if len(h.db.Content.Root.DeletedObjects) != 1 {
		t.Error("and leaves a tombstone")
	}
}

func TestGroupCreateRenameMoveDelete(t *testing.T) {
	h, _ := openFixture(t)
	infra := folderID(t, h)

	child, err := h.CreateGroup(infra, "Child")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.RenameGroup(child, "Renamed"); err != nil {
		t.Fatal(err)
	}

	sibling, err := h.CreateGroup(infra, "Sibling")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.MoveGroup(child, sibling); err != nil {
		t.Fatal(err)
	}

	var got *Folder
	for _, f := range h.Snapshot().Folders {
		if f.ID == child {
			got = &f
		}
	}
	if got == nil {
		t.Fatal("the folder vanished")
	}
	if got.Name != "Renamed" || got.Path != "Infra/Sibling/Renamed" {
		t.Errorf("folder = %+v", *got)
	}

	if err := h.DeleteGroup(child, true); err != nil {
		t.Fatal(err)
	}
	for _, f := range h.Snapshot().Folders {
		if f.ID == child {
			t.Error("the folder should be gone")
		}
	}
}

// Deleting a folder takes its contents with it, so every one of them needs its
// own tombstone — a client syncing an older copy would otherwise restore them.
func TestDeleteGroupTombstonesTheWholeSubtree(t *testing.T) {
	h, _ := openFixture(t)
	infra := folderID(t, h)

	child, err := h.CreateGroup(infra, "Child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.CreateEntry(child, Draft{Title: "doomed"}); err != nil {
		t.Fatal(err)
	}
	grandchild, err := h.CreateGroup(child, "Grandchild")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.CreateEntry(grandchild, Draft{Title: "also doomed"}); err != nil {
		t.Fatal(err)
	}

	if err := h.DeleteGroup(child, true); err != nil {
		t.Fatal(err)
	}
	// child + grandchild + two entries
	if n := len(h.db.Content.Root.DeletedObjects); n != 4 {
		t.Errorf("tombstones = %d, want 4 (the folder, its subfolder and both entries)", n)
	}
}

// Moving a folder into its own subtree would detach the branch from the tree
// entirely, taking everything in it with it.
func TestMoveGroupRefusesToSwallowItself(t *testing.T) {
	h, _ := openFixture(t)
	parent, err := h.CreateGroup(folderID(t, h), "Parent")
	if err != nil {
		t.Fatal(err)
	}
	child, err := h.CreateGroup(parent, "Child")
	if err != nil {
		t.Fatal(err)
	}

	if err := h.MoveGroup(parent, child); err == nil {
		t.Error("moving a folder into its own descendant should be refused")
	}
	if err := h.MoveGroup(parent, parent); err == nil {
		t.Error("moving a folder into itself should be refused")
	}
}

func TestRootGroupIsNotMovableOrDeletable(t *testing.T) {
	h, _ := openFixture(t)

	var rootID string
	h.walkTree(func(_ *gokeepasslib.Group, id, parentID, _ string) {
		if parentID == "" {
			rootID = id
		}
	}, nil)

	if err := h.DeleteGroup(rootID, true); err == nil {
		t.Error("deleting the root folder should be refused")
	}
	if err := h.MoveGroup(rootID, folderID(t, h)); err == nil {
		t.Error("moving the root folder should be refused")
	}
}

// A draft is the lossless view: the raw custom-field key and the raw tag string,
// not the trimmed and split forms the display projection produces.
func TestDraftIsRaw(t *testing.T) {
	h, _ := openFixture(t)
	id := h.Snapshot().Entries[0].ID

	d, err := h.EntryDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Tags != "infra;prod" {
		t.Errorf("tags = %q, want the raw string infra;prod", d.Tags)
	}
	var keys []string
	for _, f := range d.Fields {
		keys = append(keys, f.Key)
	}
	if !contains(keys, "pps.cuf.Environment") {
		t.Errorf("custom field keys = %v, want the raw pps.cuf.Environment", keys)
	}
	if !contains(keys, "Recovery") {
		t.Errorf("custom field keys = %v, want the protected Recovery field", keys)
	}
	for _, f := range d.Fields {
		if f.Key == "Recovery" && !f.Protected {
			t.Error("Recovery should keep its protected flag")
		}
	}
	if d.Password.Reveal() != "secret-pw" {
		t.Error("draft should carry the real password")
	}
	if !strings.HasPrefix(d.TOTP, "otpauth://") {
		t.Errorf("draft should carry the otp uri, got %q", d.TOTP)
	}
}

// Everything the library models but harmos does not project has to survive an
// edit — otherwise editing a title would quietly strip a user's auto-type rules.
func TestEditPreservesUnprojectedData(t *testing.T) {
	h, _ := openFixture(t)
	id := h.Snapshot().Entries[0].ID

	_, e, _ := h.findEntry(id)
	e.OverrideURL = "cmd://open {URL}"
	e.ForegroundColor = "#FF0000"
	e.AutoType.DefaultSequence = "{USERNAME}{TAB}{PASSWORD}"
	binaries := len(e.Binaries)

	d, _ := h.EntryDraft(id)
	d.Title = "edited"
	if err := h.UpdateEntry(id, d); err != nil {
		t.Fatal(err)
	}

	_, e, _ = h.findEntry(id)
	if e.OverrideURL != "cmd://open {URL}" {
		t.Error("override url lost")
	}
	if e.ForegroundColor != "#FF0000" {
		t.Error("foreground colour lost")
	}
	if e.AutoType.DefaultSequence != "{USERNAME}{TAB}{PASSWORD}" {
		t.Error("auto-type sequence lost")
	}
	if len(e.Binaries) != binaries {
		t.Errorf("attachments %d -> %d", binaries, len(e.Binaries))
	}
}

// An edit staged against a file harmos will not write is a dead end, and finding
// out at save time means finding out after typing.
func TestMutationsRefusedOnAnUnwritableSource(t *testing.T) {
	h, p := openFixture(t, vaulttest.KDBX31())
	before, err := readAll(p)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.CreateEntry("x", Draft{Title: "no"}); err == nil {
		t.Error("CreateEntry should refuse")
	}
	if err := h.UpdateEntry("x", Draft{}); err == nil {
		t.Error("UpdateEntry should refuse")
	}
	if err := h.DeleteEntry("x", true); err == nil {
		t.Error("DeleteEntry should refuse")
	}
	if _, err := h.CreateGroup("x", "no"); err == nil {
		t.Error("CreateGroup should refuse")
	}

	after, err := readAll(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a refused mutation still changed the file")
	}
}

// Duplicate UUIDs exist in the wild — bad merges produce them — and identity has
// to survive them rather than conflate two entries into one.
func TestDuplicateUUIDsGetDistinctIDs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dup.kdbx")
	vaulttest.Write(t, p, vaulttest.Shape(func(*gokeepasslib.Database) []gokeepasslib.Group {
		shared := gokeepasslib.NewUUID()
		a := gokeepasslib.NewEntry()
		a.UUID = shared
		a.Values = append(a.Values, vaulttest.Val("Title", "first"))
		b := gokeepasslib.NewEntry()
		b.UUID = shared
		b.Values = append(b.Values, vaulttest.Val("Title", "second"))

		g := gokeepasslib.NewGroup()
		g.Name = "Root"
		g.Entries = []gokeepasslib.Entry{a, b}
		return []gokeepasslib.Group{g}
	}))

	h, err := OpenHandle(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	entries := h.Snapshot().Entries
	if len(entries) != 2 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].ID == entries[1].ID {
		t.Fatalf("two entries share an id: %q", entries[0].ID)
	}
	// And each id must still resolve to the right one.
	for _, want := range entries {
		_, e, err := h.findEntry(want.ID)
		if err != nil {
			t.Fatalf("id %q does not resolve: %v", want.ID, err)
		}
		if e.GetTitle() != want.Title {
			t.Errorf("id %q resolved to %q, want %q", want.ID, e.GetTitle(), want.Title)
		}
	}
}

func TestIDsAreTypedApart(t *testing.T) {
	h, _ := openFixture(t)
	entryOne := h.Snapshot().Entries[0].ID
	folder := folderID(t, h)

	if _, _, err := h.findEntry(folder); err == nil {
		t.Error("a folder id handed to findEntry should be rejected, not silently missed")
	}
	if _, _, err := h.findGroup(entryOne); err == nil {
		t.Error("an entry id handed to findGroup should be rejected")
	}
	if _, _, _, _, err := parseID("nonsense"); err == nil {
		t.Error("a malformed id should be rejected")
	}
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

func boolWrapper(v bool) w.BoolWrapper { return w.NewBoolWrapper(v) }

func readAll(p string) ([]byte, error) { return os.ReadFile(p) }
