package vault

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
