package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

// Attachment-save modal states.
const (
	attachNone = iota
	attachPick // choosing which attachment(s) to save
	attachDest // typing the destination directory
)

// openAttachments starts the save flow for the selected entry's attachments: a
// picker when there is more than one, otherwise straight to the destination.
func (m Model) openAttachments() Model {
	e := m.selEntry()
	if e == nil || len(e.Files) == 0 {
		return m
	}
	m.attachSel, m.attachAll = 0, false
	if len(e.Files) == 1 {
		return m.enterDest()
	}
	m.attach = attachPick
	return m
}

// enterDest moves to the destination-directory prompt, prefilled with a sensible
// default the user can edit.
func (m Model) enterDest() Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Width = 48
	ti.SetValue(defaultSaveDir())
	ti.CursorEnd()
	ti.Focus()
	m.attachInput = ti
	m.attach = attachDest
	return m
}

func (m Model) updateAttach(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.selEntry()
	if e == nil || len(e.Files) == 0 {
		m.attach = attachNone
		return m, nil
	}
	switch m.attach {
	case attachPick:
		switch key {
		case "esc", "left":
			m.attach = attachNone
		case "up", "ctrl+p":
			if m.attachSel > 0 {
				m.attachSel--
			}
		case "down", "ctrl+n":
			if m.attachSel < len(e.Files)-1 {
				m.attachSel++
			}
		case "enter", "right":
			m.attachAll = false
			m = m.enterDest()
		case "a":
			m.attachAll = true
			m = m.enterDest()
		}
		return m, nil

	case attachDest:
		switch key {
		case "esc":
			if len(e.Files) == 1 {
				m.attach = attachNone // single file: nothing to pick, so esc exits
			} else {
				m.attach = attachPick
			}
			return m, nil
		case "enter":
			return m.doSave(e), nil
		}
		var cmd tea.Cmd
		m.attachInput, cmd = m.attachInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// doSave writes the chosen attachment(s) to the entered directory and closes the
// modal with a flash message.
func (m Model) doSave(e *vault.Entry) Model {
	m.attach = attachNone
	dir := expandHome(strings.TrimSpace(m.attachInput.Value()))
	if dir == "" {
		dir = defaultSaveDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.flash = "could not use " + dir + ": " + err.Error()
		return m
	}
	files := e.Files
	if !m.attachAll {
		files = e.Files[m.attachSel : m.attachSel+1]
	}
	saved, failed := saveFiles(files, dir)
	m.flash = fmt.Sprintf("saved %s to %s", plural(saved, "file", "files"), dir)
	if failed > 0 {
		m.flash += fmt.Sprintf(" (%d failed)", failed)
	}
	return m
}

// saveFiles writes attachments to dir — base names only (no path traversal),
// never overwriting an existing file.
func saveFiles(files []vault.Attachment, dir string) (saved, failed int) {
	for _, a := range files {
		name := filepath.Base(a.Name)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = "attachment"
		}
		p := uniquePath(filepath.Join(dir, name))
		if err := os.WriteFile(p, a.Data, 0o600); err != nil {
			failed++
			continue
		}
		saved++
	}
	return saved, failed
}

// uniquePath returns p, or p with a " (n)" suffix before the extension when a
// file already exists there, so a save never clobbers an existing file.
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for n := 2; ; n++ {
		q := fmt.Sprintf("%s (%d)%s", base, n, ext)
		if _, err := os.Stat(q); os.IsNotExist(err) {
			return q
		}
	}
}

// defaultSaveDir is ~/Downloads when it exists, else the home directory, else the
// working directory.
func defaultSaveDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		dl := filepath.Join(home, "Downloads")
		if fi, err := os.Stat(dl); err == nil && fi.IsDir() {
			return dl
		}
		return home
	}
	wd, _ := os.Getwd()
	return wd
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	return p
}

// attachView renders the attachment-save modal: the picker, then the destination
// prompt.
func (m Model) attachView() string {
	e := m.selEntry()
	if e == nil {
		m.attach = attachNone
		return m.vaultBody()
	}
	if m.attach == attachDest {
		what := "all " + plural(len(e.Files), "file", "files")
		if !m.attachAll && m.attachSel < len(e.Files) {
			what = filepath.Base(e.Files[m.attachSel].Name)
		}
		lines := []string{
			"  " + theme.Faded.Render("save "+what+" to:"),
			"",
			"  " + m.attachInput.View(),
		}
		return m.modal("harmos", "save attachment", lines, "↵ save · esc back")
	}

	var lines []string
	for k, a := range e.Files {
		size := humanBytes(int64(a.Size()))
		if k == m.attachSel {
			lines = append(lines, theme.SelRow.Render(" ▸ "+a.Name+"  "+size+" "))
			continue
		}
		lines = append(lines, "   "+theme.Strong.Render(a.Name)+"  "+theme.Faded.Render(size))
	}
	return m.modal("harmos", "attachments", lines, "↑↓ pick · ↵ choose folder · a all · esc cancel")
}
