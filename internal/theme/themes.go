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
	Note:     token{"#b07d00", "#e5c07b"},
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
	Note:     token{"#9a6f22", "#e8b964"},
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
	Note:     token{"#b58900", "#ebcb8b"},
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
	Note:     token{"#b58900", "#f1fa8c"},
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
	Note:     token{"#b57614", "#fabd2f"},
	SelBg:    token{"#ebdbb2", "#3c3836"},
}

// Solarized (blue/cyan on base tones).
var Solarized = Theme{
	Name:     "solarized",
	Accent:   token{"#22729e", "#4aa6dc"},
	AccentHi: token{"#1a7a86", "#7fd6cc"},
	Steel:    token{"#073642", "#eee8d5"},
	Dim:      token{"#586e75", "#93a1a1"},
	Faint:    token{"#93a1a1", "#586e75"},
	OK:       token{"#6c7a00", "#a7b42a"},
	Warn:     token{"#c0271f", "#e5564f"},
	Note:     token{"#b58900", "#b58900"},
	SelBg:    token{"#eee8d5", "#073642"},
}

// Tokyo Night (blue/purple night sky).
var TokyoNight = Theme{
	Name:     "tokyonight",
	Accent:   token{"#2e7de9", "#7aa2f7"},
	AccentHi: token{"#9854f1", "#bb9af7"},
	Steel:    token{"#343b58", "#c0caf5"},
	Dim:      token{"#6172b0", "#7982a9"},
	Faint:    token{"#a8aecb", "#3b4261"},
	OK:       token{"#587539", "#9ece6a"},
	Warn:     token{"#c64343", "#f7768e"},
	Note:     token{"#8f5e15", "#e0af68"},
	SelBg:    token{"#d4d6e4", "#292e42"},
}

// Catppuccin (mauve/pink pastel — Latte light, Mocha dark).
var Catppuccin = Theme{
	Name:     "catppuccin",
	Accent:   token{"#8839ef", "#cba6f7"},
	AccentHi: token{"#ea76cb", "#f5c2e7"},
	Steel:    token{"#4c4f69", "#cdd6f4"},
	Dim:      token{"#6c6f85", "#9399b2"},
	Faint:    token{"#acb0be", "#45475a"},
	OK:       token{"#40a02b", "#a6e3a1"},
	Warn:     token{"#d20f39", "#f38ba8"},
	Note:     token{"#df8e1d", "#f9e2af"},
	SelBg:    token{"#e6e9ef", "#313244"},
}

// Rosé Pine (muted rose/iris/gold — Dawn light, Main dark).
var RosePine = Theme{
	Name:     "rosepine",
	Accent:   token{"#907aa9", "#c4a7e7"},
	AccentHi: token{"#c07a1e", "#f6c177"},
	Steel:    token{"#575279", "#e0def4"},
	Dim:      token{"#797593", "#908caa"},
	Faint:    token{"#cecacd", "#403d52"},
	OK:       token{"#56949f", "#9ccfd8"},
	Warn:     token{"#b4637a", "#eb6f92"},
	Note:     token{"#ea9d34", "#f6c177"},
	SelBg:    token{"#f2e9e1", "#26233a"},
}

// Everforest (comfy green forest).
var Everforest = Theme{
	Name:     "everforest",
	Accent:   token{"#8da101", "#a7c080"},
	AccentHi: token{"#3a94c5", "#7fbbb3"},
	Steel:    token{"#5c6a72", "#d3c6aa"},
	Dim:      token{"#829181", "#859289"},
	Faint:    token{"#bec5b2", "#4a555b"},
	OK:       token{"#35a77c", "#83c092"},
	Warn:     token{"#f85552", "#e67e80"},
	Note:     token{"#dfa000", "#dbbc7f"},
	SelBg:    token{"#eaedc8", "#3d484d"},
}

var builtins = map[string]Theme{
	"charm":      Charm,
	"brass":      Brass,
	"nord":       Nord,
	"dracula":    Dracula,
	"gruvbox":    Gruvbox,
	"solarized":  Solarized,
	"tokyonight": TokyoNight,
	"catppuccin": Catppuccin,
	"rosepine":   RosePine,
	"everforest": Everforest,
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
