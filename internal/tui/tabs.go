package tui

import "strings"

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
// Ordered by how close a tab is to the work: Changes is the other half of
// editing the vault, so it sits next to it, and Settings — visited to configure
// something and then left — goes last. An earlier version put Changes last to
// keep 3 on Settings; the tabs are read left to right far more often than that
// one key is pressed from memory.
func tabOrder() []tabSpec {
	return []tabSpec{
		{"Vault", "1", tabVault},
		{"Changes", "2", tabChanges},
		{"Generate", "3", tabGenerate},
		{"Settings", "4", tabSettings},
	}
}

// tabNames is the display order as one string, for the help. Spelling it out by
// hand was the fourth copy this file exists to prevent.
func tabNames() string {
	var out []string
	for _, t := range tabOrder() {
		out = append(out, t.label)
	}
	return strings.Join(out, " · ")
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
