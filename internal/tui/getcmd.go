package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/clip"
	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/vault"
)

// copyGetCommand puts a scriptable `harmos get` invocation for the selected entry
// on the clipboard — for pasting into a shell script so the secret is fetched at
// runtime (pw=$(harmos get …)) rather than embedded. A command is a reference, not
// a secret, so it is copied untracked (never auto-cleared) and it cancels any
// running secret countdown, which would otherwise wipe the command when it fired.
func (m Model) copyGetCommand() (tea.Model, tea.Cmd) {
	e := m.selEntry()
	if e == nil {
		return m, nil
	}
	cmd := m.getCommand(e)
	if err := clip.CopyUntracked([]byte(cmd)); err != nil {
		return m, nil
	}
	m.copied, m.copiedWhat, m.remaining = "", "", 0 // stop any secret countdown
	m.flash = "copied command · " + cmd
	return m, nil
}

// getCommand builds `harmos get --path '<source/folder/title>'`, adding --user
// when the full path repeats and --config when it isn't the default.
func (m Model) getCommand(e *vault.Entry) string {
	b := "harmos get --path " + shellQuote(e.FullPath())
	if m.pathRepeats(e.FullPath()) && e.Username != "" {
		b += " --user " + shellQuote(e.Username)
	}
	if arg := m.configArg(); arg != "" {
		b += " " + arg
	}
	return b + " --quiet"
}

// pathRepeats reports whether more than one entry shares the full path, so the
// command needs --user to resolve to one.
func (m Model) pathRepeats(full string) bool {
	n := 0
	for _, e := range m.allEntries() {
		if e.FullPath() == full {
			if n++; n > 1 {
				return true
			}
		}
	}
	return false
}

// configArg is "--config <path>" when the active config isn't the default, so the
// copied command resolves the same vault from any working directory.
func (m Model) configArg() string {
	if m.configPath == "" {
		return ""
	}
	if def, err := config.DefaultPath(); err == nil && def == m.configPath {
		return ""
	}
	return "--config " + shellQuote(m.configPath)
}

func (m Model) allEntries() []vault.Entry {
	var out []vault.Entry
	var walk func(n *node)
	walk = func(n *node) {
		out = append(out, n.entries...)
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, r := range m.roots {
		walk(r)
	}
	return out
}

// shellQuote single-quotes s for a POSIX shell, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
