// Package theme is the harmos color set. A Theme is a named table of light/dark
// color tokens; Apply makes one active by rebuilding the package-level tokens and
// styles that the TUI renders through. Charm (violet) is the default; a config
// can select another built-in or a custom TOML theme. The accent is spent only
// on focus/secrets, teal/rust are status, steel is chrome.
package theme

import "github.com/charmbracelet/lipgloss"

// token is a light/dark color pair (hex). A single value can be repeated for both.
type token struct {
	Light string `toml:"light"`
	Dark  string `toml:"dark"`
}

// Theme is a named color set. Struct tags let a custom theme be decoded from TOML.
type Theme struct {
	Name     string `toml:"name"`
	Accent   token  `toml:"accent"`
	AccentHi token  `toml:"accent_hi"`
	Steel    token  `toml:"steel"`
	Dim      token  `toml:"dim"`
	Faint    token  `toml:"faint"`
	OK       token  `toml:"ok"`
	Warn     token  `toml:"warn"`
	// Note is amber: attention without alarm. Warn is red in every built-in, so
	// there was no way to say "changed" or "worth a look" without saying "wrong"
	// — which is why view.go had to hard-code an amber for the update marker.
	Note  token `toml:"note"`
	SelBg token `toml:"sel_bg"`
}

// Active tokens, set by Apply.
var (
	Accent   lipgloss.AdaptiveColor
	AccentHi lipgloss.AdaptiveColor
	Steel    lipgloss.AdaptiveColor
	Dim      lipgloss.AdaptiveColor
	Faint    lipgloss.AdaptiveColor
	OK       lipgloss.AdaptiveColor
	Warn     lipgloss.AdaptiveColor
	Note     lipgloss.AdaptiveColor
	SelBg    lipgloss.AdaptiveColor
)

// Active styles built from the tokens, set by Apply.
var (
	Brand  lipgloss.Style
	Dimmed lipgloss.Style
	Faded  lipgloss.Style
	Acc    lipgloss.Style
	Hi     lipgloss.Style
	Ok     lipgloss.Style
	Bad    lipgloss.Style
	Noted  lipgloss.Style
	Strong lipgloss.Style
	SelRow lipgloss.Style
	Rule   lipgloss.Style
)

func adaptive(t token) lipgloss.AdaptiveColor {
	dark := t.Dark
	if dark == "" {
		dark = t.Light
	}
	light := t.Light
	if light == "" {
		light = t.Dark
	}
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// Apply makes t the active theme, rebuilding every token and style.
func Apply(t Theme) {
	Accent = adaptive(t.Accent)
	AccentHi = adaptive(t.AccentHi)
	Steel = adaptive(t.Steel)
	Dim = adaptive(t.Dim)
	Faint = adaptive(t.Faint)
	OK = adaptive(t.OK)
	Warn = adaptive(t.Warn)
	Note = adaptive(t.Note)
	SelBg = adaptive(t.SelBg)

	Brand = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	Dimmed = lipgloss.NewStyle().Foreground(Dim)
	Faded = lipgloss.NewStyle().Foreground(Faint)
	Acc = lipgloss.NewStyle().Foreground(Accent)
	Hi = lipgloss.NewStyle().Foreground(AccentHi)
	Ok = lipgloss.NewStyle().Foreground(OK)
	Bad = lipgloss.NewStyle().Foreground(Warn)
	Noted = lipgloss.NewStyle().Foreground(Note)
	Strong = lipgloss.NewStyle().Foreground(Steel).Bold(true)
	SelRow = lipgloss.NewStyle().Background(SelBg).Foreground(AccentHi)
	Rule = lipgloss.NewStyle().Foreground(Faint)
}

func init() { Apply(Charm) }
