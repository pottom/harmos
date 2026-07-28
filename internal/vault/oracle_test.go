package vault

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// The oracle: KeePassXC decides what a valid kdbx is, not our own assertions.
// A file we wrote that only harmos can read would be a bug we could not see.
//
// This covers the write engine's floor — open, save, still openable, same
// contents. The mutation PRs extend it with the recycle bin, history and
// tombstones, which need the XML export to observe.
func TestKeepassXCOpensSavedVault(t *testing.T) {
	bin, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli not installed; skipping oracle test")
	}

	p := filepath.Join(t.TempDir(), "v.kdbx")
	vaulttest.Write(t, p)

	groupsBefore, entriesBefore := dbInfo(t, bin, p)

	h, err := OpenHandle(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := h.Writable(); !ok {
		t.Fatalf("fixture should be writable: %s", why)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	groupsAfter, entriesAfter := dbInfo(t, bin, p)
	if groupsAfter != groupsBefore || entriesAfter != entriesBefore {
		t.Errorf("counts changed across a no-op save: groups %d->%d, entries %d->%d",
			groupsBefore, groupsAfter, entriesBefore, entriesAfter)
	}

	// KeePassXC treats the first root group as the database root, so its name is
	// not part of an entry path — the same flattening harmos's walk does.
	out := runKeepassXC(t, bin, "pw", "show", "-s", "-q", p, "/Infra/db-prod")
	if !strings.Contains(out, "secret-pw") {
		t.Errorf("keepassxc-cli cannot read the password back:\n%s", out)
	}
	if !strings.Contains(out, "svc") {
		t.Errorf("keepassxc-cli cannot read the username back:\n%s", out)
	}
}

// dbInfo parses the group and entry counts out of `keepassxc-cli db-info`.
func dbInfo(t *testing.T, bin, path string) (groups, entries int) {
	t.Helper()
	out := runKeepassXC(t, bin, "pw", "db-info", "-q", path)
	for line := range strings.SplitSeq(out, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		switch strings.TrimSpace(name) {
		case "Number of groups":
			groups = n
		case "Number of entries":
			entries = n
		}
	}
	if groups == 0 && entries == 0 {
		t.Fatalf("could not parse db-info output:\n%s", out)
	}
	return groups, entries
}

// runKeepassXC feeds the password on stdin, the way the CLI expects it.
func runKeepassXC(t *testing.T, bin, password string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("keepassxc-cli %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// The mutations, proven against KeePassXC rather than against our own reading of
// them. The recycle bin, the history stack and the tombstones are the parts most
// easily got subtly wrong, and `show`/`ls` cannot see the last two — only the XML
// export can.
func TestKeepassXCReadsMutations(t *testing.T) {
	bin, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli not installed; skipping oracle test")
	}

	h, p := openFixture(t, vaulttest.RecycleBin())
	folder := folderID(t, h)

	// created
	if _, err := h.CreateEntry(folder, edit.Draft{
		Title:    "created",
		Username: "someone",
		Password: secret.New("created-pw"),
	}); err != nil {
		t.Fatal(err)
	}

	// edited — leaves a history record holding the pre-edit title
	existing := h.Snapshot().Entries[0].ID
	d, err := h.EntryDraft(existing)
	if err != nil {
		t.Fatal(err)
	}
	d.Title = "edited"
	if err := h.UpdateEntry(existing, d); err != nil {
		t.Fatal(err)
	}
	_, live, _ := h.findEntry(existing)
	entryUUID := uuidText(t, live.UUID)

	// binned
	binnedID, err := h.CreateEntry(folder, edit.Draft{Title: "binned"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.DeleteEntry(binnedID, false); err != nil {
		t.Fatal(err)
	}

	// permanently deleted — leaves a tombstone and nothing else
	doomedID, err := h.CreateEntry(folder, edit.Draft{Title: "doomed"})
	if err != nil {
		t.Fatal(err)
	}
	_, doomed, _ := h.findEntry(doomedID)
	doomedUUID := uuidText(t, doomed.UUID)
	if err := h.DeleteEntry(doomedID, true); err != nil {
		t.Fatal(err)
	}

	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	// KeePassXC reads the new entry and the edited one.
	out := runKeepassXC(t, bin, "pw", "show", "-s", "-q", p, "/Infra/created")
	if !strings.Contains(out, "created-pw") {
		t.Errorf("keepassxc-cli cannot read the created entry:\n%s", out)
	}
	if out := runKeepassXC(t, bin, "pw", "ls", "-q", p, "/Infra"); !strings.Contains(out, "edited") {
		t.Errorf("the edited title is not visible:\n%s", out)
	}

	// The binned entry is in the bin, under the name KeePass uses.
	if out := runKeepassXC(t, bin, "pw", "ls", "-q", p, "/"+recycleBinName); !strings.Contains(out, "binned") {
		t.Errorf("the binned entry is not in the recycle bin:\n%s", out)
	}

	// History and tombstones are only observable through the XML export.
	xmlOut := runKeepassXC(t, bin, "pw", "export", "-f", "xml", "-q", p)

	if !strings.Contains(xmlOut, "<History>") {
		t.Error("no history element in the exported xml")
	}
	if !strings.Contains(xmlOut, "db-prod") {
		t.Error("the history record should still hold the pre-edit title")
	}
	if strings.Count(xmlOut, entryUUID) < 2 {
		t.Errorf("the history record should carry the entry's own uuid %s", entryUUID)
	}
	if !strings.Contains(xmlOut, "<DeletedObjects>") {
		t.Error("no deleted-objects element in the exported xml")
	}
	if !strings.Contains(xmlOut, doomedUUID) {
		t.Errorf("no tombstone for the permanently deleted entry %s", doomedUUID)
	}
	if strings.Contains(xmlOut, "doomed") {
		t.Error("a permanently deleted entry should leave no trace but its tombstone")
	}
}

// uuidText is the base64 form KeePass writes into the XML.
func uuidText(t *testing.T, u gokeepasslib.UUID) string {
	t.Helper()
	b, err := u.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
