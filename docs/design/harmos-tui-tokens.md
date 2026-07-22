# harmos TUI — design tokens

The executable half of spec §7. Every visual decision references a token; no raw
color in the render path. Two themes ship — **brass** (default, chosen in M4) and
**charm** — and once the token table exists, a theme is just a column swap.

Values are grounded in the M4 prototype (`scratch/prototype/`). Use
`lipgloss.AdaptiveColor{Light, Dark}` so the set survives light terminals, and
degrade to the 16-color and `NO_COLOR` fallbacks below.

## The rule that makes it read

- **Accent (amber/brass) is spent only on secrets and the act of copying them** —
  the revealed password, the selected row, the copy caret, matched runes. Nothing
  else is allowed to be amber.
- **Steel greys carry all chrome** — labels, structure, inactive rows.
- **Teal and rust are *status*, never accent** — teal = good/fresh, rust = needs
  attention (stale, expired, error, unsaved/pending).
- **Every color-coded state has a non-color cue too** (a `●` dot, a word like
  `stale`, a `→` caret), so meaning survives `NO_COLOR`, mono, and colorblindness.

## Token table

| Token | Role | brass · dark | brass · light | charm · dark | charm · light |
|---|---|---|---|---|---|
| `accent` | secrets · selection · copy · caret | `#dca545` | `#9a6f22` | `#8b6dff` | `#6544c9` |
| `accentHi` | revealed pw · matched runes · selected fg | `#f0d193` | `#7c5416` | `#c4b4ff` | `#4a2fae` |
| `steel` | primary text / chrome (strong) | `#c6c0b0` | `#2a271f` | `#c8c5d7` | `#232030` |
| `dim` | labels · inactive rows · secondary | `#847e6d` | `#6a6252` | `#7d7a96` | `#6b6784` |
| `faint` | rules · hints · empty bar | `#544f41` | `#948b76` | `#4f4c6c` | `#9a97b0` |
| `ok` (teal) | fresh · synced · file OK | `#5fb0a0` | `#2c7d6e` | `#43d69a` | `#1f8f66` |
| `warn` (rust) | stale · expired · error · **unsaved** | `#cf6f54` | `#a8482f` | `#ff6a94` | `#c23a63` |
| `selBg` | selected row background | `#2e2413` | `#f0e6cf` | `#241d3f` | `#e7e1fb` |

Notes:
- The terminal **background is the user's own** — harmos sets foreground and only
  the `selBg` background (the selected row). It never paints a full ground.
- `accentHi` doubles as the selected-row foreground so the whole selected row
  reads as "the amber one."

## 16-color / NO_COLOR fallback

When the terminal is ANSI-256-incapable or 16-color, map to:

| Token | ANSI 16 |
|---|---|
| `accent` | bright yellow (11) |
| `accentHi` | bright yellow (11), bold |
| `steel` | white (7) |
| `dim` | bright black (8) |
| `faint` | bright black (8) |
| `ok` | cyan (6) |
| `warn` | red (1) |
| `selBg` | reverse video on the row (no bg color) |

Under `NO_COLOR`: drop all color; keep the non-color cues (`●`, `→`, words like
`stale`/`unsaved`, the selected-row `→` prefix and reverse video). The interface
must remain fully legible with zero color.

## Signature element — the clipboard countdown

- The bar is `accent` filled (`█`) over `faint` empty (`░`), with the seconds in
  `accent`, flipping to `warn` in the last 5 seconds.
- Narrow fallback: a bare `28s` in `accent` (no bar).
- charm may render the fill as a pink→purple gradient; brass stays solid amber.
