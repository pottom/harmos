package localkdbx

import (
	"path/filepath"
	"testing"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

func TestSourceOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "personal.kdbx")
	vaulttest.Write(t, path, vaulttest.WithPassword("pw"), vaulttest.WithTitle("router"))

	src := Source{Name: "personal", Path: path, Password: secret.New("pw")}
	v, err := src.Open()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Entries) != 1 || v.Entries[0].Title != "router" || v.Entries[0].Source != "personal" {
		t.Fatalf("unexpected: %+v", v.Entries)
	}
}
