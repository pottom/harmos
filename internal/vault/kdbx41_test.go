package vault

import (
	"os"
	"strings"
	"testing"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// KDBX 4.1 elements must survive a save.
//
// harmos carried a patched copy of the kdbx library for exactly this: upstream
// modelled none of the 4.1-only elements and encoding/xml discards what it does
// not model, so saving a 4.1 file quietly dropped data while leaving the 4.1
// label on it. Upstream implemented it themselves (tobischo/gokeepasslib#150,
// released in v3.7.0) and the copy is gone — this test is what stands in its
// place, and it belongs here rather than in the library because it is harmos's
// promise, not theirs.
//
// The runtime guarantee is the element census in VerifyWritable, which compares
// what a round trip returns against what the file held. This checks the same
// thing from the outside: write the elements, save, read them back.
func TestKDBX41ElementsSurviveASave(t *testing.T) {
	prevGroup := gokeepasslib.NewUUID()
	prevEntry := gokeepasslib.NewUUID()

	path := t.TempDir() + "/v41.kdbx"
	vaulttest.Write(t, path, vaulttest.MinorVersion(1), vaulttest.RecycleBin(),
		vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
			e := gokeepasslib.NewEntry()
			e.Values = append(e.Values,
				vaulttest.Val("Title", "carrier"),
				vaulttest.PVal("Password", "pw-carrier"))
			e.Times = gokeepasslib.NewTimeData()
			qc := w.NewBoolWrapper(false)
			e.QualityCheck = &qc               // 4.1
			e.PreviousParentGroup = &prevEntry // 4.1
			e.CustomData = []gokeepasslib.CustomData{{Key: "e-cd", Value: "kept"}}

			inner := gokeepasslib.NewGroup()
			inner.Name = "Inner"
			inner.Tags = "one;two"                 // 4.1
			inner.PreviousParentGroup = &prevGroup // 4.1
			inner.CustomData = []gokeepasslib.CustomData{{Key: "g-cd", Value: "kept"}}
			inner.Entries = []gokeepasslib.Entry{e}

			root := gokeepasslib.NewGroup()
			root.Name = "Root"
			root.Groups = []gokeepasslib.Group{inner}
			return []gokeepasslib.Group{root}
		}))

	h, err := OpenHandle(path, "own", Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := h.VerifyWritable(); !ok {
		t.Fatalf("a 4.1 file with 4.1 elements should be writable: %s", why)
	}

	// Change something unrelated, then write.
	var target string
	for _, e := range h.Snapshot().Entries {
		if e.Title == "carrier" {
			target = e.ID
		}
	}
	if target == "" {
		t.Fatal("fixture entry missing")
	}
	d, err := h.EntryDraft(target)
	if err != nil {
		t.Fatal(err)
	}
	d.Username = "changed"
	if err := h.UpdateEntry(target, d); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	// Read the file back and look for every 4.1 element.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials("pw")
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		t.Fatal(err)
	}
	if got := db.Header.Signature.MinorVersion; got != 1 {
		t.Errorf("the file should still be 4.1, minor version is %d", got)
	}

	var group *gokeepasslib.Group
	var entry *gokeepasslib.Entry
	var walk func(gs []gokeepasslib.Group)
	walk = func(gs []gokeepasslib.Group) {
		for i := range gs {
			if gs[i].Name == "Inner" {
				group = &gs[i]
				for j := range gs[i].Entries {
					if strings.Contains(gs[i].Entries[j].GetTitle(), "carrier") {
						entry = &gs[i].Entries[j]
					}
				}
			}
			walk(gs[i].Groups)
		}
	}
	walk(db.Content.Root.Groups)

	if group == nil || entry == nil {
		t.Fatal("the fixture did not survive the save at all")
	}
	if group.Tags != "one;two" {
		t.Errorf("Group>Tags lost: %q", group.Tags)
	}
	if group.PreviousParentGroup == nil || *group.PreviousParentGroup != prevGroup {
		t.Errorf("Group>PreviousParentGroup lost: %v", group.PreviousParentGroup)
	}
	if len(group.CustomData) != 1 || group.CustomData[0].Value != "kept" {
		t.Errorf("Group>CustomData lost: %v", group.CustomData)
	}
	if entry.QualityCheck == nil || entry.QualityCheck.Bool {
		t.Errorf("Entry>QualityCheck lost: %v", entry.QualityCheck)
	}
	if entry.PreviousParentGroup == nil || *entry.PreviousParentGroup != prevEntry {
		t.Errorf("Entry>PreviousParentGroup lost: %v", entry.PreviousParentGroup)
	}
	if len(entry.CustomData) != 1 || entry.CustomData[0].Value != "kept" {
		t.Errorf("Entry>CustomData lost: %v", entry.CustomData)
	}
	// And the edit landed.
	if got := entry.GetContent("UserName"); got != "changed" {
		t.Errorf("the edit did not land: UserName = %q", got)
	}
}

var _ = edit.Draft{}
