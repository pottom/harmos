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

func openFileEntry(t *testing.T, files []vault.Attachment) Model {
	t.Helper()
	ents := []vault.Entry{{Source: "s", Path: "p", Title: "withfiles", Password: secret.New("p"), Files: files}}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 18})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "withfiles")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	return up(m, tea.KeyMsg{Type: tea.KeyRight}) // open detail
}

func typeInto(m Model, s string) Model {
	// clear the prefilled default, then type the new value
	m.attachInput.SetValue("")
	return typeStr(m, s)
}

// A single attachment: s goes straight to the destination prompt; the user picks
// the folder; save writes there (not the working directory).
func TestSaveSingleAttachment(t *testing.T) {
	dir := t.TempDir()
	m := openFileEntry(t, []vault.Attachment{{Name: "ca.pem", Data: []byte("hello")}})

	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if m.attach != attachDest {
		t.Fatalf("one attachment should jump to the destination prompt, got attach=%d", m.attach)
	}
	m = typeInto(m, dir)
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.attach != attachNone {
		t.Error("saving should close the modal")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "ca.pem")); err != nil || string(b) != "hello" {
		t.Errorf("attachment not saved to the chosen folder: %v / %q", err, b)
	}
	if !strings.Contains(m.flash, "saved") {
		t.Errorf("expected a save confirmation, got %q", m.flash)
	}
}

// Several attachments: s opens a picker; pick one, choose a folder, save just it.
func TestSavePickOneAttachment(t *testing.T) {
	dir := t.TempDir()
	m := openFileEntry(t, []vault.Attachment{
		{Name: "first.txt", Data: []byte("one")},
		{Name: "second.txt", Data: []byte("two")},
	})

	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if m.attach != attachPick {
		t.Fatalf("several attachments should open the picker, got attach=%d", m.attach)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyDown})  // select "second.txt"
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // → destination
	m = typeInto(m, dir)
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // save

	if _, err := os.Stat(filepath.Join(dir, "second.txt")); err != nil {
		t.Error("the picked attachment was not saved")
	}
	if _, err := os.Stat(filepath.Join(dir, "first.txt")); err == nil {
		t.Error("only the picked attachment should be saved")
	}
}

// 'a' in the picker saves every attachment.
func TestSaveAllAttachments(t *testing.T) {
	dir := t.TempDir()
	m := openFileEntry(t, []vault.Attachment{
		{Name: "dup.txt", Data: []byte("one")},
		{Name: "dup.txt", Data: []byte("two")}, // same name → uniquified
	})

	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) // all → destination
	m = typeInto(m, dir)
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !strings.Contains(m.flash, "saved 2 files") {
		t.Errorf("expected a two-file save confirmation, got %q", m.flash)
	}
	if _, err := os.Stat(filepath.Join(dir, "dup.txt")); err != nil {
		t.Error("first attachment not saved")
	}
	if _, err := os.Stat(filepath.Join(dir, "dup (2).txt")); err != nil {
		t.Error("duplicate-named attachment not uniquified")
	}
}
