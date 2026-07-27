# harmos — spec

A terminal password client for **Pleasant Password Server** and local `.kdbx` files.

> **Status:** corrected against the live server on 2026-07-21. This document supersedes Section 3 of `docs/harmos-brief.md`; the brief remains as the original handoff. Every correction here is backed by `docs/FINDINGS.md` (the M1 PoC output). Sections marked **[corrected]** changed after contact with the real server; the rest survived unchanged.
>
> Read this whole document before writing code. The PoC (§13) already ran — its findings are folded in.

---

## 1. What this is

A CLI/TUI tool for browsing and copying my passwords. Two sources:

1. **Pleasant Password Server** (a.k.a. KeePass Hub, by Pleasant Solutions) — a commercial .NET password server with a REST API. My instance is **9.1.11.0** (the brief guessed 9.3.3; the real version is lower — see FINDINGS).
2. **Local `.kdbx` files** — plain KeePass 2 databases on disk.

**v1 is strictly read-only.** Browse, search, copy. No writing back to the server, no entry editing, no bidirectional sync. This is a deliberate scope decision — see §3.

### Ship it in two stages, and stop after the first

**v0.1 is `harmos sync` and nothing else.** No TUI. No bubbletea in `go.mod`. It authenticates, pulls the offline package, writes the kdbx, and exits. That's it — and it's already useful, because KeePassXC and MacPass both open the result (proven in the PoC).

Then stop. I'm going to use it that way for a few weeks before the TUI gets written.

The reason is not process hygiene, it's that this project has an obvious failure mode. After the PoC there's a working dump→kdbx pipeline, which is roughly 90% of the *value* and 20% of the *work*; the TUI is the remaining 10% of value and 80% of the work. That's exactly the shape of project that gets abandoned at 95%. Shipping the boring half first means the thing is useful even if the TUI never happens — and if it does happen, it'll be built against weeks of real usage.

So: when v0.1 works, **don't start the TUI.** Tell me it's done and wait.

## 2. The one architectural insight

Pleasant Password Server is **not** kdbx-backed. It has its own database; "KeePass" in the product name means it's compatible with the KeePass desktop client via a plugin. There is no `.kdbx` file on the server to download.

So: **the Pleasant backend's only job is to produce a kdbx file.** Everything downstream — the TUI, search, clipboard, detail view — reads a kdbx and knows nothing about Pleasant.

```
Pleasant API ──► mapper ──► local .kdbx cache ─┐
                                               ├──► vault reader ──► TUI / CLI
local .kdbx file ──────────────────────────────┘
```

Two producers, one reader. Do **not** build a general "vault abstraction" interface with a Pleasant implementation and a kdbx implementation — that's the obvious design and it's the wrong one here. It doubles the surface area for zero benefit, because the kdbx format is already a superset of what Pleasant exposes. (The PoC confirmed this: the whole real vault mapped into a kdbx with no information loss.)

The local cache is a real kdbx, encrypted with a master password I choose at `init`, openable in KeePassXC. That gives us at-rest encryption, attachments, and interop for free.

## 2a. Multiple sources — N Pleasant servers and N local files

I will have several sources at once: more than one Pleasant server, plus several local `.kdbx` files. The data model must assume N sources from the start.

**One cache per source. Never merge them into one kdbx file.** Sources have independent lifecycles, credentials, and trust. Merging destroys provenance, and provenance is a safety property here.

**Tree shape: profiles are the root level.** The top level of the tree *is* the list of sources; expanding one descends into its folders. Provenance becomes structural: the path always tells me which source I'm in.

**One unlock, all sources.**

- **`harmos` owns its Pleasant caches, so it picks their key.** All Pleasant caches are encrypted with a *single* harmos master password, chosen at `init`. One prompt opens all of them. They remain individually valid kdbx files.
- **External kdbx sources keep their own passwords.** Offer at `profile add` to store the password in the OS keyring (opt-in, per source). If stored, it opens with the same single unlock. If not, that source is lazily unlocked and search reports it as excluded.
- **Never build an unencrypted search index.** Entry titles, usernames, URLs, and folder paths are themselves sensitive.

**Every entry carries a visible source badge — always,** including in search results and the detail view. This is the one piece of chrome that never gets dropped at narrow widths (§8a). Rationale: a production and a staging server will both contain an entry called `admin`, and copying the wrong one is the worst thing this program could do to me.

**Namespace by profile.** Profile names are unique (enforced at config load) and qualify everything: `work/Infra/db-prod`, `client-a/Infra/db-prod`.

**Partial failure is the normal case.**

- `harmos sync` with no argument syncs all sources concurrently (bounded — `errgroup` with a limit, per-source timeout), and **succeeds partially**. Report per-source outcomes. Exit non-zero only if every source failed.
- A dead source must never prevent the TUI from starting or block browsing the others.
- `status` reports per-source: last sync, cache age, package expiry, last error.

**The two source types are asymmetric — don't paper over it.** A `type = "kdbx"` source has no cache, no sync, no meaningful "cache age": the file *is* the source. `harmos sync` on one is a no-op, not an error. A `pleasant` source has no meaningful `path`.

> **Safety requirement:** for a local kdbx source, open the file **read-only**. Never write to it, never create a `.lock` file beside it, never rewrite it. State it in code as an invariant and test it (assert mtime and bytes unchanged after a full browse session).

## 3. Why read-only (do not talk me out of this)

Writing back would require bidirectional reconcile, and Pleasant's API makes it nasty:

- **No delta endpoint.** The offline package is a full dump every time (confirmed: a full multi-tens-of-MB dump, no incremental option).
- **No tombstones.** A deleted entry just vanishes from the dump.
- **Permissions look identical to deletion.** Pleasant has per-folder access levels (`HasViewEntryContentsAccess`). **The PoC confirmed this is real** — some folders in my vault return `HasViewEntryContentsAccess=false`. If my access to a folder is revoked, those entries vanish from the dump exactly as a deletion would. You cannot distinguish the two.
- **No visible optimistic locking.** Probably last-write-wins.

Read-only sidesteps all of it. If v2 ever adds writes, it'll be explicit per-entry pushes with confirmation, never an automatic merge.

## 4. Stack

- Go (current stable). Module path: **`github.com/pottom/harmos`**.
- **kdbx:** `github.com/tobischo/gokeepasslib/v3` — supports KDBX4 (Argon2, ChaCha20). **License verified: MIT** (v3.6.2). Use **`db.AddBinary`** for attachments — the deprecated `InnerHeader.Binaries.Add` stores them wrong (§6).
- **TUI:** `charmbracelet/bubbletea`, `charmbracelet/bubbles`, `charmbracelet/lipgloss`.
- **CLI:** `spf13/cobra`.
- **Config:** TOML. `BurntSushi/toml`.
- **Clipboard:** hand-rolled per platform, not a library — see §9 and §4a.
- **Token storage:** `zalando/go-keyring` (OS keychain). Never the config file.
- **HTTP:** stdlib `net/http`. No client framework.

## 4a. Why Go — decided, don't re-litigate

**This project's core operation is *writing* a kdbx** (§2). Only Go has a mature, permissively-licensed kdbx writer:

| | writes KDBX4 | licence |
|---|---|---|
| `tobischo/gokeepasslib/v3` (Go) | stable | **MIT (verified)** |
| `keepass` / sseemayer (Rust) | **experimental** | — |
| `kdbx-rs` / tonyfinn (Rust) | yes | **GPL-3.0+** (would force the project to GPL) |

Two costs Go genuinely imposes, both accepted knowingly:

1. **Clipboard concealment needs cgo on macOS** (§9), losing `CGO_ENABLED=0` cross-compilation for darwin. Contained to one build-tagged file.
2. **Memory zeroing is best-effort only.** Go's GC moves objects and strings are immutable, so secrets cannot be reliably wiped. We accept it and say so honestly in the README.

## 5. The Pleasant API **[corrected — this is where the PoC changed the most]**

Base URL `https://host:10001`.

**TLS.** The server certificate's Subject Alternative Names may be **internal names only** (e.g. `pps.internal.example`, `host01.internal.example`), with the public/CN hostname *not* in the SAN — modern TLS validation then rejects the public name. **Target a SAN hostname**, or use connection-level host mapping while verifying against the SAN name. The internal issuing CA is trusted on my client; **do not add `--insecure`** as the ergonomic path (§9). Support pointing at a CA bundle for the internal-CA case.

### Auth

`POST /OAuth2/Token`, `Content-Type: application/x-www-form-urlencoded`:

```
grant_type=password&username=<user>&password=<pass>
```

Response JSON contains `access_token`. **Verified against the live server:**

- `grant_type=password` works; **no 2FA** on my account.
- `token_type: bearer`, **`expires_in: 3900`** (65 min), **no refresh token** returned.
- **Both** `Authorization: <token>` (bare) **and** `Authorization: Bearer <token>` are accepted. Use `Bearer` (conventional).

### API version

**v5, v6, and v7 all respond `200`** on my 9.1.11.0 server. **Use v6:** it is the version whose documented shape we corrected against, it is newer than v5, and it avoids the fully-undocumented v7. Moving to v7 later is a URL change with no mapper impact if its shape proves identical — a separate decision.

### The endpoint that makes this project easy — `OfflinePackage`

`POST /api/v6/rest/OfflinePackage`, body `{"Comment": "..."}`

**This returns a ZIP archive, not a JSON body.** (The brief's §5 assumed a single JSON with base64-inlined attachments. It is wrong.) The ZIP contains:

- **exactly one `<guid>.json` manifest** — the folder/entry tree, top-level keys `Root`, `Attachments`, `Expiry`;
- **one binary file per attachment**, each named by its **`AttachmentId`** (the join key — *not* `CredentialObjectId`).

`Attachments[].FileData` is **always `null`** in the manifest — bytes live in the zip entries. `FileSize` matches the zip entry size.

Rough scale from my vault (exact figures live in the gitignored FINDINGS): **tens of MB, over a minute per pull, on the order of ~1,300 folders and ~10,000 entries, several hundred attachments, max depth 8.** See §10 for what this means for sync UX.

Manifest shape (corrected, abridged — invented values):

```jsonc
{
  "Root": {
    "Id": "<guid>", "Name": "Root", "ParentId": "00000000-...",
    "Notes": null, "Created": "...", "Modified": "...", "Expires": null,
    "Tags": [], "CustomUserFields": {}, "CustomApplicationFields": {},
    "HasModifyEntriesAccess": true, "HasViewEntryContentsAccess": true,
    "CommentPrompts": { "AskForCommentOnViewOffline": false, ... },
    "Children": [ /* nested folders, same shape */ ],
    "Credentials": [
      {
        "Id": "<guid>", "Name": "ABC", "Username": "ABC",
        "Password": "112233", "Url": "", "Notes": "",
        "GroupId": "<guid>",
        "Created": "...", "Modified": "...", "Expires": null,
        "Tags": [ { "Name": "infra" } ],          // array of OBJECTS, not strings
        "CustomUserFields": {}, "CustomApplicationFields": { "IconId": "17" },
        "TOTPSecret": "", "TOTPDigits": 6, "TOTPPeriod": 30, "TOTPIssuer": "",
        "HasViewEntryContentsAccess": true, "HasViewEntryPasswordAccess": true,
        "HasViewTOTPAccess": true, "HasModifyEntriesAccess": true, "HasModifyTOTPAccess": true,
        "CommentPrompts": { ... },
        "Attachments": [
          { "AttachmentId": "<guid>", "CredentialObjectId": "<guid>",
            "FileName": "updates9.txt", "FileSize": 6 }   // no FileData; bytes in the zip
        ]
      }
    ]
  },
  "Attachments": [ /* flat list, same records as the per-credential Attachments[] */ ],
  "Expiry": "9999-12-31T23:59:59.9999999+00:00"
}
```

Notable corrections vs the brief's §5:

- **`Tags` is `[{"Name": "…"}]`**, not `[string]`.
- Credentials carry **native TOTP fields** (`TOTPSecret/TOTPDigits/TOTPPeriod/TOTPIssuer`) and **per-entry ACLs** (`HasView*/HasModify*`) and `CommentPrompts`.
- Attachments are referenced both per-credential and in the top-level flat list; bytes are in the zip, joined by `AttachmentId`.

### Other endpoints

- `GET /api/v6/rest/entries/{id}`, `GET /api/v6/rest/folders/{id}` — single-object fetch.
- `IsOfflineAvailable` — **the admin can disable offline packages.** On my server it returns **`true`** (the go/no-go passed). Check it and fail with a clear message rather than a confusing 4xx.
- `IsCommentRequired` — **not an endpoint on my server (404).** Comment policy is surfaced **per folder** via `CommentPrompts.AskForCommentOnViewOffline`. The `Comment` field in the request body is still accepted; send a truthful one (every offline fetch is audited).
- `GetServerInfo` — returns `ServerVersion`. **Leaks infra** (OS version, internal IPs, adapter names); never put its output in a fixture.
- Search is a `POST` with `{"Search": "..."}`. v1 doesn't need it (we search the local cache).

## 6. Mapping Pleasant → kdbx **[corrected]**

| Pleasant | kdbx |
|---|---|
| Folder | Group — `Name`, `Notes`, `Times` |
| `Credential.Name` | Entry `Title` |
| `Username` / `Password` / `Url` / `Notes` | standard entry fields (`Password` protected) |
| `Created` / `Modified` | `Times.CreationTime` / `Times.LastModificationTime` |
| `Expires` | `Times.ExpiryTime` + `Expires` flag |
| `Tags` (`[{Name}]`) | entry `Tags` — join the `.Name` values |
| `TOTPSecret` (+ `Digits`/`Period`/`Issuer`) | KeePassXC `otp` field as an `otpauth://` URI; store the raw seed in `pps.TOTPSecret` too |
| `CustomUserFields` | custom string fields, prefixed `pps.cuf.` |
| `CustomApplicationFields` | custom string fields, prefixed `pps.caf.` (heavily used — ~22% of entries — map them all) |
| Attachments | kdbx binaries via **`db.AddBinary`**; read bytes from the zip entry named `AttachmentId` |
| `Id` (GUID) | see below |

**Attachments (corrected).** Do **not** read a base64 `FileData` field — it is null. For each credential's `Attachments[]`, open the zip entry named `AttachmentId`, and add the bytes with **`db.AddBinary(data)`**, then `entry.Binaries = append(entry.Binaries, bin.CreateReference(FileName))`. The deprecated `db.Content.InnerHeader.Binaries.Add` stores the content as base64(gzip(...)) text and KeePassXC reads back garbage — the PoC caught exactly this bug, verified by extracting attachments with `keepassxc-cli` and comparing sha256 to the source (byte-for-byte identical after the fix).

**On the GUID.** A .NET GUID and a kdbx UUID are both 16 bytes, but .NET GUIDs have mixed-endian field ordering — a naive copy produces subtly wrong UUIDs. **Do not copy the bytes.** Store the server `Id` verbatim in a custom string field (`pps.Id`) as the join key, and derive the kdbx UUID **deterministically as UUIDv5 over the `Id` string** (fixed namespace). The PoC proved this: `keepassxc-cli` shows version-5 UUIDs and the round-trip is stable.

Write the source URL and the fetch timestamp into the kdbx `Meta` custom data so `status` and the TUI can report cache age without a network call.

## 7. TUI design direction

*(Unchanged from the brief — pinned for real in the M4 design phase. Summary here; the full direction is the brief's §7.)*

Deliberate, not decorated. **Not** the default Charm look (`#7D56F4`/`#FF5F87`, rounded borders everywhere, gradient header).

**Direction: locksmith's bench.** Brass/amber reserved for *secrets and the actions that touch them*; steel grays for chrome; patina teal for fresh/OK; oxidized red for expired/stale/error. `lipgloss.AdaptiveColor`; degrade to ANSI-256/16; respect `NO_COLOR`. Draw almost no borders — a single vertical rule between panes, whitespace and weight for hierarchy.

**Signature element — the clipboard countdown.** A depleting brass bar showing seconds until the copied secret is wiped. The one memorable thing; spend the boldness here.

**Cache age is structural**, not decoration — permanent in the status line, teal→red as it ages. Same for entry expiry.

Screens: `unlock → syncing → browsing → detail`. `/` opens search (§8b). `?` shows keys. Passwords masked by default, revealed with an explicit keypress, never in the list view.

## 8. Bubble Tea correctness

- Argon2 KDF unlock and the OfflinePackage fetch are slow — **neither may run inside `Update`.** Wrap both in `tea.Cmd`, show the spinner, deliver the result as a message. (The fetch is ~76 s on my vault — a real spinner with progress, not a blip.)
- Model the states as a real state machine, not a pile of booleans.
- Consider `charmbracelet/x/exp/teatest` for TUI tests.

## 8a. Responsive layout — first-class

The interface must adapt at every size and re-adapt live on resize. Build it in from the first commit.

- Handle `tea.WindowSizeMsg` in the root model and **propagate to every child** (`list.SetSize()`, `viewport`, `textinput.Width`).
- `tea.WithAltScreen()`. Never hardcode 80×24 or any number.
- Subtract frame sizes with `lipgloss.Style.GetFrameSize()`; measure with `lipgloss.Width()`/`Height()`, never `len(s)`.
- **Display width, not byte or rune count.** Use `charmbracelet/x/ansi` (`ansi.StringWidth`, `ansi.Truncate`) or `mattn/go-runewidth`. In my vault a meaningful minority of titles are non-ASCII (~8%) and the longest exceeds 100 columns — unicode is real, and §12 requires a test for it.

Breakpoints (a design decision; exact thresholds picked in M4 against real data):

| Width | Layout |
|---|---|
| ≥ 100 cols | Two-pane: tree/list left, detail right, single vertical rule |
| 60–99 | One pane, push/pop navigation |
| 40–59 | One pane, compact: drop columns |
| < 40 or < 10 rows | Refuse to render; show current size + minimum needed |

**Column priority as width shrinks.** Drop from the right: `modified` → `tags` → `url` → `username`. Title never drops. Truncate the *middle* of URLs.

**Height.** Status line and countdown are pinned; the list flexes to `total − chrome`, recomputed on every resize (including when the countdown appears/disappears).

**Non-TTY.** `harmos ls`, `get`, `status` must work piped/redirected/in CI — detect no-TTY, don't emit ANSI. Launching the TUI without a TTY exits with a clear message.

**Test it** with `teatest` at 200×50, 100×30, 80×24, 60×20, 40×12, 30×8, including a resize *sequence*. Golden-file the output.

## 8b. Search — the primary interaction **[corrected]**

95% of sessions are "get the X password into my clipboard, go away." Search *is* the TUI.

**Do not build a search index.** The corpus is **~10k entries** — the upper edge of "a few thousand," **not** tens of thousands. A linear scan over a flat slice of structs is sub-millisecond and has none of an index's cache-invalidation bugs. **But put the benchmark in the repo** (`go test -bench`, ~10k synthetic entries) as proof, per the brief.

- At unlock, flatten every source into one `[]Match` (source, qualified path, title, username, url, tags) with lowercased copies precomputed **once**. Never allocate/lowercase inside the filter loop.
- Filter synchronously in `Update`, every keystroke, **no debounce**. If you ever measure >~5ms on the real corpus, *then* move to a `tea.Cmd` — measure first.
- Search covers **all sources at once**, interleaved in one ranked list.

**Search is a mode, entered with `/`** — *corrected against the build.* Bare letters stay free for hotkeys (`c` copies a `get` command, `g` jumps to a result's folder), which a type-to-search surface would consume. `esc` clears and restores the full tree.

Ranking, best to worst *(as shipped)*: exact title → title prefix → title substring → username → tags → folder path → url → custom field → notes → attachment name → title fuzzy. Highlight matched runes in brass.

Two corrections to the original ranking. **Fuzzy is the last resort, not the third tier** — a loose subsequence (`ppk` scattered across `GRPPHVC04K`) outranking real substring hits is what makes a search feel useless; the shipped matcher is hand-written (no `sahilm/fuzzy`) and gates a subsequence on tightness. **Notes *are* searched by default**, ranked low rather than hidden behind a modifier — an opt-in nobody discovers is not a feature. Protected custom fields match by name only, so a search never surfaces a secret value.

**Field scopes:** `title: user: url: notes: tag: path: field: file:` and `src:` (source). `src:` is a filter, not a ranking signal: it decides which entries are eligible and leaves the tier to the terms beside it.

**The fast path.** `enter` on a result **copies the password immediately** and starts the countdown. `tab`/`→` opens detail. Because `enter` acts without confirmation, the confirmation line shows the **fully qualified target** (`client-a/Infra/db-prod`) so a wrong-source copy is visible instantly.

**The CLI uses the same matcher.** `harmos get <query>` ranks identically — same code path. If ambiguous, **print candidates and exit non-zero.** Never guess in a scriptable command.

## 9. Security requirements

- **Never** log/print/`%v` a password, token, or master password. Add `String()` returning `[redacted]` on any secret-carrying type.
- Cache file and config: `0600`. Config directory `0700`.
- Master password and OAuth token: never in the config file. Token → OS keyring.
- Clipboard cleared after N seconds (default 30). Clear on exit. If something else overwrote our value, don't clobber it.
- Password never rendered in the list view.
- **TLS verification on by default.** For the internal CA, support a CA bundle; **do not** add a blanket `--insecure`. Prefer targeting the certificate's SAN hostname (§5).
- Be honest about memory zeroing: use `[]byte` for secrets where practical, zero best-effort, claim no more in the README.

### The clipboard needs more than a timer

The real threat is that the password is captured elsewhere before the timer fires (Klipper, Windows clipboard history, Maccy, macOS Universal Clipboard syncing to my phone). Opt out per platform:

| Platform | Marker |
|---|---|
| macOS | `org.nspasteboard.ConcealedType` (needs **cgo**) |
| KDE / Klipper | `x-kde-passwordManagerHint: secret` |
| Windows | `ExcludeClipboardContentFromMonitorProcessing` (no cgo) |
| GNOME / wl-clipboard | no standard; document the gap honestly |

`atotto/clipboard` cannot set these. Write it yourself in `internal/clip`, per platform. Reference: `blindcopy` (Rust), `arboard` (Rust), KeePassXC's `Clipboard.cpp`. macOS is the one cgo file; keep Linux/Windows pure-Go so they cross-compile. goreleaser builds darwin on a macOS runner.

### Honor the server's `Expiry`

The package carries a server-issued `Expiry`. **On my server it is `9999-12-31` (never)** — the admin set no short expiry, so the cache is durable today. But a different admin/instance may set 7 days, and that expresses a policy. **Enforce it:** an expired cache means `sync` or fail, with a message that says so.

### Stale cache can lock me out — warn before it does

If a password rotated on the server and I copy the old one, a few retries can **lock my account**. `harmos get` must warn on **stderr** when the cache is older than a configurable threshold; `harmos status` must make age impossible to miss. Warn on stderr so pipelines still work.

## 10. CLI surface **[corrected — sync UX]**

Binary and module name: **`harmos`** (coined, lowercase, not an acronym — Greek ἁρμός, "the joint where two fitted parts meet"). The README opens with that one line and never explains it again.

```
harmos init                  # create a profile interactively
harmos profile ls|add|rm
harmos sync [profile]        # pull OfflinePackage → local kdbx cache
harmos                       # launch the TUI (default command)
harmos ls [profile]          # list entries, scriptable
harmos get <query>           # print or copy a password; for scripts
harmos status                # profiles, cache age, package expiry
```

**Sync is slow and heavy — design for it.** One pull of my vault is **tens of MB and over a minute**, dominated by attachments. This is not "a few seconds":

- `harmos sync` shows real progress (bytes/percent where possible), not just a spinner blip.
- **Pull once.** Never fetch on a timer or in the background; every fetch is audited and attributed. Sync is always explicit and user-initiated.
- Stream the ZIP to disk (`0600`), then parse from the file — don't hold 52 MB of response in memory unnecessarily.

Config at `$XDG_CONFIG_HOME/harmos/config.toml`. Profile names unique (validate at load). `--profile` and `HARMOS_PROFILE` select a subset.

```toml
default = "work"
clipboard_timeout = "30s"

[[profile]]
name  = "work"
type  = "pleasant"
url   = "https://pps.internal.example:10001"   # target the certificate SAN hostname (§5)
user  = "..."
cache = "~/.local/share/harmos/work.kdbx"
# ca_bundle = "~/.certs/internal-ca.pem"

[[profile]]
name = "personal"
type = "kdbx"
path = "~/vaults/personal.kdbx"              # read-only, never written
keyfile = "~/.keys/personal.key"
```

Suggested layout:

```
cmd/harmos/
internal/
  config/
  vault/            # the single kdbx reader — everything downstream uses this
  source/
    pleasant/       # API client + mapper. The only package that knows Pleasant exists.
    localkdbx/
  tui/  clip/  secret/  theme/
scripts/sanitize/   # the PoC sanitizer (fixture generator)
```

## 11. Repo hygiene (MIT, public GitHub)

- `LICENSE` — MIT. `NOTICE` with the **"not affiliated with or endorsed by Pleasant Solutions or the KeePass project"** line. Third-party trademarks used nominatively only.
- `README.md` — what it is, install, config, a screenshot/VHS gif, the non-affiliation notice.
- **No internal hostnames, usernames, org names, IPs, or real data anywhere in the repo or history.** Test fixtures are synthetic (produced by the sanitizer). `GetServerInfo` output in particular leaks infra — never commit it.
- `.gitignore` covering config, `*.kdbx`, `.env`, dumps, `FINDINGS.md`.
- `SECURITY.md` with a disclosure address and an honest "not audited" line.
- CI: `go vet`, `golangci-lint`, `go test`, build on linux/macos/windows (macOS on a real runner for cgo). `gitleaks` on every push and full history.
- `goreleaser` for binaries.

## 12. Testing

- The mapper is where the bugs live. Table-driven tests, synthetic fixtures.
- **Fixtures come from the sanitizer** (`scripts/sanitize/`): a real OfflinePackage zip → a safe zip (`<guid>.json` manifest + tiny fake attachment blobs), consistent GUID remap, scrubbed secrets, preserved structure/unicode/edge cases. Fake the Pleasant server with `httptest` serving the sanitized zip.
- kdbx round-trip: map → write → read → assert equality, including attachments and custom fields.
- **The oracle: `keepassxc-cli` in CI.** Generate a cache, have `keepassxc-cli` open/list/extract it, and compare — including an **attachment sha256 check** (this is what caught the `AddBinary` bug). Where a spec says "open it and look," automate it.
- Explicitly test the edge cases the PoC found: empty password, duplicate entry names in one folder (common — many folders have them), unicode names, an entry with many attachments (over a dozen), an expired entry, a `HasViewEntryContentsAccess=false` folder.

## 13. Verified — the PoC already ran

The PoC (`docs/harmos-brief.md` §2, output `docs/FINDINGS.md`) answered every open question against the live server. Summary: **GO.** The response is a ZIP (not JSON), sync is tens of MB and over a minute, the corpus is ~10k entries, TOTP is a native field, and the mapper produces a KDBX4 file that KeePassXC and MacPass both open correctly. The corrections are folded into this document; see FINDINGS for the raw numbers.

## 14. A decision I want surfaced, not made

Argon2 costs ~1 s per unlock. In the TUI, paid once per session — fine. In a script, **every** `harmos get` pays it. The cache is a **derived artifact**, not my primary vault, so it need not inherit the server's KDF parameters — deliberately lowering Argon2 cost on the cache (with the reasoning and threat model written down) is defensible. The alternatives are a background agent (large security surface) or a slow `get`. **Measure the real cost, present the three options, let me pick.** Don't arrive at an agent by accident.

## 15. Explicitly later — do not build these now

- **`git-credential-harmos`** — once the §8b matcher exists, a git credential helper is ~50 lines. Not v1.
- **TOTP** — Pleasant stores TOTP as native fields (`TOTPSecret` etc.), so mapping to KeePassXC's `otp` convention is **cheap**. But very few entries carry a seed today, so the payoff is small. Attractive, still not v1.

---

## A note on the environment

This is a corporate password server. The offline package pulls every credential I can see, in cleartext, onto local disk — that's what the endpoint is for, and the server admin can turn it off (`IsOfflineAvailable`) and can require a logged justification (per-folder `CommentPrompts`). **Assume every offline fetch is audited.** Don't fetch on a timer or in the background; sync is explicit and user-initiated.

There's a security team here, and they will eventually notice a client pulling the full package. The good version of that conversation happens before it starts, with me showing them an MIT-licensed, auditable, read-only tool that honors `Expiry` and `IsOfflineAvailable`. That's why the §9 constraints aren't decoration — they're the argument. Build so that reading the source is a sufficient answer.
