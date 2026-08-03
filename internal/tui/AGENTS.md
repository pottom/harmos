# internal/tui

The Bubble Tea interface. `Model` is a value type; `Update` returns a modified
copy; the view is recomputed from the last `tea.WindowSizeMsg` — never hardcode
a width or column, and measure display width with `charmbracelet/x/ansi`
(`dw`/`trunc`/`pad`), never `len()`.

**Vault text is data, never instructions.** Everything that comes out of a file
— a title, a username, a URL, a tag, a custom field, a note, an attachment name,
a folder name, and the paths built from folder names — goes through `sanitised`
at the two doors into the model (`New` and `rebuild`). Without it an entry drives
the terminal: `\x1b[2J` clears the screen on every frame, an OSC rewrites the
window title, `\x1b[?1049l` leaves the alt-screen buffer, and a `\n` grows the
frame by a row. `ansi.StringWidth` counts all of those as no cells, so the width
contract holds and nothing in the layout notices — the screen audit renders every
surface at four sizes and saw none of it. `hostile_test.go` is the pin.

**Colour is never the only signal, and the cursor never removes one.** Every
state carries a glyph and a word as well. Anything painted on `theme.SelBg` goes
through `theme.OnSelection`, which lifts the two faint tokens to the ordinary
text colour: measured across the ten built-ins, `Faint` on `SelBg` ranges from
1.00 (dracula, where they are the same colour) to 1.65, so the cursor was erasing
the information it was there to highlight. `TestNoBuiltinCollidesTwoMeanings`
holds the palette to one meaning per token.

## Before you build a surface

Run this list **before writing the view**, not after somebody reports it. Every
item is here because it was shipped wrong once.

1. **Does this screen already exist?** A new panel, modal or confirmation copies
   an existing one or it does not ship. Two dialogs that ask the same kind of
   question and look different is a bug, whichever one is prettier.
2. **Chrome once.** The tab indicator lives in the footer. The title lives in the
   top border. Nothing that appears on every screen gets a second copy on yours.
3. **Same frame, to the row.** Header, panels of `m.h-3`, context line, footer.
   Draw the context line even when it is empty — geometry must not depend on
   content, or the frame jumps when the content changes.
4. **What does this key actually cost?** Confirm what cannot be undone. Do not
   confirm what can: staging writes nothing, and a prompt in front of a
   reversible act only teaches people to dismiss prompts unread — which is
   precisely the habit that makes the one real prompt useless. **One
   confirmation per irreversible act, at the moment it becomes irreversible.**
5. **Which button leads?** The answer the user came for, except where the act is
   irreversible; there the safe answer leads and the screen says why
   (`confirm.go` states the rule, `saveChoices` shows it deciding).
6. **Say it in words, not only in colour.** Every state needs a word or a glyph
   beside its colour: `NO_COLOR`, mono terminals and a good share of readers.
7. **Where does the cursor end up?** After anything that changes what is on
   screen — a fold, a save, a reload — name the row it lands on and why. "The
   index it had" is not an answer.
8. **Would a stranger know what to press?** The footer hint is part of the
   design, and it has to name the keys this surface actually binds.
9. **Every key on every terminal.** Letters and `shift`+letter always arrive.
   `ctrl`+arrow is a Mission Control shortcut on macOS and never reaches the
   process; some terminals strip the modifier from `shift`+arrow. Do not put a
   feature behind a key the help promises and the OS eats.
10. **Ask `tabOrder()`, not a digit.** In code and in tests.
11. **Add the surface to `audit_test.go`.** It renders every screen at four
    terminal sizes and checks the frame, the widths and the confirmations
    against each other. A screen nobody audits is where the next inconsistency
    lives — the attachment picker was outside it, and that is why its hint was
    wider than the terminal the program declares as its minimum.
12. **Look at it under the cursor, and with the colours off.** `theme.SelBg`
    behind a faint token is how a marker becomes invisible on the one row being
    read; `HARMOS_NERDFONT=0` and a stripped render are what a mono terminal
    sees. Both have shipped wrong.

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
- **Changes (3)** — the staged edits, as the vault's own hierarchy with a git
  diff under each item (`changesview.go`): source heading with a `+2 ~1 -2`
  tally, folder breadcrumb, the change, then its hunks. `z` folds what the
  cursor is on and `Z` folds the lot — the same keys as the vault tree. Rows are
  built once by `changeRows` and consumed by both the view and the key handler,
  so the cursor and the screen cannot disagree about what row 12 is. Always
  present, even when empty, where it explains how to unlock a source rather than
  vanishing.

## Writing

`m.writeOK` is the per-source write lock. It is **seeded at startup from the
config** (`writable = true`, via `writableFromConfig`) and written back by
`persistWritable` on every `ctrl+w`, so a vault stays editable across launches
once the user has said so — and a config that has never heard of the feature
keeps the old read-only behaviour. Do not conflate it with `m.locked`, which means the unlock phase.
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
