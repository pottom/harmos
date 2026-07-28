package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
)

func TestResolveNerd(t *testing.T) {
	orig, had := os.LookupEnv("HARMOS_NERDFONT")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("HARMOS_NERDFONT", orig)
		} else {
			_ = os.Unsetenv("HARMOS_NERDFONT")
		}
	})
	tru, fls := true, false

	// the environment wins over the config
	_ = os.Setenv("HARMOS_NERDFONT", "0")
	if resolveNerd(&config.Config{NerdFont: &tru}) {
		t.Error("HARMOS_NERDFONT=0 must win over config=true")
	}
	_ = os.Setenv("HARMOS_NERDFONT", "1")
	if !resolveNerd(&config.Config{NerdFont: &fls}) {
		t.Error("HARMOS_NERDFONT=1 must win over config=false")
	}

	// unset env → the config decides
	_ = os.Unsetenv("HARMOS_NERDFONT")
	if resolveNerd(&config.Config{NerdFont: &fls}) {
		t.Error("config=false should apply when the env is unset")
	}
	if !resolveNerd(&config.Config{NerdFont: &tru}) {
		t.Error("config=true should apply when the env is unset")
	}
	// neither set → default on
	if !resolveNerd(nil) || !resolveNerd(&config.Config{}) {
		t.Error("the default should be on")
	}
}

func TestIconsToggle(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "") // don't let a set env pin the value
	t.Cleanup(func() { nerd = true })

	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if _, err := config.WriteKdbxSource(cfg, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	m := up(New(nil, nil, cfg, 30*time.Second), tea.WindowSizeMsg{Width: 84, Height: 16})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}}) // settings
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}) // Icons pane
	if m.setCat != catIcons || m.focus != 1 {
		t.Fatalf("i should open the Icons pane (cat=%d focus=%d)", m.setCat, m.focus)
	}
	before := nerd
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // toggle
	if nerd == before {
		t.Error("space should toggle the Nerd Font setting")
	}
	saved, err := config.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NerdFont == nil || *saved.NerdFont != nerd {
		t.Errorf("the toggle should persist to config, got %v", saved.NerdFont)
	}
}

// Every icon must exist in both sets. A field added to one and forgotten in the
// other renders as nothing — which is how the staged-change markers shipped
// empty in the Nerd set while the plain fallback looked fine.
func TestEveryIconExistsInBothSets(t *testing.T) {
	n := reflect.ValueOf(nerdIcons)
	p := reflect.ValueOf(plainIcons)

	for i := range n.NumField() {
		name := n.Type().Field(i).Name
		if n.Field(i).String() == "" {
			t.Errorf("nerd icon %s is empty", name)
		}
		if p.Field(i).String() == "" {
			t.Errorf("plain icon %s is empty", name)
		}
	}
}
