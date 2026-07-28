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

Four top-level tabs, switched by `1` / `2` / `3` / `4` once unlocked. **The order
lives in one place — `tabOrder()` in `tabs.go`** — because it used to be spelled
out in the indicator, the mouse hit-test, the number-key switch and the help
overlay, and the internal indices do not match the display order. Displayed order
is **Vault / Changes / Generate / Settings** — ordered by how close a tab is to
the work, so Changes sits beside the vault it edits and Settings, visited and
left, goes last. The internal `m.tab` values are unrelated to it: Vault `0`,
Settings `1`, Generate `2`, Changes `3`. **Tests must ask `tabOrder()` for a
tab's key rather than spelling the digit**, or the next reordering retargets them
silently — and a test that still passes against the wrong tab is worse than one
that fails.

Every tab draws the same frame: a header line, panels of `m.h-3`, a context line,
then the footer. The context line is drawn whether or not it has anything to say,
so the geometry never depends on the content, and **the tab indicator belongs to
the footer alone** — `TestTabFrameIsUniform` pins both.

- **Vault (0)** — the browse/search surface: a folder tree on the left (collapsible
  with `ctrl+b` to a thin rail), entry list / detail on the right. The header
  carries the brand, the stamped version, and a yellow `⬆` marker when the
  background check finds a newer release.
- **Generate (2)** — a `crypto/rand` password generator (`generate.go`): a
  live-updating options column and a centered password hero with a strength bar,
  class breakdown, and recent-roll history. Shares `internal/pwgen` with
  `harmos gen`; saved options persist to the config.
- **Settings (1)** — a two-pane layout mirroring the Vault; resets `m.focus` to 0
  (the left pane) on switch.
- **Changes (3)** — the staged edits. Always present, even when empty, where it
  explains how to unlock a source rather than vanishing.

## Writing

`m.writeOK` is the per-source write lock, and it starts **empty on every run**:
nothing is persisted, so a vault is editable only because the user unlocked it
this session. Do not conflate it with `m.locked`, which means the unlock phase.
`m.handles` holds the sources the session could open for writing — a Pleasant
cache never gets one — and `m.chg` is the staged change set. `ctrl+w` toggles the
lock, asking before unlocking and never before locking.

## Settings: two-pane

- **Left (`m.focus == 0`)** is a category selector (`settingsCats`: Sources,
  Theme, Icons, Preferences — `catSources`/`catTheme`/`catIcons`/`catPrefs`),
  rendered by `catLines`. `updateSettingsNav` moves `m.setCat` with ↑↓;
  →/tab/enter (or `t` straight to Theme) calls `enterCategory`, which sets
  `m.focus = 1` and, for Theme, snapshots the active theme into
  `themeOrig`/`themeSel` so the picker can revert on cancel.
- **Right (`m.focus == 1`)** configures the selected category:
  - `updateSourcesPane` + `sourceLines` — the sources table (add/edit/sync/
    save-pw/clear-pw/remove); ←/esc returns focus to the left.
  - `updateThemePane` + `themeLines` — the live theme picker: ↑↓ calls
    `applyThemeAt`, which `theme.Apply`s immediately (live preview); ↵ writes
    `theme = <name>` via `config.SetTopLevelKey`; ←/esc reverts to `themeOrig`.
  - the Icons category toggles the Nerd Font fallback (`nerdfont`).
  - `updatePrefsPane` + `prefsLines` — editable, persisted preferences: the
    clipboard timeout and the cache-stale threshold (`config.SetPreferences`).
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
