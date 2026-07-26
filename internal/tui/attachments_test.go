package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

func TestSaveAttachments(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	ents := []vault.Entry{{
		Source: "s", Path: "p", Title: "withfiles", Password: secret.New("p"),
		Files: []vault.Attachment{
			{Name: "ca.pem", Data: []byte("hello")},
			{Name: "ca.pem", Data: []byte("dup")}, // same name → uniquified
		},
	}}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 16})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "withfiles")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = up(m, tea.KeyMsg{Type: tea.KeyRight}) // open detail

	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}) // save
	if !strings.Contains(m.flash, "saved") {
		t.Errorf("expected a save confirmation, got flash %q", m.flash)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "ca.pem")); err != nil || string(b) != "hello" {
		t.Errorf("ca.pem not written correctly: %v / %q", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "ca (2).pem")); err != nil || string(b) != "dup" {
		t.Errorf("duplicate attachment not uniquified: %v / %q", err, b)
	}

	// any key dismisses the flash
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if m.flash != "" {
		t.Error("the flash should clear on the next key")
	}
}
