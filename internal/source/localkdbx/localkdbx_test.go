package localkdbx

import (
	"os"
	"path/filepath"
	"testing"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/secret"
)

func TestSourceOpen(t *testing.T) {
	// write a small password-protected kdbx
	path := filepath.Join(t.TempDir(), "personal.kdbx")
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Credentials = gokeepasslib.NewPasswordCredentials("pw")
	e := gokeepasslib.NewEntry()
	e.Values = append(e.Values, gokeepasslib.ValueData{Key: "Title", Value: gokeepasslib.V{Content: "router"}})
	g := gokeepasslib.NewGroup()
	g.Name = "Root"
	g.Entries = []gokeepasslib.Entry{e}
	db.Content.Root = &gokeepasslib.RootData{Groups: []gokeepasslib.Group{g}}
	if err := db.LockProtectedEntries(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := gokeepasslib.NewEncoder(f).Encode(db); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	src := Source{Name: "personal", Path: path, Password: secret.New("pw")}
	v, err := src.Open()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Entries) != 1 || v.Entries[0].Title != "router" || v.Entries[0].Source != "personal" {
		t.Fatalf("unexpected: %+v", v.Entries)
	}
}
