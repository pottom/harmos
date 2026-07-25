package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	gokeyring "github.com/zalando/go-keyring"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
)

func makeKDBX(t *testing.T, path, password, title string) {
	t.Helper()
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	e := gokeepasslib.NewEntry()
	e.Values = append(e.Values, gokeepasslib.ValueData{Key: "Title", Value: gokeepasslib.V{Content: title}})
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
}

// runCmd executes a tea.Cmd and feeds its message back through Update.
func runCmd(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	nm, _ := m.Update(cmd())
	return nm.(Model)
}

func TestUnlockCollectsAndOpens(t *testing.T) {
	gokeyring.MockInit()
	t.Setenv("HARMOS_MASTER", "")
	dir := t.TempDir()
	cache := filepath.Join(dir, "work.kdbx")
	own := filepath.Join(dir, "personal.kdbx")
	makeKDBX(t, cache, "master", "db-prod")
	makeKDBX(t, own, "ownpw", "router")

	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WritePleasantProfile(cfgPath, "work", "https://pps:10001", "u", cache, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteKdbxProfile(cfgPath, "personal", own, "", false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(cfgPath)

	m := up(NewLocked(cfg, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 22})
	if len(m.ulSteps) != 2 {
		t.Fatalf("want 2 credential steps (master + personal), got %d", len(m.ulSteps))
	}
	m = typeStr(m, "master")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // → personal step
	m = typeStr(m, "ownpw")

	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // last step → openCmd
	m = runCmd(nm.(Model), cmd)

	if m.locked {
		t.Fatalf("should be unlocked and browsing (err=%q)", m.ulErr)
	}
	titles := map[string]bool{}
	for _, e := range m.matcher.Match("") {
		titles[e.Entry.Title] = true
	}
	if !titles["db-prod"] || !titles["router"] {
		t.Errorf("both sources should be browsable, got %v", titles)
	}
}

func TestUnlockWrongMasterRequeues(t *testing.T) {
	gokeyring.MockInit()
	t.Setenv("HARMOS_MASTER", "")
	dir := t.TempDir()
	cache := filepath.Join(dir, "work.kdbx")
	makeKDBX(t, cache, "master", "db-prod")

	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WritePleasantProfile(cfgPath, "work", "https://pps:10001", "u", cache, "", false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(cfgPath)

	m := up(NewLocked(cfg, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 22})
	m = typeStr(m, "WRONG")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(nm.(Model), cmd)

	if !m.locked {
		t.Fatal("a wrong master must keep the screen locked")
	}
	if !strings.Contains(m.ulErr, "master") {
		t.Errorf("expected a wrong-master message, got %q", m.ulErr)
	}
	if len(m.ulSteps) == 0 || m.ulSteps[0].name != "" {
		t.Errorf("the master step should be re-queued first, got %+v", m.ulSteps)
	}

	// the right password now unlocks in place
	m = typeStr(m, "master")
	nm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(nm.(Model), cmd)
	if m.locked {
		t.Fatalf("correct master should unlock (err=%q)", m.ulErr)
	}
}

func TestUnlockSkipExcludesSource(t *testing.T) {
	gokeyring.MockInit()
	t.Setenv("HARMOS_MASTER", "")
	dir := t.TempDir()
	cache := filepath.Join(dir, "work.kdbx")
	own := filepath.Join(dir, "personal.kdbx")
	makeKDBX(t, cache, "master", "db-prod")
	makeKDBX(t, own, "ownpw", "router")

	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WritePleasantProfile(cfgPath, "work", "https://pps:10001", "u", cache, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteKdbxProfile(cfgPath, "personal", own, "", false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(cfgPath)

	m := up(NewLocked(cfg, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 22})
	m = typeStr(m, "master")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})         // → personal step
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // skip personal → open
	m = runCmd(nm.(Model), cmd)

	if m.locked {
		t.Fatalf("skipping an optional source should still unlock (err=%q)", m.ulErr)
	}
	if len(m.excluded) != 1 || m.excluded[0].Source != "personal" {
		t.Fatalf("skipped source should be recorded as excluded, got %+v", m.excluded)
	}
	if m.excluded[0].Reason != "skipped" {
		t.Errorf("reason = %q, want skipped", m.excluded[0].Reason)
	}
	if !strings.Contains(m.View(), "unavailable") {
		t.Error("the search line should flag the unavailable source")
	}
}

func TestUnlockAutoWhenAllSaved(t *testing.T) {
	gokeyring.MockInit()
	t.Setenv("HARMOS_MASTER", "")
	dir := t.TempDir()
	own := filepath.Join(dir, "personal.kdbx")
	makeKDBX(t, own, "ownpw", "router")

	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WriteKdbxProfile(cfgPath, "personal", own, "", false); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Store("personal", secret.New("ownpw")); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(cfgPath)

	m := up(NewLocked(cfg, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 22})
	if len(m.ulSteps) != 0 {
		t.Fatalf("a fully keyring-covered config needs no prompts, got %d step(s)", len(m.ulSteps))
	}
	// Init opens straight away; drive the open cmd directly.
	m = runCmd(m, m.openCmd())
	if m.locked {
		t.Fatalf("should auto-unlock to browsing (err=%q)", m.ulErr)
	}
}
