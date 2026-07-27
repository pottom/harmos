package pleasant

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pottom/harmos/internal/secret"
)

// TestKeepassXCOpensCache is the oracle: an independent tool (keepassxc-cli)
// must open the cache we produce and read it back correctly. It is skipped when
// keepassxc-cli is absent (local dev) and runs in the CI oracle job. This is the
// differential check that caught the attachment-encoding bug in the PoC.
func TestKeepassXCOpensCache(t *testing.T) {
	bin, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli not installed; skipping oracle test")
	}

	res := mapFixture(t, Meta{SourceURL: "https://pps.example:10001", FetchedAt: time.Now()})
	cache := filepath.Join(t.TempDir(), "cache.kdbx")
	const master = "oracle-master"
	if err := Write(res.DB, cache, secret.New(master), ""); err != nil {
		t.Fatal(err)
	}

	// db-info: an independent parser reports the structure.
	info := runKeepassXC(t, bin, master, "db-info", "-q", cache)
	if !strings.Contains(info, "Number of groups: 3") {
		t.Errorf("keepassxc db-info groups mismatch:\n%s", info)
	}
	if !strings.Contains(info, "Number of entries: 7") {
		t.Errorf("keepassxc db-info entries mismatch:\n%s", info)
	}

	// Attachment integrity: extract via keepassxc-cli and compare bytes. Use the
	// uniquely-named multi-attach entry to avoid the duplicate-title collision.
	outFile := filepath.Join(t.TempDir(), "a.txt")
	runKeepassXC(t, bin, master, "attachment-export", "-q", cache, "/Infra/multi-attach", "a.txt", outFile)
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	want := attachmentContent("00000000-0000-0000-0000-00000000a002")
	if !bytes.Equal(got, want) {
		t.Errorf("attachment bytes via keepassxc-cli mismatch: got %q want %q", got, want)
	}
}

func runKeepassXC(t *testing.T, bin, master string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(master + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("keepassxc-cli %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
