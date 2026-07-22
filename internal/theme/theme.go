// Package theme is the harmos color set — the executable form of the M4 token
// table (docs/design/harmos-tui-tokens.md). Brass is the default. Colors are
// AdaptiveColor so the set survives light terminals; amber is spent only on
// secrets and the act of copying them, teal/rust are status, steel is chrome.
package theme

import "github.com/charmbracelet/lipgloss"

// Tokens (brass · light/dark).
var (
	Accent   = lipgloss.AdaptiveColor{Light: "#9a6f22", Dark: "#dca545"}
	AccentHi = lipgloss.AdaptiveColor{Light: "#7c5416", Dark: "#f0d193"}
	Steel    = lipgloss.AdaptiveColor{Light: "#2a271f", Dark: "#c6c0b0"}
	Dim      = lipgloss.AdaptiveColor{Light: "#6a6252", Dark: "#847e6d"}
	Faint    = lipgloss.AdaptiveColor{Light: "#948b76", Dark: "#544f41"}
	OK       = lipgloss.AdaptiveColor{Light: "#2c7d6e", Dark: "#5fb0a0"}
	Warn     = lipgloss.AdaptiveColor{Light: "#a8482f", Dark: "#cf6f54"}
	SelBg    = lipgloss.AdaptiveColor{Light: "#f0e6cf", Dark: "#2e2413"}
)

// Styles built from the tokens.
var (
	Brand  = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	Dimmed = lipgloss.NewStyle().Foreground(Dim)
	Faded  = lipgloss.NewStyle().Foreground(Faint)
	Acc    = lipgloss.NewStyle().Foreground(Accent)
	Hi     = lipgloss.NewStyle().Foreground(AccentHi)
	Ok     = lipgloss.NewStyle().Foreground(OK)
	Bad    = lipgloss.NewStyle().Foreground(Warn)
	Strong = lipgloss.NewStyle().Foreground(Steel).Bold(true)
	SelRow = lipgloss.NewStyle().Background(SelBg).Foreground(AccentHi)
	Rule   = lipgloss.NewStyle().Foreground(Faint)
)
