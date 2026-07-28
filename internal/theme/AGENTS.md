# internal/theme

The color set. Everything the TUI renders through resolves here — nothing in the
render path hardcodes a hex value.

## Model

- A `token` is a `{Light, Dark}` hex pair. `adaptive` turns it into a
  `lipgloss.AdaptiveColor`; a missing side falls back to the other.
- A `Theme` is a named table of tokens with fixed roles:
  - `Accent` / `AccentHi` — spent only on focus and secrets; `AccentHi` is also
    the selected-row foreground, so it must contrast with `SelBg` on both grounds.
  - `Steel` — chrome and strong text (the light side is a near-black for light
    terminals, the dark side a near-white for dark terminals).
  - `Dim` / `Faint` — secondary text and the faintest chrome (borders, placeholders).
  - `OK` / `Warn` — status only (teal-ish / rust-ish), separate from the accent.
  - `Note` — attention without alarm (a staged change, an available update).
    `Warn` is red in every built-in, so this is what says "changed" without also
    saying "wrong".
  - `Writable` — a source unlocked for editing: its tree icons, its `rw` badge,
    and the editor's border. One token for the whole idea, so a theme has a
    single knob for it. Each built-in draws from its **own** palette rather than
    every one landing on the same amber — that is the mistake `Note` made, which
    is why it reads as a hard-coded yellow even though it is a token.
  - `SelBg` — selected-row background (a light tint on the light side, a dark
    tint on the dark side).
- `Apply(t)` rebuilds every package-level token **and** the derived
  `lipgloss.Style` values (`Brand`, `Acc`, `Hi`, `Ok`, `Bad`, `Strong`,
  `SelRow`, `Noted`, `Editable`, …). `init()` applies `Charm`.

  A token added after a custom theme was written has no value in that file.
  `Apply` falls back to a named neighbour rather than leaving it empty, because
  lipgloss treats an empty colour as "the terminal default" rather than as an
  error — the distinction would vanish silently. A reflective test asserts every
  built-in fills every token, for the same reason. Calling `Apply` again re-themes the
  whole UI live — that is how the Settings picker previews.

## Built-ins & selection

`builtins` maps name → `Theme`; `Builtin(name)` looks one up, `Names()` lists
them sorted. Ten ship: charm (default), brass, nord, dracula, gruvbox,
solarized, tokyonight, catppuccin, rosepine, everforest. A config
`theme = "<name>"` picks a built-in; `cmd/harmos` falls back to a custom
`themes/<name>.toml` (decoded onto the `Theme` struct via its `toml` tags) when
the name is not built in.

## Adding a theme

Add a `var` with a `{light, dark}` pair for every token, register it in
`builtins`, and check contrast on **both** grounds — the light side runs on a
light terminal, the dark side on a dark one. `theme_test.go` guards the wiring
(`Apply`, `Builtin`, `Names >= 5`, the adaptive fallback). Keep names lowercase,
one word.
