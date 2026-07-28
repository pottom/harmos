package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// RunOnboarding launches the interface with no config yet. Instead of erroring on
// the command line, it lands the user on the Settings → Sources pane — empty, with
// the "press 'a' to add one" guidance — so the first thing (and, until a source
// exists, the only useful thing) is creating a source. It is written to
// configPath; on the next launch the vault opens normally.
func RunOnboarding(configPath string, timeout time.Duration) error {
	m := New(nil, nil, configPath, timeout)
	m.tab, m.setCat, m.focus = 1, catSources, 1 // Settings → Sources, content pane
	m.onboarding = true
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
