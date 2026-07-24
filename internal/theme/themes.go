package theme

import "sort"

// Charm (violet) is the default.
var Charm = Theme{
	Name:     "charm",
	Accent:   token{"#6a48d0", "#8b6dff"},
	AccentHi: token{"#4a2ea8", "#c4b4ff"},
	Steel:    token{"#2b2740", "#c8c5d7"},
	Dim:      token{"#6b6884", "#7d7a96"},
	Faint:    token{"#a29fb8", "#4f4c6c"},
	OK:       token{"#1f9b70", "#43d69a"},
	Warn:     token{"#cc3b62", "#ff6a94"},
	SelBg:    token{"#ebe6fb", "#241d3f"},
}

// Brass (amber) — harmos's original palette.
var Brass = Theme{
	Name:     "brass",
	Accent:   token{"#9a6f22", "#dca545"},
	AccentHi: token{"#7c5416", "#f0d193"},
	Steel:    token{"#2a271f", "#c6c0b0"},
	Dim:      token{"#6a6252", "#847e6d"},
	Faint:    token{"#948b76", "#544f41"},
	OK:       token{"#2c7d6e", "#5fb0a0"},
	Warn:     token{"#a8482f", "#cf6f54"},
	SelBg:    token{"#f0e6cf", "#2e2413"},
}

// Nord (arctic blue).
var Nord = Theme{
	Name:     "nord",
	Accent:   token{"#5e81ac", "#88c0d0"},
	AccentHi: token{"#4c688f", "#8fbcbb"},
	Steel:    token{"#2e3440", "#e5e9f0"},
	Dim:      token{"#4c566a", "#7b88a1"},
	Faint:    token{"#a6adbb", "#434c5e"},
	OK:       token{"#5c8a5c", "#a3be8c"},
	Warn:     token{"#bf616a", "#bf616a"},
	SelBg:    token{"#e5e9f0", "#3b4252"},
}

// Dracula (purple/pink).
var Dracula = Theme{
	Name:     "dracula",
	Accent:   token{"#7b4bc0", "#bd93f9"},
	AccentHi: token{"#a13a8f", "#ff79c6"},
	Steel:    token{"#282a36", "#f8f8f2"},
	Dim:      token{"#575c7e", "#6272a4"},
	Faint:    token{"#a3adcf", "#44475a"},
	OK:       token{"#2e9b57", "#50fa7b"},
	Warn:     token{"#c13b52", "#ff5555"},
	SelBg:    token{"#efe9ff", "#44475a"},
}

// Gruvbox (warm retro).
var Gruvbox = Theme{
	Name:     "gruvbox",
	Accent:   token{"#af3a03", "#fe8019"},
	AccentHi: token{"#9d0006", "#fabd2f"},
	Steel:    token{"#3c3836", "#ebdbb2"},
	Dim:      token{"#7c6f64", "#a89984"},
	Faint:    token{"#a89984", "#504945"},
	OK:       token{"#79740e", "#b8bb26"},
	Warn:     token{"#9d0006", "#fb4934"},
	SelBg:    token{"#ebdbb2", "#3c3836"},
}

var builtins = map[string]Theme{
	"charm":   Charm,
	"brass":   Brass,
	"nord":    Nord,
	"dracula": Dracula,
	"gruvbox": Gruvbox,
}

// Builtin returns a built-in theme by name.
func Builtin(name string) (Theme, bool) {
	t, ok := builtins[name]
	return t, ok
}

// Names lists the built-in theme names, sorted.
func Names() []string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
