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
	Note token `toml:"note"`
	// Writable marks a source that is unlocked for editing, and the editor itself.
	// Its own token rather than a reuse of Note: "this row changed" and "you are
	// standing in writable territory" are different statements, and a theme
	// should be able to make them look different. Each built-in picks from its
	// own palette rather than every one landing on the same amber.
	Writable token `toml:"writable"`
	SelBg    token `toml:"sel_bg"`
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
	Writable lipgloss.AdaptiveColor
	SelBg    lipgloss.AdaptiveColor
)

// Active styles built from the tokens, set by Apply.
var (
	Brand    lipgloss.Style
	Dimmed   lipgloss.Style
	Faded    lipgloss.Style
	Acc      lipgloss.Style
	Hi       lipgloss.Style
	Ok       lipgloss.Style
	Bad      lipgloss.Style
	Noted    lipgloss.Style
	Editable lipgloss.Style
	Strong   lipgloss.Style
	SelRow   lipgloss.Style
	Rule     lipgloss.Style
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
	// A custom TOML theme written before this token existed has no value for
	// it. Falling back keeps such a theme working — an empty colour is not an
	// error to lipgloss, it simply renders as the terminal default, which would
	// silently lose the distinction rather than complain.
	Writable = adaptive(firstToken(t.Writable, t.Note, t.Accent))
	SelBg = adaptive(t.SelBg)

	Brand = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	Dimmed = lipgloss.NewStyle().Foreground(Dim)
	Faded = lipgloss.NewStyle().Foreground(Faint)
	Acc = lipgloss.NewStyle().Foreground(Accent)
	Hi = lipgloss.NewStyle().Foreground(AccentHi)
	Ok = lipgloss.NewStyle().Foreground(OK)
	Bad = lipgloss.NewStyle().Foreground(Warn)
	Noted = lipgloss.NewStyle().Foreground(Note)
	Editable = lipgloss.NewStyle().Foreground(Writable)
	Strong = lipgloss.NewStyle().Foreground(Steel).Bold(true)
	// The selection is the background. The row keeps the text colour it has
	// everywhere else, so moving the cursor onto something does not change what
	// it looks like it is — a row that turned accent under the cursor read as a
	// different kind of row among its neighbours. A row with something staged
	// against it still wears that state's colour; selRowStyle puts it back on
	// top of this.
	SelRow = lipgloss.NewStyle().Background(SelBg).Foreground(Steel).Bold(true)
	Rule = lipgloss.NewStyle().Foreground(Faint)
}

func init() { Apply(Charm) }

// firstToken is the first token that has a colour, so a theme missing a newer
// one degrades to a sensible neighbour instead of to nothing.
func firstToken(ts ...token) token {
	for _, t := range ts {
		if t.Light != "" || t.Dark != "" {
			return t
		}
	}
	return token{}
}
