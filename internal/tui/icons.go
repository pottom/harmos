package tui

import "os"

// nerd reports whether to use Nerd Font glyphs. On by default; set HARMOS_NERDFONT=0
// to fall back to plain Unicode on terminals without a Nerd Font.
var nerd = os.Getenv("HARMOS_NERDFONT") != "0"

type iconSet struct {
	folder, folderOpen  string
	entry, keyfile      string
	kdbx, pps           string
	saved, search, none string
	user, link, tag     string
	clock               string
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
}

func ic() iconSet {
	if nerd {
		return nerdIcons
	}
	return plainIcons
}
