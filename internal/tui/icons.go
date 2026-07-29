package tui

import (
	"os"

	"github.com/pottom/harmos/internal/config"
)

// nerd reports whether to use Nerd Font glyphs. On by default; resolved at model
// build time from the environment, then the config, then the default (see
// resolveNerd). The Settings > Icons toggle flips it live.
var nerd = true

// resolveNerd decides whether to use Nerd Font glyphs: HARMOS_NERDFONT wins if
// set (=0 off), else the config's nerdfont setting, else on. This lets a user on
// a terminal without a Nerd Font turn the glyphs off and have it stick.
func resolveNerd(cfg *config.Config) bool {
	if v, ok := os.LookupEnv("HARMOS_NERDFONT"); ok {
		return v != "0"
	}
	if cfg != nil && cfg.NerdFont != nil {
		return *cfg.NerdFont
	}
	return true
}

type iconSet struct {
	folder, folderOpen  string
	entry, keyfile      string
	kdbx, pps           string
	saved, search, none string
	user, link, tag     string
	clock, note         string
	locked, unlocked    string
	plus, pencil, trash string
	moved               string
}

// Nerd Font glyphs (private-use codepoints) — render as boxes without a Nerd
// Font, hence the HARMOS_NERDFONT=0 fallback. Written as \uXXXX escapes: pasted
// glyphs get mangled by editors.
var nerdIcons = iconSet{
	folder:     "\uf07b",
	folderOpen: "\uf07c",
	entry:      "\uf084",
	keyfile:    "\uf084",
	kdbx:       "\uf15b",
	pps:        "\uf233",
	saved:      "\uf023",
	search:     "\uf002",
	none:       "\uf068",
	user:       "\uf007",
	link:       "\uf0c1",
	tag:        "\uf02b",
	clock:      "\uf017",
	note:       "\uf15c",
	// A source's write lock. saved is also a padlock, but it means "stored in
	// the keyring": reusing it would conflate two unrelated states, and its
	// plain fallback is a tick, which would be wrong here.
	locked:   "\uf023",
	unlocked: "\uf09c",
	// Staged-change markers.
	plus:   "\uf067",
	pencil: "\uf040",
	trash:  "\uf1f8",
	moved:  "\uf061",
}

var plainIcons = iconSet{
	folder:     "▸", // triangle-right
	folderOpen: "▾", // triangle-down
	entry:      "•", // bullet
	keyfile:    "⚷", // chiron / keyfile
	kdbx:       "▪", // small square
	pps:        "◆", // diamond
	saved:      "✓", // check
	search:     "⌕", // telephone recorder / search
	none:       "—", // em dash
	user:       "@",
	link:       "↗", // up-right arrow
	tag:        "#",
	clock:      "◷", // clock / totp
	note:       "≡", // notes
	// Single-cell BMP symbols, deliberately not emoji: a double-width glyph
	// would throw off every column width computed with dw/pad.
	locked:   "⊠", // squared times — closed
	unlocked: "⊡", // squared dot — open
	plus:     "+",
	pencil:   "~",
	trash:    "-",
	moved:    "→",
}

func ic() iconSet {
	if nerd {
		return nerdIcons
	}
	return plainIcons
}
