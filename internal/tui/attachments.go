package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// saveAttachments writes the selected entry's attachments to the current working
// directory — base names only (no path traversal), never overwriting — and
// flashes the result on the bottom line.
func (m Model) saveAttachments() (tea.Model, tea.Cmd) {
	e := m.selEntry()
	if e == nil || len(e.Files) == 0 {
		return m, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		m.flash = "could not find the working directory"
		return m, nil
	}
	var saved, failed int
	for _, a := range e.Files {
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
	m.flash = fmt.Sprintf("saved %s to %s", plural(saved, "attachment", "attachments"), dir)
	if failed > 0 {
		m.flash += fmt.Sprintf(" (%d failed)", failed)
	}
	return m, nil
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
