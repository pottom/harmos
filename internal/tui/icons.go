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
}

// Nerd Font glyphs (private-use codepoints) — render as boxes without a Nerd
// Font, hence the HARMOS_NERDFONT=0 fallback.
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
}

var plainIcons = iconSet{
	folder:     "▸", // ▸
	folderOpen: "▾", // ▾
	entry:      "•", // •
	keyfile:    "⚷", // ⚷
	kdbx:       "▪", // ▪
	pps:        "◆", // ◆
	saved:      "✓", // ✓
	search:     "⌕", // ⌕
	none:       "—", // —
}

func ic() iconSet {
	if nerd {
		return nerdIcons
	}
	return plainIcons
}
