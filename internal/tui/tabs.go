package tui

// Tab order, in one place.
//
// It used to be spelled out in four: the indicator, the mouse hit-test, the
// number-key switch and the help overlay. Adding a tab meant finding all four,
// and the internal indices do not match the display order, so each spelling was
// its own chance to get it subtly wrong.

const (
	tabVault    = 0
	tabSettings = 1
	tabGenerate = 2
	tabChanges  = 3
)

// tabSpec is one tab as the user meets it: a label, the key that selects it, and
// the internal index it maps to.
type tabSpec struct {
	label string
	key   string
	idx   int
}

// tabOrder is the display order, left to right, and therefore also the order the
// number keys follow.
//
// Changes goes last, on 4, rather than taking 3 from Settings. v0.2.1 had just
// corrected the help overlay and the README to 1/2/3 for Vault/Generate/Settings;
// moving Settings now would break the muscle memory of anyone who read either.
func tabOrder() []tabSpec {
	return []tabSpec{
		{"Vault", "1", tabVault},
		{"Generate", "2", tabGenerate},
		{"Settings", "3", tabSettings},
		{"Changes", "4", tabChanges},
	}
}

// tabForKey maps a number key to its tab.
func tabForKey(key string) (int, bool) {
	for _, t := range tabOrder() {
		if t.key == key {
			return t.idx, true
		}
	}
	return 0, false
}
