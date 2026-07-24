package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
)

// applyThemeAt previews the theme at idx by making it active immediately.
func (m Model) applyThemeAt(idx int) Model {
	names := theme.Names()
	if idx < 0 || idx >= len(names) {
		return m
	}
	m.themeSel = idx
	if t, ok := theme.Builtin(names[idx]); ok {
		theme.Apply(t)
		m.themeName = names[idx]
	}
	return m
}

// updateThemePane is the right pane for the Theme category: ↑↓ previews live,
// enter saves to the config, left/esc reverts to the theme in effect on entry.
func (m Model) updateThemePane(key string) (tea.Model, tea.Cmd) {
	names := theme.Names()
	switch key {
	case "left", "esc":
		if t, ok := theme.Builtin(m.themeOrig); ok {
			theme.Apply(t)
		}
		m.themeName = m.themeOrig
		m.focus = 0
	case "up", "ctrl+p":
		if m.themeSel > 0 {
			m = m.applyThemeAt(m.themeSel - 1)
		}
	case "down", "ctrl+n":
		if m.themeSel < len(names)-1 {
			m = m.applyThemeAt(m.themeSel + 1)
		}
	case "enter":
		if err := config.SetTopLevelKey(m.configPath, "theme", m.themeName); err != nil {
			m.setStatus = "could not save theme: " + err.Error()
		} else {
			m.setStatus = "theme saved: " + m.themeName
			m.themeOrig = m.themeName
		}
	}
	return m, nil
}

func (m Model) themeLines(w int) []string {
	var out []string
	for i, n := range theme.Names() {
		if i == m.themeSel {
			cur := "  "
			if n == m.themeOrig {
				cur = theme.Ok.Render("● ")
			}
			if m.focus == 1 {
				out = append(out, theme.SelRow.Width(w).Render(trunc("▸ "+n, w)))
				continue
			}
			out = append(out, cur+theme.Hi.Render(n))
			continue
		}
		mark := "  "
		if n == m.themeOrig {
			mark = theme.Ok.Render("● ")
		}
		out = append(out, mark+theme.Strong.Render(n))
	}
	return out
}
