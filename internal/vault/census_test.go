package vault

import (
	"path/filepath"
	"testing"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// A CustomIconUUID of all zeros means "no custom icon" — exactly what leaving
// the element out means. KeePass writes it only when an icon is set; other
// writers put the zero UUID there, and gokeepasslib drops those on a rewrite.
//
// Nothing is lost, but the element count changes, and a census that counted them
// refused to write a real vault with 444 of them in it. The gate exists to catch
// information the library cannot carry, not punctuation.
func TestZeroCustomIconsDoNotBlockAWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icons.kdbx")
	vaulttest.Write(t, path, vaulttest.RecycleBin(), vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		e := gokeepasslib.NewEntry()
		e.Values = append(e.Values,
			vaulttest.Val("Title", "iconless"),
			vaulttest.PVal("Password", "pw"))
		e.Times = gokeepasslib.NewTimeData()
		e.CustomIconUUID = gokeepasslib.UUID{} // written as the zero UUID

		g := gokeepasslib.NewGroup()
		g.Name = "Infra"
		g.CustomIconUUID = gokeepasslib.UUID{}
		g.Entries = []gokeepasslib.Entry{e}

		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{g}
		return []gokeepasslib.Group{root}
	}))

	h, err := OpenHandle(path, "own", Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := h.VerifyWritable(); !ok {
		t.Fatalf("a vault whose only difference is an unset icon should be writable: %s", why)
	}

	// A custom icon that is actually set is a different matter: it carries
	// information, and losing it would still refuse the write.
	before, err := censusOf(h.db)
	if err != nil {
		t.Fatal(err)
	}
	if n := before["Entry>CustomIconUUID"]; n != 0 {
		t.Errorf("a zero icon reference should not be counted, counted %d", n)
	}
}
