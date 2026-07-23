// Package theme is the harmos color set — the executable form of the M4 token
// table (docs/design/harmos-tui-tokens.md). Charm (violet) is the default.
// Colors are AdaptiveColor so the set survives light terminals; the accent is
// spent only on secrets and the act of copying them, teal/rust are status,
// steel is chrome.
package theme

import "github.com/charmbracelet/lipgloss"

// Tokens (charm · light/dark).
var (
	Accent   = lipgloss.AdaptiveColor{Light: "#6a48d0", Dark: "#8b6dff"}
	AccentHi = lipgloss.AdaptiveColor{Light: "#4a2ea8", Dark: "#c4b4ff"}
	Steel    = lipgloss.AdaptiveColor{Light: "#2b2740", Dark: "#c8c5d7"}
	Dim      = lipgloss.AdaptiveColor{Light: "#6b6884", Dark: "#7d7a96"}
	Faint    = lipgloss.AdaptiveColor{Light: "#a29fb8", Dark: "#4f4c6c"}
	OK       = lipgloss.AdaptiveColor{Light: "#1f9b70", Dark: "#43d69a"}
	Warn     = lipgloss.AdaptiveColor{Light: "#cc3b62", Dark: "#ff6a94"}
	SelBg    = lipgloss.AdaptiveColor{Light: "#ebe6fb", Dark: "#241d3f"}
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
