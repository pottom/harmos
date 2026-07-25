# internal/tui

The Bubble Tea interface. `Model` is a value type; `Update` returns a modified
copy; the view is recomputed from the last `tea.WindowSizeMsg` — never hardcode
a width or column, and measure display width with `charmbracelet/x/ansi`
(`dw`/`trunc`/`pad`), never `len()`.

## Unlock phase

A model built with `NewLocked(cfg, …)` starts **locked** (`m.locked`): the whole
UI is the unlock screen (`unlock.go`) until every source is open. `runTUI` uses
this — the master password and any per-source kdbx password not already in the
keyring are entered **in the TUI**, never on stdin.

- `NewLocked` resolves what it can (HARMOS_MASTER, keyring) and builds `ulSteps`,
  one masked prompt per still-needed credential (shared master first, then each
  uncovered kdbx source). If nothing is left, `Init` opens straight away — no
  screen flash for saved users.
- `sourceStats` computes the per-source badge: Pleasant → cache freshness
  (`fileAge` → `● cache 2h` / `● 4d — stale` / `no cache`), kdbx → `● keyring` /
  `○ own password` / `keyfile`.
- `enter` advances a step; after the last, `openCmd` runs `session.Open` off the
  update loop (Argon2 is slow) and returns `unlockDoneMsg`. `onUnlockDone` either
  moves to browsing (`intoBrowsing`) or re-queues the rejected credentials in
  place, using `session.Excluded.BadCredential` to tell a wrong password from a
  missing/corrupt file. `esc` skips an optional source.

## Tabs

Two top-level tabs, switched by `1` / `2` (`m.tab`), once unlocked:

- **Vault (0)** — the browse/search surface: a tree on the left, entry list /
  detail on the right.
- **Settings (1)** — a two-pane layout mirroring the Vault. Both reset `m.focus`
  to 0 (the left pane) on switch.

## Settings: two-pane

- **Left (`m.focus == 0`)** is a category selector (`settingsCats`:
  `catSources`, `catTheme`), rendered by `catLines`. `updateSettingsNav` moves
  `m.setCat` with ↑↓; →/tab/enter (or `t` straight to Theme) calls
  `enterCategory`, which sets `m.focus = 1` and, for Theme, snapshots the active
  theme into `themeOrig`/`themeSel` so the picker can revert on cancel.
- **Right (`m.focus == 1`)** configures the selected category:
  - `updateSourcesPane` + `sourceLines` — the sources table (add/edit/sync/
    save-pw/clear-pw/remove); ←/esc returns focus to the left.
  - `updateThemePane` + `themeLines` — the live theme picker: ↑↓ calls
    `applyThemeAt`, which `theme.Apply`s immediately (live preview); ↵ writes
    `theme = <name>` via `config.SetTopLevelKey`; ←/esc reverts to `themeOrig`.
- Modal overlays (`setForm`, `setPrompt`, `setRemove`, `setSyncing`) intercept
  before the two-pane dispatch and render as boxed overlays.
- `settingsHint` is the footer, contextual to focus and category.

## Chrome

Panels are hand-built boxes (`box`/`boxTop`) with the title in the top border and
an `N/total` (or count) marker; the active pane's border uses the accent color.
Nerd Font icons come from `ic()` with a plain-Unicode fallback
(`HARMOS_NERDFONT=0`) — type icon glyphs as `\uXXXX` escapes, never pasted, or
the editor mangles them. Colors resolve through `internal/theme`; re-theming is
live because `theme.Apply` rebuilds the styles the view reads.
