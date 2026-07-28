# harmos write model — M6, writable local kdbx

> **Status:** design, agreed 2026-07-28. This document is the contract for M6. It is
> written to be read cold: someone (or some agent) arriving with no other context
> should be able to execute the milestone from this file plus `AGENTS.md`,
> `internal/tui/AGENTS.md` and `docs/design/harmos-tui-interaction.md`.

## Why

harmos through v0.2.1 is a **strictly read-only** client. That is not a style
preference: it is a stated contract (`AGENTS.md`), a tested invariant
(`internal/vault/vault_test.go` `TestReadOnlyInvariant`), and an assertion repeated
in fifteen places across the docs and code.

M6 makes **local kdbx sources** writable: create a folder, create an entry, edit an
entry, delete an entry or folder, move and rename — as *staged* changes with visual
feedback, reviewed and explicitly confirmed before a single byte is written.

The Pleasant side stays read-only permanently. Its cache is a derived artifact that
`harmos sync` regenerates from the server; an edit there would be silently discarded
on the next sync. This is enforced structurally (§5.4), not by convention.

The spec anticipated this milestone (`docs/harmos-spec.md` §3):

> If v2 ever adds writes, it'll be explicit per-entry pushes with confirmation,
> never an automatic merge.

### Decisions taken (do not re-litigate)

| Question | Decision |
|---|---|
| Default writability | **Read-only by default; per-source unlock that lasts one run.** Nothing is persisted to the config. If daily use argues for it, config persistence is a *later* change. |
| Delete semantics | **Recycle Bin by default (`d`), permanent with shift (`D`).** |
| Review surface | **Both**: in-place colouring in the tree and entry list, *and* a separate Changes tab with a git-style diff. |
| Extra scope | All four: password generator in the editor, custom field add/remove, KeePass history writing, move/rename. |
| Edit-mode affordance | vim-like: the active panel border turns amber and the UI says it is in edit mode. |

### Non-goals

- **No `harmos edit` CLI.** Writing is an explicit, confirmed TUI act. A headless
  write command would open exactly the surface the spec closes.
- **No KDBX 3.1 writing** (see §4.1). The gate refuses it with a reason.
- **No three-way reconcile.** If the file changed underneath us, we refuse and offer
  reload or an explicitly confirmed overwrite. Nothing merges.
- **No Pleasant writes, ever.**

---

## 1. Verified library findings

All of the following were verified against **gokeepasslib v3.6.2** at
`~/go/pkg/mod/github.com/tobischo/gokeepasslib/v3@v3.6.2/`. **Re-verify if the
dependency is upgraded** — several of these are bug-shaped and may be fixed upstream.

### 1.1 Nonce reuse — the mandatory fix

`Encoder.Encode` writes the decoded file's `MasterSeed`, `EncryptionIV` and
`KdfParameters.Salt` back **unchanged** (`header.go` `writeTo4`). The same password
with the same IV under ChaCha20 means **keystream reuse** between two consecutive
saves. KeePassXC regenerates these on every save.

Before every write, regenerate from `crypto/rand`:

| Field | Size |
|---|---|
| `Header.FileHeaders.MasterSeed` | 32 B |
| `Header.FileHeaders.EncryptionIV` | **match the current length** — 12 B ChaCha20, 16 B AES/TwoFish |
| `Header.FileHeaders.KdfParameters.Salt` | 32 B |
| `Content.InnerHeader.InnerRandomStreamKey` | 64 B |

`header.go` `updateRawData` rebuilds the KDF `VariantDictionary` from the struct
fields, so setting `Salt` on the struct is sufficient.

**Never touch the KDF cost** (`Memory`, `Iterations`, `Parallelism`, `Rounds`,
`Version`) on a user's file. `internal/source/pleasant/mapper.go` `Write` deliberately
overrides these — correct for a cache we own, forbidden for a vault we do not.

### 1.2 KDBX 4.1 silent data loss — why we refuse

`Signature.MinorVersion` is read from the file and written back verbatim
(`header.go`), but `IsKdbx4()` only inspects `MajorVersion`. The library does **not**
model 4.1-only elements: `Entry.PreviousParentGroup`, `Group.PreviousParentGroup`,
`Entry.QualityCheck`, `Group.Tags`, `Group.CustomData`. `Group.unmarshalGroupToken`
ends in `default: return nil` — unknown elements are **dropped silently**.

So saving a 4.1 file written by KeePassXC 2.7 would strip those fields while keeping
the 4.1 label. That is silent corruption of the user's primary vault.

**The gate refuses `MinorVersion >= 1` with a reason.** This is the one place in the
milestone where the correct answer is "no". Supporting 4.1 means an upstream
contribution or a vendored fork — its own milestone.

### 1.3 Other traps

| Finding | Evidence | Consequence |
|---|---|---|
| `cleanupBinaries` only walks `Root.Groups[0]` | `database.go` `getBinariesUsages` | Attachments in the 2nd..nth root group are treated as unused and **deleted**. Gate: refuse `len(Root.Groups) != 1`. |
| `Encode` expects a **locked** db and leaves it **locked**; `Open` leaves it **unlocked** | `encoder.go`, `database.go` (`LockProtectedEntries` warns "do not call this if entries are already locked"), `vault.go` | Exactly **one** `LockProtectedEntries()` before the write, one `UnlockProtectedEntries()` after. |
| `Entry.Clone()` assigns a **new UUID** | `entry.go` | Unusable for a history snapshot — a KeePass history record carries the *parent's* UUID. Hand-write the deep copy. |
| `Histories []History`, `History{Entries []Entry}` | `entry.go` | Two `History` elements marshal as two `<History>` siblings; the schema expects one. Always append into `Histories[0].Entries`, and clear `Histories` on the snapshot or history nests recursively. |
| `Meta.HistoryMaxItems` = 10, `HistoryMaxSize` = 6 MiB | `meta_data.go` | Prune per the user's file. `<= 0` means unlimited (KeePass convention). |
| `NewTimeData()` points four fields at **one shared `&now`** | `time_data.go` | Mutating one in place mutates `CreationTime`, `LastModificationTime`, `LastAccessTime` and `LocationChanged` together. Allocate a fresh `*TimeWrapper` per assignment. |
| `DeletedObjectData.setKdbxFormatVersion` does not nil-check `DeletionTime` | `deleted_object_data.go` | Leaving it unset panics. |
| `DBCredentials` stores **sha256 hashes**, not plaintext | `credentials.go` | The handle can retain credentials for re-saving **without holding a password in memory**. No re-prompt at save time. |
| `bubbles/textarea` is **already** available | `bubbles` v1.0.0 module | Multi-line Notes needs no new dependency. |
| Every new header points at the **same package-level signature variable** (`Signature: &DefaultKDBX4Sig`) | `header.go` | Writing through `db.Header.Signature` mutates the default for every database built afterwards in the process. harmos never sets it in production, but anything that does — a test fixture, a future format shim — must swap in a copy first. Found the hard way in PR1: one 4.1 fixture turned every later fixture in the test binary into a 4.1 file. |

---

## 2. Why the read model cannot be the write model

`vault.Entry` is a **projection**, and a lossy one. `walk` drops `Entry.UUID`,
`Group.UUID` and every group attribute, `Histories`, `CustomData`, `IconID`,
`AutoType`, `Times.LastAccessTime` / `LocationChanged` / `UsageCount`, and the
`Protected` flag of the standard fields.

It also **transforms**: `oneline()` maps control characters to spaces in Title,
Username, URL and custom field names/values; `customLabel()` irreversibly strips the
`pps.cuf.` / `pps.caf.` prefix; `splitTags()` splits on `;` *or* `,`.

`Vault` holds only `Source` and `Entries` — `Open` discards the `*gokeepasslib.Database`,
the file path and the credentials. And `FullPath()` is **not unique**: the repo's own
fixture contains a title collision (see `internal/source/pleasant/oracle_test.go` and
`cmd/harmos/get.go` `pathRepeats`).

> **Hard rule, to be stated in the package doc comment:** `vault.Entry` is a
> projection. It is never the source of a write.

---

## 3. Architecture

### 3.1 Packages

| Package | Responsibility |
|---|---|
| `internal/vault` (extended) | The **one** kdbx decode *and* encode: handle, ID index, rekey, atomic write, recycle bin, tombstones, history, mutation primitives. |
| `internal/edit` (new) | The staged change set: `Op`, `Set`, reducer, diff, decoration map, revert. **Pure data — no gokeepasslib, no TUI.** |
| `internal/atomicfile` (new) | temp → fsync → rename → dir fsync, 0600. |

**Why the writer lives in `internal/vault`.** The rule in `AGENTS.md` forbids a
*second reader*. A separate writer package would need its own traversal of the
gokeepasslib object graph — *that* would be the violation. Decode and encode of one
format stay together, and `vault.Open` becomes `OpenHandle(...).Snapshot()`, so there
is still exactly one decode path. `pleasant.Write` stays where it is and is never
called on a user vault.

### 3.2 The handle

```go
// internal/vault/handle.go
type Handle struct {
    db     *gokeepasslib.Database      // unlocked after decode
    path   string
    source string
    creds  *gokeepasslib.DBCredentials // sha256 hashes only — never plaintext
    fp     fingerprint                 // size, mtime, sha256 at open
    index  map[string]loc              // stable ID → (uuid, dupIdx)
    why    string                      // "" if writable, else the refusal reason
    backed bool                        // a session backup has been taken
}

func (h *Handle) String() string { return "vault.Handle[" + h.source + "]" }
```

`OpenHandle(path, source, creds)` is today's `Open` body plus the index, the
fingerprint and the writability gate. `(*Handle).Snapshot() *Vault` is today's `walk`,
unchanged. `Open(...)` becomes `OpenHandle(...).Snapshot()`.

**The writability gate always carries a reason**, and the unlock keybinding shows that
reason verbatim:

1. `!IsKdbx4()` → `"KDBX 3.1: not writable yet"`
2. `Signature.MinorVersion >= 1` → `"KDBX 4.1: harmos cannot round-trip 4.1 fields"`
3. `len(Root.Groups) != 1` → `"multiple root groups: attachments would be lost"`
4. file or directory not writable → `"file or directory is read-only"`

### 3.3 Identity, end to end

`vault.Entry` gains `ID string` and `GroupID string`. Opaque tokens; the format is an
implementation detail:

```
ID      = source + ":"   + base64url(entry.UUID[:])   // duplicates: "#2", "#3" in file order
GroupID = source + ":g:" + base64url(group.UUID[:])
```

- Plain `string`, **no type coupling** — neither `search` nor `tui` gains an import,
  and there is no `vault` ↔ `edit` cycle (`edit.Op.Target` is a `string` too).
- **No pointers are stored.** `Group.Entries` slices reallocate on any mutation.
  Resolution is always a fresh walk (`findEntry`); at ~10k entries that is
  microseconds, and it is always correct.
- Duplicate UUIDs exist in the wild (bad merges) — the index detects and
  disambiguates them with `#n`.

This also fixes three latent bugs: `tree.go` `entryKey`
(`Source\0Path\0Title\0Username`) currently conflates two same-titled entries in
`matchCounts()`; `gotoResultFolder` matches on Title+Username; and restoring the
selection after a save becomes possible at all.

### 3.4 Empty folders

`buildTree` builds **only from entries**, so an empty KeePass group is invisible today
— which means a newly created folder would not appear. `Vault` gains
`Folders []Folder{ID, Source, Path, Name, ParentID}` covering every group. This is a
shippable read-only fix on its own.

### 3.5 The edit draft

The editor form loads from a fresh, **lossless** read:

```go
func (h *Handle) EntryDraft(id string) (Draft, error)

type Draft struct {
    ID, GroupID string
    Title, Username, URL, Notes, Tags string  // RAW — no oneline()
    Password secret.Secret
    Fields []DraftField                       // RAW key: "pps.cuf.Env"
    Expires bool
    ExpiryTime time.Time
}

type DraftField struct {
    Key, Value string
    Protected  bool
}
```

**`Apply` is a field-level patch, never an object replacement.** Do not build a new
`gokeepasslib.Entry` and splice it in; mutate the existing one in place. That keeps
everything the library models but harmos does not project: `AutoType`, `CustomData`,
`CustomIconUUID`, `IconID`, `ForegroundColor`, `BackgroundColor`, `OverrideURL`,
`Binaries`, `UsageCount`, `Times.LastAccessTime`. (What the library itself does not
model — the 4.1 fields — is covered by the format gate.)

---

## 4. The save pipeline

`(*Handle).Save`, in order. Every step earns its place:

1. **Fingerprint re-check** (size + mtime + sha256). Mismatch → `ErrChangedUnderneath`,
   and **nothing is written**.
2. **Session backup, once per source**: `<path>.harmos-backup-<RFC3339>.kdbx`, mode
   0600, in the same directory. It is an encrypted kdbx, not a plaintext leak. Not
   pruned automatically — we never delete a user's backup.
3. `Rekey(db)` — §1.1.
4. `db.LockProtectedEntries()` — **exactly once** (`Open` left it unlocked, `Encode`
   wants it locked).
5. `atomicfile.Write(...)` wrapping `gokeepasslib.NewEncoder(f).Encode(db)`, then
   `f.Sync()`.
6. **Verify-decode the temp file — before the rename.** A fresh `NewDatabase` with the
   same credentials must decode it. This is the single highest-value step in the
   design: a corrupt write never replaces the user's vault. It costs one Argon2
   derivation, which is acceptable on an explicit save.
7. **Attachment census**: compare the pre-write `map[entryUUID][]name` against the
   verify-decode result. Second line of defence against the `cleanupBinaries` class
   (§1.3), independent of the root-group gate.
8. `os.Rename` + directory `Sync`.
9. `db.UnlockProtectedEntries()` (Encode left it locked) and refresh the fingerprint.

### 4.1 KDBX 3.1 is out of scope

3.1 needs a different set of regenerated header fields — `TransformSeed` 32 B,
`StreamStartBytes` 32 B, `ProtectedStreamKey` 32 B, `EncryptionIV` 16 B, and the
Salsa20 inner stream. Achievable, but a separate blast radius. The gate refuses
honestly; the user can upgrade to 4.0 in KeePassXC with one click. If it is ever
wanted: its own PR, with its own acceptance ("two saves of a 3.1 file reuse no seed,
IV or stream key, and `keepassxc-cli` opens it").

---

## 5. The change model

```go
// internal/edit
type Kind uint8 // CreateEntry, EditEntry, MoveEntry, DeleteEntry,
                // CreateGroup, RenameGroup, MoveGroup, DeleteGroup

type Op struct {
    Seq    int       // monotonic within a Set — the handle for revert
    Kind   Kind
    Source string
    Target string    // entry/group ID — assigned up front, even for a create
    Parent string    // destination group for create/move
    Name   string    // group create/rename
    Before *Draft    // nil for a create
    After  *Draft    // nil for a delete
    Perm   bool      // delete: permanent vs. recycle bin
    At     time.Time
}

type Set struct { ops []Op; seq int }
```

### 5.1 One decision makes composition trivial

**A create assigns its kdbx UUID immediately** (`gokeepasslib.NewUUID()`). An entry
that does not exist yet therefore has a stable `ID` from the first moment, every
subsequent edit/move/delete addresses that same target, and there is **no second
identity space and no ID rewriting at apply time**.

### 5.2 Composition is derived, not merged in place

Every op **stays in the log** — that is what the Changes tab renders and what
per-change revert removes. The *effective* state per target is derived:

| chain | effective |
|---|---|
| `create` → `edit`* | one `create` carrying the last `After`; no history record |
| `create` → `delete` | **nothing** — the file was never touched; the bin is not involved |
| `edit`* | one `edit`; `Before` = earliest, `After` = latest → **exactly one** history record (KeePassXC-consistent: one save, one history entry) |
| `edit`* → `delete(bin)` | the edit, then the move to the bin — what you saw is what got binned |
| `edit`* → `delete(perm)` | the delete only; the edit is skipped (no pointless history) |
| `move` → `move` | one `move` to the final parent |
| `rename group` + `move group` | both, order-independent (disjoint fields) |
| `deleteGroup` | **one op**, not exploded into child ops (that would flood the tab). Rendered as `Infra/Legacy (3 entries, 1 folder)`; apply moves or deletes the whole subtree. |

Staging a target underneath an already-deleted group is rejected with a flash message.

### 5.3 Revert

`Set.Revert(seq)` removes the op from the log and **re-derives**. No inverse-operation
machinery, so there is no wrong-undo class of bug, and reverting from the middle of a
chain is well defined. Reverting a `create` cascades to every op targeting it; the
confirmation line says so.

### 5.4 One source of truth, two surfaces

```go
func (s Set) State() map[string]State  // ID → New | Modified | Moved | Deleted
func (s Set) Diff() []Change           // git-style lines
func (s Set) Counts() map[string]int   // source → effective change count
```

`State()` has the same shape as the existing `matchCounts() map[*node]int`
(`tree.go`), which `treeLines` already consumes — the in-place decoration reuses that
wiring. Folder precedence: `Deleted > New > Modified > ContainsChanges > None`, where
`ContainsChanges` only applies when the node has no state of its own.

> **Secret rule, enforced by test:** a `secret.Secret` never enters a diff line. The
> password diff renders `Password  ••••••• → ••••••••••` (changed / unchanged / set /
> cleared), and protected custom fields the same way. `TestDiffNeverContainsSecrets`
> asserts the fixture password never appears as a substring of any `Diff()` output.

**Pleasant is unwritable structurally**: there is no code path that constructs a
`Handle` for a Pleasant source. Not a boolean flag, not a convention — the absence of
a constructor call, pinned by `TestPleasantSourceIsNeverWritable`.

---

## 6. Visual language

### 6.1 Staged states

`AGENTS.md` requires a non-colour cue beside every colour-coded state, so meaning
survives `mono`, `NO_COLOR` and colourblindness. `badgeDot` (`unlock.go`) is the
existing `kind → (glyph, style)` shape to copy.

| State | Token | Glyph | Word |
|---|---|---|---|
| New | `theme.Ok` (teal) | `+` | `new` |
| Modified | `theme.Note` (**new token**, amber) | `~` | `mod` |
| Deleted | `theme.Bad` (rust) + strikethrough | `-` | `del` |

**`theme.Note` is a new token.** The palette has **no yellow** today: `Warn`/`Bad` is
red in all ten built-ins, which is exactly why `view.go` hardcodes an amber
`updateStyle` with a comment saying so. The new token goes into all ten built-ins in
`themes.go`, and that hardcoded style retires onto it — net one hardcoded colour
removed from the render path.

Strikethrough is used **nowhere** in the repo today. Note that the selected row
renders a *plain* string through `theme.SelRow.Width(w).Render(...)`, so a nested
style will not survive: use
`theme.SelRow.Width(w).Strikethrough(true).StrikethroughSpaces(false)`. The
`StrikethroughSpaces(false)` keeps the line from running across the width padding.

### 6.2 Edit mode — vim-like

- **The border turns amber.** `borderStyle(active bool)` (`panel.go`) is binary today;
  widen it to a state — `inactive` / `active` / `editing`, where `editing` uses
  `theme.Note`. Its caller is `boxV`.
- **A mode indicator on the surface**, in the spirit of vim's `-- INSERT --`: in edit
  mode `hints()` (`view.go`) is replaced by `-- EDIT --` in `theme.Note` plus the
  context keys. The panel's `info` slot carries the name of the entry being edited.
- `boxTop` passes a title containing `\x1b` through un-restyled, so the coloured mode
  marker can be injected into the border itself.

### 6.3 The padlock

New icon pairs in `icons.go`, following the four-step convention there (struct field,
Nerd entry as a `\uXXXX` **escape — never a pasted glyph**, plain fallback, optionally
the Icons preview): `locked` / `unlocked`, plus `plus` / `pencil` / `trash` for the
change markers. **No emoji** — double-width glyphs break the `dw`/`pad` column
arithmetic. The padlock always carries the two-letter word `ro` / `rw` beside it.

Note that the existing `saved` icon *is* a padlock in the Nerd set, but its plain
fallback is a checkmark — do not reuse it.

### 6.4 Tabs

**`1`/`2`/`3` stay Vault/Generate/Settings; Changes is `4`.** v0.2.1 has just
corrected the help overlay and README to that mapping; moving Settings from `3` to `4`
would be a muscle-memory regression. The Changes tab is **always present**; when empty
it reads *"nothing pending — no source is unlocked for writing (ctrl+w)"*.

Tab order must come from **one source** (`m.tabOrder() []tabSpec`). It is currently
hardcoded in four places: `tabIndicator` (`view.go`), the tab hit-test (`mouse.go`),
the number-key switch (`tui.go`) and `keyList` (`help.go`).

---

## 7. PR sequence

Each PR builds, runs and is reviewable on its own (`AGENTS.md`). They land on `main`
one at a time, on green CI plus human approval.

| PR | Branch | Acceptance |
|---|---|---|
| **0** | `docs/m6-write-model` | This document exists; the workflow and spec name M6 and its limits. Zero code change. |
| **1** | `feat/vault-write-engine` | A kdbx opened and saved with **no changes** differs in bytes (fresh nonces), decodes field-for-field identically, and `keepassxc-cli db-info` reports the same group/entry counts. |
| **2** | `feat/vault-mutations` | create/edit/move/delete round-trip on a temp kdbx; `keepassxc-cli show` reads the new entry, the bin group holds the binned one, and a permanently deleted UUID appears in `DeletedObjects`. |
| **3** | `feat/edit-change-model` | A table-driven test walks every composition pair and asserts the derived diff and decoration map; reverting any single op leaves a consistent set. **← stop-and-wait gate** |
| **4** | `feat/session-handles` | Over one Pleasant and one kdbx source, `session.Open` yields **exactly one** handle — the kdbx one. |
| **5** | `refactor/tui-form-and-tokens` | Every built-in theme fills every token (reflective test); the Settings source form behaves identically; the new glyphs render under `HARMOS_NERDFONT=0`. |
| **6** | `feat/tui-write-lock` | No source is writable at launch; `ctrl+w` + `y` unlocks exactly one and flips the padlock; a Pleasant source refuses with a reason; the teatest contract stays green. |
| **7** | `feat/tui-edit` | A staged edit colours the row and increments the dirty count **while the file on disk is byte-identical**. |
| **8** | `feat/tui-changes-and-save` | edit → `ctrl+s` → `y` writes the file; `keepassxc-cli` opens it and shows the edited value; the tab empties and the tree decoration clears. |
| **9** | `test/write-oracle` | The CI oracle job proves `keepassxc-cli` reads back the recycle bin, the history record **and** a tombstone from a harmos-written file. |

### PR1 notes

**As shipped**, PR1 deferred the ID index to PR2: nothing in "open and save
unchanged" needs to address an individual entry, so the index arrives with the
mutations that do. PR1 also brought its own oracle test forward from PR9, because
its acceptance line names `keepassxc-cli`; PR9 extends that test with the recycle
bin, history and tombstones.

New: `internal/atomicfile/atomicfile.go` — and migrate both existing copies onto it
(`internal/source/pleasant/sync.go` `writeAtomic`, `internal/config/edit.go`
`writeFileAtomic`). One implementation, not three. Also `internal/vault/handle.go`,
`rekey.go`, `write.go`, and `internal/vault/vaulttest/` to absorb the fixture builder
that is currently duplicated three times (`vault_test.go`, `session_test.go`,
`localkdbx_test.go`).

### PR2 notes

`mutate.go`, `times.go` (`nowPtr()` — a fresh allocation per assignment, §1.3),
`recyclebin.go`, `history.go`.

**Recycle bin.** If `Meta.RecycleBinEnabled == false`, `d` is *also* permanent and the
confirm overlay says so out loud (KeePassXC-consistent). A missing bin is created as a
direct child of the root group with `IconID = 43`, setting `Meta.RecycleBinUUID` and
`RecycleBinChanged`. Permanent delete appends to `Root.DeletedObjects` with a
non-nil `DeletionTime`; deleting a group tombstones **every descendant**.

**History.** Hand-written deep copy (**not `Clone()`** — it would assign a new UUID) →
clear `Histories` on the copy → append into `Histories[0].Entries` → prune per
`HistoryMaxItems` / `HistoryMaxSize`, oldest first → only then mutate the live entry
and set `LastModificationTime`. No history for creates or deletes.

### PR7 notes

**The edit modal must dispatch above `q`.** Check `m.edit` immediately after the
`m.attach` guard in `Update` — that is above the `?` help toggle, above `m.help`, and
critically above the `q`-quits branch. Without that, typing "q" into a Title field
quits the application. `TestQuitAndSearchQ` and `TestLettersAreFreeUntilSlash` pin the
current behaviour.

Keys (only on a source unlocked for writing, never in search mode), checked against
the existing bindings in results (`c`, `g`), tree (`/`, `c`) and detail (`s`, `c`,
`ctrl+r/u/o/t`):

| Key | Action |
|---|---|
| `e` | edit the selected entry |
| `n` / `N` | new entry / new folder |
| `d` / `D` | delete → recycle bin / **permanent** |
| `m` | move |
| `r` | rename folder |
| `ctrl+g` | roll a password in the editor (uses the Generate tab's saved options) |
| `ctrl+w` | toggle the source lock |
| `ctrl+s` | save |

`e` and `ctrl+s` are already specified in `docs/design/harmos-tui-interaction.md` as
the v2 flow — extend that keymap, do not invent a new one.

Custom fields edit the **raw** key (`pps.cuf.Env`), not the `customLabel`-trimmed one,
and the field hint says so.

### PR8 notes

The save confirmation shows three things before `y`: the full destination path, the
per-source change counts, and **the backup path it is about to create**.

`saveCmd` runs **outside the update loop** as a `tea.Cmd` — the Argon2 re-derivation
takes ~0.5–1 s, the same reason `openCmd` exists.

On `ErrChangedUnderneath` the save is refused and the staged set is left **intact**;
the overlay offers "quit without saving" or "overwrite — their changes will be lost",
the latter behind a second confirmation.

**Quit guard**: when dirty, both `q` *and* `ctrl+c` raise an overlay. `ctrl+c`
currently quits unconditionally, which becomes data loss once editing exists.

---

## 8. Verification

```sh
go test ./...        # unit, mapper, oracle, and the teatest interaction contract
go vet ./...
golangci-lint run    # config .golangci.yml
make build           # then exercise it by hand on a throwaway kdbx
```

### The read-only invariant is kept, not deleted

`TestReadOnlyInvariant` stays, narrowed to its real meaning and joined by three
siblings:

| Test | Asserts |
|---|---|
| `TestBrowseSessionLeavesFileUnchanged` | today's test, plus the session **stages** an edit and a delete and does **not** save → bytes and mtime unchanged, no `.lock` file |
| `TestLockedSourceCannotBeSaved` | with the source locked, the save path refuses and the file is untouched |
| `TestPleasantSourceIsNeverWritable` | no code path constructs a `Handle` for a Pleasant source |
| `TestSaveIsTheOnlyWriter` | mtime changes **only** after an explicit `Save` |

Critical new pins: `TestSaveRegeneratesNonces` (two saves must never share an IV — the
ChaCha20 keystream-reuse pin), `TestRefusesKdbx41`, `TestRefusesMultipleRootGroups`,
`TestVerifyDecodeRejectsCorruptTemp`, `TestKdfCostPreserved`, `TestEditModalSwallowsQ`,
`TestQuitGuardOnDirty`, `TestStagedEditDoesNotTouchDisk`,
`TestDiffNeverContainsSecrets`, and a reflective theme-token completeness test (today
`theme_test.go` enumerates no tokens, so a theme that forgets one yields a silent
empty colour).

### Oracle

`internal/vault/oracle_test.go`, modelled on `internal/source/pleasant/oracle_test.go`:
`db-info` for the counts, `show` for the edited value, `ls "/Recycle Bin"` for the
binned entry, and **`export -f xml`** parsed independently for the `<History>` block
(one `<History>` element holding one `<Entry>` with the same `<UUID>`) and the
`<DeletedObjects>` tombstone. The XML export is the only oracle path to history and
tombstones — `show` and `ls` cannot see them.

CI currently runs `go test -run TestKeepassXCOpensCache ./internal/source/pleasant/`;
widen it to `-run TestKeepassXC ./internal/source/pleasant/ ./internal/vault/`.

**Fixture consolidation**: the shared fixture gains an entry with several attachments
at a non-first root-level depth, and a duplicate-UUID pair, so the two ugliest classes
are covered.

---

## 9. Risks

| # | Risk | Mitigation |
|---|---|---|
| 1 | KDBX 4.1 silent data loss | Refuse `MinorVersion >= 1` with a reason (§1.2) |
| 2 | ChaCha20 keystream reuse | `Rekey` before every write; an IV-collision test pins it (§1.1) |
| 3 | `cleanupBinaries` attachment loss | Root-group gate **and** an attachment census before the rename |
| 4 | A corrupt write replaces the vault | Verify-decode before rename; session backup; fsync; atomic rename |
| 5 | Lock/unlock discipline | One rule in the doc comment: *`Encode` expects a locked db and leaves it locked; `Open` leaves it unlocked* |
| 6 | Overwriting an external edit | Fingerprint at open, re-checked before rename; force needs a second confirmation |
| 7 | `q` quits mid-edit | The modal dispatches above `q`; dedicated test |
| 8 | History corruption | Hand-written deep copy keeping the UUID; single `<History>`; pruning; oracle XML check |
| 9 | Shared `*TimeWrapper` | `nowPtr()` allocates per assignment |
| 10 | A secret in the diff | `Secret` never enters a diff line; substring test |
| 11 | A forgotten theme token | Reflective completeness test |
| 12 | Writing a Pleasant source | Structural: no handle-constructing code path |
| 13 | Duplicate UUIDs in the wild | `#n` disambiguation, covered by the fixture |
| 14 | Doc drift | PR0 does the spec and workflow; each later PR fixes the assertions it invalidates (list below) |

### Read-only assertions to amend

`AGENTS.md` · `internal/tui/AGENTS.md` · `SECURITY.md` · `README.md` ·
`docs/harmos-spec.md` §1/§2/§3/config sample · `docs/harmos-workflow.md` M3 row ·
`internal/vault/vault.go` (package doc, `Vault`, `Open`, the `O_RDONLY` comment) ·
`internal/source/localkdbx/localkdbx.go` · `internal/config/config.go` (`Kdbx` doc) ·
`internal/session/session.go` (`Result` doc) · `cmd/harmos/main.go` ·
`cmd/harmos/root.go` · `cmd/harmos/source.go` · `cmd/harmos/remove.go` ·
`.goreleaser.yaml`

Each PR amends the assertions its own change invalidates. Nothing is left claiming
read-only once writing exists.
