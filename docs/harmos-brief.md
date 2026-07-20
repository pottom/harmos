# harmos — complete Claude Code brief

This single file is the whole handoff. It contains five documents, concatenated with divider bars:

1. **README / map** — what this is and the order to work in
2. **PoC brief** — run this FIRST, in a scratch dir with no repo
3. **Spec** — the what and the how
4. **Workflow** — milestones, branches, gates
5. **AGENTS.md** — becomes the repo's root contract at M2a

The TUI mockups referenced by the spec (§7, §8a) are the six PNGs in `docs/design/` — visual direction only, not a spec to reproduce pixel-for-pixel.

**Start by reading all five sections below. Then execute only the PoC (section 2) and stop at FINDINGS.md.**


═══════════════════════════════════════════════════════════════════════════════
# SECTION 1 — README / MAP
═══════════════════════════════════════════════════════════════════════════════

# harmos — handoff package

Read this first. It's the map to the other files.

harmos is a read-only terminal password client for **Pleasant Password Server** (a.k.a. KeePass Hub) and local `.kdbx` files. It syncs a Pleasant server's offline package into a local kdbx cache and reads that cache — plus any local kdbx files — through one shared reader, in a Bubble Tea TUI and scriptable CLI. Go, MIT, public repo.

## The files, and the order

1. **The PoC brief (section 2 below)** — do this **first**, in a throwaway scratch dir with **no repo and no remote**. It verifies the one assumption the whole project rests on (that the server's `OfflinePackage` endpoint works and is enabled). Output is a `FINDINGS.md`. Then stop.
2. **`FINDINGS.md`** — *you* produce this from the PoC. It corrects the spec against the real server before any real build starts.
3. **The spec (section 3 below)** — the what and the how: architecture, the Pleasant API, the kdbx mapping, security requirements, the TUI direction, the CLI surface. Section 13 points back at the PoC.
4. **The workflow (section 4 below)** — the milestone plan and working agreement: what counts as a milestone, the branch/merge model, the stop-and-wait gates, and the detailed M1/M2 issue lists.
5. **The AGENTS.md (section 5 below)** — becomes the repo's root `AGENTS.md` at M2a, written to disk as `AGENTS.md` and the docs to `docs/`. The binding DOX contract: ownership, local contracts, global workflow, verification, and the child-doc index.

Supporting: **`harmos-mockups.html`** and **`m1`–`m6` PNGs** are the visual direction for the TUI — reference only, not a spec. The real design is pinned in the M4 design phase against real data. Don't reproduce the mockups pixel-for-pixel; the spec's §7 and §8a are the actual rules.

## The two things only you can settle

- **The module path** — the repo owner's GitHub username goes into `github.com/<owner>/harmos`. Ask.
- **`IsOfflineAvailable`** — the PoC's go/no-go. If the admin disabled offline packages, the whole design changes, and that's a human decision, not yours.

## The three hard stops

Finish and wait for the human at each: **after the PoC**, **after M2b** (weeks of real use with KeePassXC before any TUI), and **after the M4 design phase** (present options, wait for picks). Green CI does not clear these — they're judgement gates.

## Non-negotiables, up front

- **Nothing real touches git.** `.gitignore` before `git init`; never `git add .`; `gitleaks` on every push and the full history. One stray offline package in the history burns the repo.
- **No AI attribution.** No `Co-Authored-By`, no "Generated with" trailer, no tool as author on any commit, PR, or file.
- **Read-only means read-only**, including never writing to or locking a user's own local kdbx file.
- **Verify the name and the licence** in the PoC: `harmos` free on pkg.go.dev and distro package lists, and `gokeepasslib` permissively licensed (the Go choice depends on it).


═══════════════════════════════════════════════════════════════════════════════
# SECTION 2 — POC BRIEF (run first, scratch dir, no repo, no remote)
═══════════════════════════════════════════════════════════════════════════════

# Stage zero: Pleasant Password Server PoC

This is **not** the project. This is a throwaway spike whose only job is to find out whether the project's central assumption is true, before anyone writes a line of the real thing.

Read `harmos-spec.md` for context, then ignore most of it. Your scope here is §5 and §13 of that document and nothing else.

## The question

The whole design rests on one endpoint:

> `POST /api/v5/rest/OfflinePackage` returns my entire vault — folders, entries with cleartext passwords, attachments — in one call.

If that's true, the project is easy. If it's false, the project is a different project. Everything else in the spec is downstream of it.

The problem is that I can't verify it from here. My server is **Pleasant Password Server 9.3.3** (July 2026). The documentation the spec was written from describes **API v6**, and the docs navigation shows a **v7** exists that neither of us knows anything about. The spec's §5 is an educated guess dressed up as a specification. Treat it as a hypothesis to test, not a contract.

## Hard scope limits

**Do not build:**

- any TUI. No bubbletea, no lipgloss, no bubbles.
- cobra, config files, profiles, multi-source anything.
- packages, interfaces, or a directory structure. One `main.go` is correct here.
- tests, CI, a README, a license.

The temptation will be to start on the interesting part. The interesting part has no risk in it. Resist.

**Do build:** the shortest possible vertical slice that produces a real `.kdbx` from my real server.

## Stage 0 — curl, before any Go

Answer these with `curl` and paste the results into findings. This is thirty minutes, and it may end the project or change it fundamentally.

1. `GetServerInfo` — what is the actual `ServerVersion` and the highest working API version?
2. Does `/api/v7/rest/...` respond? Does `/api/v5/...` still work on a 9.x server, or has it been removed?
3. `POST /OAuth2/Token` with `grant_type=password` — does it work? Does the account require **2FA**, and if so what's the flow?
4. `Authorization: Bearer <token>` or a bare `<token>`? The official PowerShell example uses the bare form, which is unusual. Test both, record which one the server accepts.
5. **`IsOfflineAvailable` — the go/no-go.** The admin can disable offline packages server-side.
6. `IsCommentRequired` — does the server demand a justification string?

**If (5) is false, stop and tell me.** Do not work around it, do not start walking the tree entry-by-entry to reconstruct the package. That's a different design with different performance and audit characteristics, and it's my call whether to pursue it, not yours.

## Stage 1 — dump

~100 lines of Go. Auth, `POST /api/vN/rest/OfflinePackage`, write the raw JSON to a scratch file, print statistics.

Credentials come from the environment or a prompt. **Nothing hardcoded, nothing in a file, nothing in shell history.** Scratch output goes to a gitignored directory, mode `0600`.

Then tell me about the real corpus, because the spec makes claims that depend on it:

- How many folders, entries, attachments? What's the maximum tree depth?
- **How long does the call take, and how large is the response?** The spec assumes sync is a foreground action of a few seconds. If it's 45 seconds and 200MB because attachments are inlined, the sync UX in §10 is wrong.
- **Is the entry count in the thousands or the tens of thousands?** §8b asserts that a linear scan beats an index and forbids building one. That assertion has a corpus size baked into it. Check it.
- Which fields are actually populated versus always null? Are `CustomUserFields` / `CustomApplicationFields` used at all?
- **What is the real `Expiry` value on the package?** The docs example says year 9999, i.e. never. If my admin set it to days, that's a policy, and §9 of the spec requires enforcing it. This single field decides whether the cache is durable or perishable, and the whole offline story depends on which.
- **Are TOTP seeds in there?** Check `CustomUserFields`, `CustomApplicationFields`, and the notes for anything that looks like an `otpauth://` URI or a base32 seed. Don't build anything with it — just report. It decides whether §15 is cheap or impossible.
- Does the response shape match §5? Diff it properly. Note every field the docs didn't mention and every documented field that isn't there.
- Are there entries that will break the mapper: empty passwords, duplicate names in one folder, unicode or RTL names, several attachments on one entry, expired entries, a folder with `HasViewEntryContentsAccess: false`?

## Stage 2 — map and prove it

Map the dump to a `.kdbx` with `gokeepasslib/v3`, per §6 of the spec. Then the actual proof:

**Open the result in KeePassXC and confirm it looks right.** Folders nested correctly, passwords intact, attachments openable, unicode not mangled. If KeePassXC can't open it or shows garbage, the mapper is wrong, and finding that out now costs an afternoon instead of a rewrite.

Watch the GUID trap from §6: .NET GUIDs are mixed-endian and a naive 16-byte copy into a kdbx UUID produces subtly wrong values that look fine until they don't.

## Deliverables

The code is disposable. These are what survive into the real repo:

1. **`FINDINGS.md`** — every question above, answered, with the real numbers. Explicitly list where §5 of the spec was wrong. That document is what I'll use to correct the spec before the real build starts.
2. **Sanitized golden fixtures** — the real response shape, with invented data. These become the `httptest` fixtures in §12.
3. **The sanitizer itself** — a small program that turns a real dump into a fixture. Write it carefully: it must scrub passwords, usernames, hostnames, GUIDs, org names, and note bodies while preserving structure, nesting, unicode, and edge cases. This is the one piece of PoC code worth keeping, because it's how I'll safely capture regressions later.

## The one rule

Real dumps, tokens, and unsanitized fixtures **never touch git** — not a commit, not a stash, not the reflog. Do this work in a scratch directory outside the repo, or in a repo with no remote. The public MIT repo comes later and starts clean.

Assume every OfflinePackage fetch is logged and attributed to me on a corporate server. Pull once, work from the file on disk, don't loop.

## One unrelated errand while you're here

Confirm the name **`harmos`** is free before it's baked into a module path. GitHub and the general namespace are already clear (checked while choosing it); what's left is `pkg.go.dev` and the Debian/Ubuntu/Fedora/Homebrew/AUR/npm/PyPI package lists. Two earlier candidates (`keyway`, `ward`) died at exactly this step — `ward` turned out to be a Go password manager CLI, same language and domain — so finish the check rather than assuming. Report what you find; don't rename anything on your own.

**And check `gokeepasslib`'s licence.** The whole language choice rests on it being the only mature, permissively-licensed kdbx writer (§4a of the spec), and the repo is meant to be MIT (§11). I believe it's permissive but I have not verified it. If it turns out to be copyleft, that's a §4a-level problem, not a footnote — stop and tell me.

## When you're done

Report back with FINDINGS.md and a recommendation: does `harmos-spec.md` survive contact with the real server, or does it need surgery? Don't start building the real thing. That's a separate session and a separate decision.


═══════════════════════════════════════════════════════════════════════════════
# SECTION 3 — SPEC
═══════════════════════════════════════════════════════════════════════════════

# Project brief: a terminal password client for Pleasant Password Server

You are building a new open-source Go project from scratch. Read this whole brief before writing code. At the end there is a list of things you must verify against the real server before you rely on them — do that first, then come back and confirm the plan with me.

---

## 1. What this is

A CLI/TUI tool for browsing and copying my passwords. Two sources:

1. **Pleasant Password Server** (a.k.a. KeePass Hub, by Pleasant Solutions) — a commercial .NET password server with a REST API. My instance is **9.3.3**.
2. **Local `.kdbx` files** — plain KeePass 2 databases on disk.

**v1 is strictly read-only.** Browse, search, copy. No writing back to the server, no entry editing, no bidirectional sync. This is a deliberate scope decision, not an oversight — see §3.

### Ship it in two stages, and stop after the first

**v0.1 is `harmos sync` and nothing else.** No TUI. No bubbletea in `go.mod`. It authenticates, pulls the offline package, writes the kdbx, and exits. That's it — and it's already useful, because KeePassXC opens the result.

Then stop. I'm going to use it that way for a few weeks before the TUI gets written.

The reason is not process hygiene, it's that this project has an obvious failure mode. After the PoC there'll be a working dump→kdbx pipeline, which is roughly 90% of the *value* and 20% of the *work*; the TUI is the remaining 10% of value and 80% of the work. That's exactly the shape of project that gets abandoned at 95%, because the fun part is also the optional part. Shipping the boring half first means the thing is useful even if the TUI never happens — and if it does happen, it'll be built against weeks of real usage rather than against §7 of a document.

So: when v0.1 works, **don't start the TUI.** Tell me it's done and wait.

## 2. The one architectural insight

Pleasant Password Server is **not** kdbx-backed. It has its own database; "KeePass" in the product name means it's compatible with the KeePass desktop client via a plugin. There is no `.kdbx` file on the server to download.

So: **the Pleasant backend's only job is to produce a kdbx file.** Everything downstream — the TUI, search, clipboard, detail view — reads a kdbx and knows nothing about Pleasant.

```
Pleasant API ──► mapper ──► local .kdbx cache ─┐
                                               ├──► vault reader ──► TUI / CLI
local .kdbx file ──────────────────────────────┘
```

Two producers, one reader. Do **not** build a general "vault abstraction" interface with a Pleasant implementation and a kdbx implementation — that's the obvious design and it's the wrong one here. It doubles the surface area for zero benefit, because the kdbx format is already a superset of what Pleasant exposes.

The local cache is a real kdbx, encrypted with a master password I choose at `init`, openable in KeePassXC. That gives us at-rest encryption, attachments, and interop for free.

## 2a. Multiple sources — N Pleasant servers and N local files

I will have several sources at once: more than one Pleasant server (e.g. two environments, or two clients), plus several local `.kdbx` files. This is not a "nice to have later" — the data model must assume N sources from the start. Retrofitting a second source into a single-vault design means touching every layer.

**One cache per source. Never merge them into one kdbx file.** Sources have independent lifecycles, independent credentials, independent trust. Merging destroys provenance, and provenance is a safety property here (see below).

**Tree shape: profiles are the root level.** Don't ask me to "select a profile" from a menu and then browse one vault at a time — that's a modal dialog pretending to be navigation. Instead, the top level of the tree *is* the list of sources; expanding one descends into its folders. Provenance then becomes structural: the path I'm looking at always tells me which source I'm in.

**One unlock, all sources.** An earlier draft of this brief said "unlock lazily, search what's unlocked." That's wrong, and the reason matters: search has to cover everything (§8b), and you cannot search a locked source. But prompting for N passwords at startup is also intolerable. The resolution:

- **`harmos` owns its caches, so it gets to pick their key.** All Pleasant caches are encrypted with a *single* harmos master password, chosen at `init`. One prompt opens all of them. They remain individually valid kdbx files, openable in KeePassXC with that password.
- **External kdbx sources keep their own passwords** — they're my files, I'm not re-keying them. Offer at `profile add` to store the password in the OS keyring (opt-in, per source). If stored, it opens with the same single unlock. If not, that source is lazily unlocked and search reports it as excluded.
- **Never build an unencrypted search index.** Entry titles, usernames, URLs, and folder paths are themselves sensitive — a list of names like `prod-db-root` discloses infrastructure even with no passwords attached. There is no metadata tier here that's safe to leave in the clear.

So the common case is: one password, everything searchable. The degraded case (an external kdbx not in the keyring) is explicit and visible, never silent.

**Every entry carries a visible source badge — always, including in search results and in the detail view.** This is the one piece of chrome that never gets dropped at narrow widths (§8a). Rationale: if I have a production and a staging Pleasant server, both will contain an entry called `admin`, and copying the wrong one is the worst thing this program could do to me. Provenance outranks aesthetics here.

**Namespace by profile.** Two servers both have a folder called `Root`. Profile names are unique (enforce at config load) and qualify everything: `work/Infra/db-prod`, `client-a/Infra/db-prod`.

**Partial failure is the normal case.** One server is unreachable, one is fine, local files always work. Consequently:

- `harmos sync` with no argument syncs all sources concurrently (bounded — `errgroup` with a limit, per-source timeout), and **succeeds partially**. Report per-source outcomes. Exit non-zero only if every source failed.
- A dead or unreachable source must never prevent the TUI from starting or block browsing the others. Render it in the tree with an error state and let me keep working.
- `status` reports per-source: last sync, cache age, package expiry, last error.

**The two source types are asymmetric — don't paper over it.** A `type = "kdbx"` source has no cache, no sync, and no meaningful "cache age": the file *is* the source, read directly. `harmos sync` on one is a no-op, not an error, and says so. Conversely a `pleasant` source has no meaningful `path`.

> **Safety requirement:** for a local kdbx source, open the file **read-only**. Never write to it, never create a KeePass `.lock` file beside it, never rewrite it "to update timestamps." That file may be my actual primary vault, and v1 has no business modifying it. This is not implied by "v1 is read-only" — state it in the code as an invariant and test it (assert mtime and bytes are unchanged after a full browse session).

## 3. Why read-only (do not talk me out of this)

Writing back would require bidirectional reconcile, and Pleasant's API makes that genuinely nasty:

- **No delta endpoint.** The offline package is a full dump every time.
- **No tombstones.** A deleted entry just vanishes from the dump.
- **Permissions look identical to deletion.** Pleasant has per-folder access levels. If my access to a folder is revoked, those entries also vanish from the dump. You cannot distinguish "deleted on the server" from "I can't see it anymore" — and guessing wrong means either resurrecting a deleted credential or destroying one.
- **No visible optimistic locking** in the API docs. Probably last-write-wins.

Read-only sidesteps all of it. If v2 ever adds writes, it'll be explicit per-entry pushes with confirmation, never an automatic merge.

## 4. Stack

- Go (current stable). Module path: `github.com/<me>/harmos` — ask me for the actual path.
- **kdbx:** `github.com/tobischo/gokeepasslib/v3` — supports KDBX4 (Argon2, ChaCha20).
- **TUI:** `charmbracelet/bubbletea`, `charmbracelet/bubbles` (`list`, `textinput`, `viewport`, `spinner`), `charmbracelet/lipgloss`.
- **CLI:** `spf13/cobra`.
- **Config:** TOML. `BurntSushi/toml` is enough — don't pull in Viper for four fields.
- **Clipboard:** hand-rolled per platform, not a library — see §9 and §4a. `atotto/clipboard` cannot meet the requirement.

## 4a. Why Go — the decision is made, don't re-litigate it

Rust was evaluated seriously. Go won on exactly one argument, and it's the load-bearing one:

**This project's core operation is *writing* a kdbx** (§2 — the Pleasant backend's only job is to produce one). Only Go has a mature, permissively-licensed kdbx writer:

| | writes KDBX4 | licence |
|---|---|---|
| `tobischo/gokeepasslib/v3` (Go) | stable | permissive — **verify** |
| `keepass` / sseemayer (Rust) | **experimental** | — |
| `kdbx-rs` / tonyfinn (Rust) | yes | **GPL-3.0+** — forces the whole project to GPL |

A password cache is not something to write with experimental support: the failure mode isn't a rendering glitch, it's a corrupted file full of my credentials. And GPL contradicts §11.

If you find yourself thinking Rust would be better here, you're probably right about the *language* and wrong about the *library*. That's the trade that was made. Two things Go genuinely costs us, both accepted knowingly:

1. **Clipboard concealment needs cgo on macOS** (§9), which loses `CGO_ENABLED=0` cross-compilation for darwin. Rust has `blindcopy` and `arboard`; Go has nothing equivalent. Contained to one build-tagged file.
2. **Memory zeroing is best-effort only.** Go's GC moves objects and strings are immutable, so secrets cannot be reliably wiped. Rust's `zeroize`/`secrecy` would fix this. We accept it and say so honestly in the README rather than implying protection we don't have.

- **Token storage:** `zalando/go-keyring` (OS keychain). Never the config file.
- **HTTP:** stdlib `net/http`. No client framework.

## 5. The Pleasant API

Base URL is typically `https://host:10001`. **Verify all of this against the live server before building on it** — my server is 9.3.3, the public docs I have describe v6, and the docs navigation shows a v7 exists. I don't know what changed.

### Auth

`POST /OAuth2/Token`, `Content-Type: application/x-www-form-urlencoded`:

```
grant_type=password&username=<user>&password=<pass>
```

Response JSON contains `access_token`. Put it in the `Authorization` header on all subsequent calls.

> **Verify:** the official PowerShell example sets `Authorization` to the raw token with no `Bearer ` prefix, which is unusual. Test both forms and use whichever the server actually accepts. Also check token TTL and whether a refresh token comes back.
>
> **Verify:** the docs have an "OAuth Two-Factor Support" page. Find out whether my server requires 2FA and what the flow is.

### The endpoint that makes this project easy

`POST /api/v5/rest/OfflinePackage`, body `{"Comment": "..."}`

This exists precisely so KeePass-like clients can go offline. One call returns the **entire tree** — folders, entries with cleartext passwords, and attachments base64-encoded — plus a package-level `Expiry`. No need to walk the tree entry by entry.

Response shape (abridged, from the v5 docs):

```jsonc
{
  "Root": {
    "Id": "c04f874b-...", "Name": "Root", "ParentId": "00000000-...",
    "Notes": null, "Created": "...", "Modified": "...", "Expires": null,
    "Tags": [], "CustomUserFields": {}, "CustomApplicationFields": {},
    "HasModifyEntriesAccess": true, "HasViewEntryContentsAccess": true,
    "CommentPrompts": { "AskForCommentOnViewPassword": false, "AskForCommentOnViewOffline": false, ... },
    "Children": [ /* nested folders, same shape */ ],
    "Credentials": [
      {
        "Id": "13caaa57-...", "Name": "ABC", "Username": "ABC",
        "Password": "112233", "Url": "", "Notes": "",
        "GroupId": "c04f874b-...",
        "Created": "2018-08-08T09:15:17-06:00",
        "Modified": "2018-08-08T09:25:45-06:00",
        "Expires": null,
        "Tags": [], "CustomUserFields": {}, "CustomApplicationFields": {}
      }
    ]
  },
  "Attachments": [
    { "CredentialObjectId": "13caaa57-...", "AttachmentId": "496ea02f-...",
      "FileName": "updates9.txt", "FileData": "UGFzc01h", "FileSize": 6 }
  ],
  "Expiry": "9999-12-31T23:59:59.9999999+00:00"
}
```

Note `Attachments` is a **flat list** joined back to entries via `CredentialObjectId`, not nested.

### Other endpoints worth knowing

- `GET /api/v5/rest/entries/{id}`, `GET /api/v5/rest/folders/{id}` — single-object fetch. (`Entries`/`Folders` are v5+ aliases for the older `Credential`/`CredentialGroup` names; both still work.)
- `IsOfflineAvailable` — **the admin can disable offline packages server-side.** Check this and fail with a clear message rather than a confusing 4xx.
- `IsCommentRequired` — the server may demand a justification string; that's what `Comment` in the request body is for.
- `GetServerInfo` — returns `ServerVersion`. Use it to detect the API version at runtime.
- Search is a `POST` with `{"Search": "..."}`. v1 doesn't need it (we search the local cache), but note it exists.

## 6. Mapping Pleasant → kdbx

| Pleasant | kdbx |
|---|---|
| Folder | Group — `Name`, `Notes`, `Times` |
| `Credential.Name` | Entry `Title` |
| `Username` / `Password` / `Url` / `Notes` | standard entry fields (`Password` protected) |
| `Created` / `Modified` | `Times.CreationTime` / `Times.LastModificationTime` |
| `Expires` | `Times.ExpiryTime` + `Expires` flag |
| `Tags` | entry `Tags` |
| `CustomUserFields` | custom string fields, prefixed |
| `CustomApplicationFields` | custom string fields, prefixed |
| Attachments | kdbx binaries (KDBXv4 inner-header form) |
| `Id` (GUID) | see below |

**On the GUID:** a .NET GUID and a kdbx UUID are both 16 bytes, so a direct mapping is tempting. Don't do it naively — .NET GUIDs have mixed-endian field ordering and you will produce subtly wrong UUIDs that look fine in testing. Store the server ID verbatim in a custom string field (`pps.Id`) as the join key. If you also want a stable kdbx UUID, derive it deterministically (e.g. UUIDv5 over the server ID) and document the choice.

Write the source URL and the fetch timestamp into the kdbx `Meta` custom data so `status` and the TUI can report cache age without a network call.

## 7. TUI design direction

I want this to look good. That means **deliberate**, not decorated.

**Do not** produce the default Charm look: the `#7D56F4` purple / `#FF5F87` pink pair, rounded borders around every pane, a gradient header. That's the lipgloss README, it's what every AI-generated TUI looks like, and it says nothing about this program.

**Direction: locksmith's bench.** The subject's world is brass, steel, and tumblers — physical security, not neon cyber. Build a small token set and use it consistently:

- **Brass/amber** — the accent. Reserved for *secrets and the actions that touch them*: the revealed password, the selected row, the copy affordance. Nothing else gets to be amber.
- **Steel grays** — all chrome, structure, labels, inactive rows.
- **Patina teal** — fresh/synced/OK status.
- **Oxidized red** — expired entries, stale cache, errors.

Use `lipgloss.AdaptiveColor` so it survives light terminals, and degrade sanely to ANSI-256 and 16-color. Respect `NO_COLOR`.

**Restraint:** most TUIs over-box. Draw almost no borders. Separate the tree pane from the detail pane with a single vertical rule and let whitespace do the rest. Type hierarchy comes from weight, dimming, and spacing — not from frames.

**Signature element — the clipboard countdown.** When I copy a password, show a depleting brass bar with the seconds remaining before it's wiped. This is the one memorable thing in the program and it earns it: it's the only place where the interface shows me the actual lifetime of an exposed secret. Spend the boldness here and keep everything else quiet.

**Structure that encodes something true:** cache age is not decoration, it's the most important fact on screen when I'm offline. Put it in the status line permanently, and let it go from teal to red as it ages past a threshold. Same for entry expiry.

**Screens / states:** `unlock → syncing → browsing → detail`. Tree/list on the left, entry detail on the right. `/` filters (bubbles `list` has fuzzy filtering built in). `?` shows keys. Passwords masked by default, revealed with an explicit keypress, never revealed in the list view.

**Copy discipline** (this is design material too): errors state what happened and how to fix it, in the interface's voice, and never apologize. Empty states invite an action — an empty vault says "Nothing synced yet — press s", not "No entries found."

## 8. Bubble Tea correctness

- Argon2 KDF unlock is ~0.5–2s and RAM-hungry. The OfflinePackage fetch is network-bound. **Neither may run inside `Update`.** Wrap both in `tea.Cmd`, show the spinner, deliver the result as a message.
- Model the states as a real state machine, not a pile of booleans.
- Consider `charmbracelet/x/exp/teatest` for TUI tests.

## 8a. Responsive layout — treat this as a first-class requirement

The interface must adapt to the terminal, at every size, and re-adapt live while I drag the window. This is not a polish item to do at the end; retrofitting it into a layout built around hardcoded widths is a rewrite. Build it in from the first commit.

**Mechanics**

- `tea.WindowSizeMsg` arrives once at startup and again on every resize. Handle it in the root model, store the dimensions, and **propagate to every child component** — `list.SetSize()`, `viewport.Width/Height`, `textinput.Width`. A child that never gets resized will silently render at its default and look broken only at certain sizes.
- Use `tea.WithAltScreen()`.
- **Never hardcode 80×24**, or any number, anywhere.
- Subtract frame sizes properly. `lipgloss.Style.GetFrameSize()` returns the horizontal/vertical space that borders and padding consume; failing to account for it is the classic off-by-N that causes wrapping and scrollbar jitter. Measure with `lipgloss.Width()`/`lipgloss.Height()`, never `len(s)`.

**Truncation and width — get this right**

`len(s)` is bytes. `utf8.RuneCountInString` is runes. Neither is display width. CJK and emoji are double-width, combining marks are zero-width, and ANSI escape sequences have no width at all but will be counted by naive slicing — and slicing through an escape sequence corrupts the rest of the line.

Use `charmbracelet/x/ansi` (`ansi.StringWidth`, `ansi.Truncate`) or `mattn/go-runewidth` for all measurement and truncation. There will be entries with unicode names; §12 requires a test for exactly this.

**Breakpoints — a design decision, not a fallback**

Width drives the layout mode:

| Width | Layout |
|---|---|
| ≥ 100 cols | Two-pane: tree/list left, entry detail right, single vertical rule between |
| 60–99 | One pane, push/pop navigation: list → detail is a screen transition, `esc` goes back |
| 40–59 | One pane, compact: drop columns per the priority below |
| < 40 or < 10 rows | Refuse to render. Show a centered, calm one-liner: the current size, the minimum needed. |

The narrow mode is not a degraded version of the wide one — it's a different interaction model (navigate *into* an entry rather than preview it alongside). Implement it as such. Pick the exact thresholds by looking at what the content actually needs, not from this table; these are my guesses.

**Column priority as width shrinks.** Drop from the right: `modified` → `tags` → `url` → `username`. Title never drops. Truncate with an ellipsis, and truncate the *middle* of URLs, where the distinguishing part is usually at both ends.

**Height**

The status line (cache age, profile) and the clipboard countdown are pinned; the list flexes to fill what's left. Compute the list height as `total − chrome`, recomputed on every resize. When the countdown appears and disappears it changes the available height — handle that rather than letting it overlap the last row.

**The signature element scales too.** The clipboard countdown bar takes available width; at narrow sizes it degrades to a numeric `28s` rather than a squashed two-cell bar.

**Non-TTY**

`harmos ls`, `get`, and `status` must work with no TTY at all — piped, redirected, in CI. Detect it and don't emit ANSI. If the TUI is launched without a TTY, exit with a clear message rather than crashing or hanging.

**Test it**

`teatest` lets you send `tea.WindowSizeMsg` directly. Assert the layout renders correctly at 200×50, 100×30, 80×24, 60×20, 40×12, and 30×8 — including a resize *sequence*, since state that survives a resize (scroll position, selection) is where the bugs are. Golden-file the output.

## 8b. Search — the primary interaction

Be honest about what this program is for. I don't browse a password vault for pleasure; 95% of sessions are "I need the X password, get it into my clipboard, go away." Search is not a feature of the TUI, it *is* the TUI. Everything else is secondary.

### Fast: do the boring thing

**Do not build a search index. Do not import bleve, or sqlite FTS, or anything like it.** A vault has hundreds to a few thousand entries. After unlock they are already in memory. A linear scan over a flat slice of structs is microseconds — genuinely faster than any index lookup once you count the indirection, and with none of the cache-invalidation bugs. Reaching for an index here is the classic instinct that makes the program slower and more fragile at once.

What actually matters for latency:

- At unlock, flatten every source into one `[]Match` — source name, qualified path, title, username, url, tags — with lowercased copies precomputed **once**. Never lowercase or allocate inside the filter loop.
- Filter synchronously in `Update`, on every keystroke. **No debounce.** At this scale debouncing adds nothing but perceived lag. If you ever measure the filter exceeding ~5ms on a realistic corpus, *then* move it to a `tea.Cmd` — but measure first, and put the benchmark in the repo (`go test -bench`, 5000 synthetic entries).
- Search covers **all sources at once**, always. Results from four vaults interleave in one ranked list.

### Simple: no mode switch

**Typing searches.** Not `/` then a mode, not a dialog. The search field is always present at the top; the tree/list is below; typing narrows it; `esc` clears and restores the full tree. There is no "search mode" to enter or leave, so there is nothing to learn.

Ranking, best to worst — a plain fuzzy match with no ranking will bury the exact hit under noise and it's the single thing that will make this feel bad:

1. Exact title match (case-insensitive)
2. Title prefix
3. Title fuzzy (`sahilm/fuzzy` — already in the dependency tree via bubbles)
4. Username
5. Folder path / tags
6. URL

Highlight the matched runes in brass. Notes are **not** searched by default — they're long, noisy, and often hold secondary secrets; make it an explicit opt-in modifier.

### The fast path

`enter` on a result **copies the password immediately** and starts the countdown (§7). This is the whole point of the program; do not make me open a detail view first to get to the thing I came for. `tab` or `→` opens the detail view for when I need the username or notes.

Because `enter` acts without confirmation, the confirmation line must show the **fully qualified target** — `client-a/Infra/db-prod` — so a wrong-source copy is visible the instant it happens, while the countdown is still running and I can still fix it. This is why the source badge is non-droppable (§2a).

### The CLI uses the same matcher

`harmos get <query>` must rank identically to the TUI — same code path, not a reimplementation. If the query is ambiguous, **print the candidates and exit non-zero**. Never guess in a scriptable command; a script that silently gets the staging password is worse than one that fails.

## 9. Security requirements



These are requirements, not suggestions:

- **Never** log, print, or `%v` a password, token, or master password. Add a `String()` on any secret-carrying type that returns `[redacted]`.
- Cache file and config: mode `0600`. Config directory `0700`.
- Master password and OAuth token: never written to the config file. Token → OS keyring.
- Clipboard cleared after N seconds (configurable, default 30). Clear on exit too. If we wrote the clipboard and something else has since overwritten it, don't clobber the new value.
- Password never rendered in the list view, only in the detail view behind an explicit reveal.
- TLS verification **on** by default. For an internal CA, support pointing at a CA bundle — do **not** add a blanket `--insecure` as the ergonomic path.
- Be honest in comments about memory zeroing: Go's GC and string immutability mean you cannot reliably wipe secrets from memory. Use `[]byte` for secrets where practical, zero them best-effort, and don't claim more than that in the README.

### The clipboard needs more than a timer

"Clear after 30 seconds" does not address the actual threat, which is that the password is already somewhere else before the timer fires. Clipboard managers (Klipper, Windows clipboard history, Maccy) capture it on write. macOS **Universal Clipboard** syncs it over iCloud to my phone — a production credential, on a different device, silently.

Every serious password manager opts out of this, and there are platform conventions for it:

| Platform | Marker |
|---|---|
| macOS | `org.nspasteboard.ConcealedType` |
| KDE / Klipper | `x-kde-passwordManagerHint: secret` |
| Windows | `ExcludeClipboardContentFromMonitorProcessing` (and `CanIncludeInClipboardHistory`) |
| GNOME / wl-clipboard | no standard; document the gap honestly |

`atotto/clipboard` cannot set these — it shells out to `pbcopy`/`xclip` and has no concept of pasteboard types. There is no Go library that does this properly, so write it yourself, per platform, in `internal/clip`:

- **macOS** — requires **cgo**: `NSPasteboard`, `setString:forType:` with `org.nspasteboard.ConcealedType`. One `//go:build darwin` file. This is the cgo cost from §4a; keep it quarantined so the Linux and Windows builds stay pure Go and cross-compile normally. goreleaser must build darwin on a macOS runner.
- **Windows** — **no cgo needed**: `golang.org/x/sys/windows`, register the `ExcludeClipboardContentFromMonitorProcessing` clipboard format and write an empty entry after the text.
- **Linux/KDE** — **no cgo needed**: shell out with an explicit MIME target (`xclip -t x-kde-passwordManagerHint`, or `wl-copy -t`).
- **GNOME / plain Wayland** — no convention exists. Say so in the README plainly, don't bury it.

Three reference implementations already solve this and are worth reading before writing a line: **`blindcopy`** (Rust, BSD-3, does all three conventions, explicitly modelled on KeePassXC), **`arboard`** (Rust, maintained by 1Password, has `exclude_from_history()`), and **KeePassXC's own `Clipboard.cpp`**. Port the behaviour, don't reinvent the research.

### Honor the server's `Expiry`

The offline package carries a server-issued `Expiry` (§5). The docs example shows year 9999, but a real admin may set 7 days. If they did, **they expressed a policy**, and refusing to use an expired cache is not an inconvenience — it's the entire difference between a compliant client and an exfiltration script that happens to have a nice TUI. Enforce it: expired cache means `sync` or fail, with a message that says so.

### Stale cache can lock me out — warn before it does

This is the read-only cache's real failure mode, and it isn't "I saw the wrong value." If a password rotated on the server and I copy the old one, a few retries **lock my account** — possibly a domain account. The cache age is already permanent in the status line (§7); that's not enough for the scriptable path. `harmos get` must warn on stderr when the cache is older than a configurable threshold, and `harmos status` must make age impossible to miss. Warn on stderr, not stdout, so pipelines still work.

## 10. CLI surface

Binary and module name: **`harmos`**. It's a coined word, not an acronym, so don't try to expand it or "fix" the capitalisation — it's always lowercase `harmos`. It comes from the Greek ἁρμός (*harmós*), "a joint — the place where two fitted parts meet." The reference is the *tessera hospitalis*: a token broken in two, each party keeping half, the fit between them proving the bond. That's the tool's model of authentication — it carries one half of a credential to a system that holds the other. The README opens with that one line and never needs to explain the name again.

The name deliberately carries no vendor or product term, and deliberately isn't in the lock/key/password vocabulary — that space is a graveyard where every name either collides or implies a false affiliation (`keepass-tui`, `pleasant-cli`, `keyring`, and half a dozen dead candidates all failed there). A coined word can't collide on meaning because it has none; like `redis` or `nginx`, it earns its associations rather than borrowing them. Do not rename it to something "clearer" — clarity lives in the README's first line, not the binary name.

**Verify the name is free** before baking it into the module path: pkg.go.dev, and the Debian/Ubuntu/Fedora/Homebrew/AUR package lists. GitHub and the general namespace were already checked and are clear, but two earlier candidates died at exactly this step, so finish the job.

```
harmos init                  # create a profile interactively
harmos profile ls|add|rm
harmos sync [profile]        # pull OfflinePackage → local kdbx cache
harmos                       # launch the TUI (default command)
harmos ls [profile]          # list entries, scriptable
harmos get <query>           # print or copy a password; for scripts
harmos status                # profiles, cache age, package expiry
```

Config at `$XDG_CONFIG_HOME/harmos/config.toml`. Profile names must be unique; validate at load. `--profile` and `HARMOS_PROFILE` select a subset where a command takes one.

```toml
default = "work"
clipboard_timeout = "30s"

[[profile]]
name  = "work"
type  = "pleasant"
url   = "https://..."
user  = "..."
cache = "~/.local/share/harmos/work.kdbx"
# ca_bundle = "~/.certs/internal-ca.pem"

[[profile]]
name  = "client-a"
type  = "pleasant"
url   = "https://..."
user  = "..."
cache = "~/.local/share/harmos/client-a.kdbx"

[[profile]]
name = "personal"
type = "kdbx"
path = "~/vaults/personal.kdbx"      # read-only, never written
keyfile = "~/.keys/personal.key"

[[profile]]
name = "archive"
type = "kdbx"
path = "~/vaults/old.kdbx"
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
  tui/
    model.go styles.go views/
  clip/
  secret/           # redacting types
```

## 11. Repo hygiene (MIT, public GitHub)

- `LICENSE` — MIT.
- `README.md` — what it is, install, config, a screenshot/VHS gif, and an explicit **"not affiliated with or endorsed by Pleasant Solutions or the KeePass project"** notice. "Pleasant Password Server" and "KeePass" are third-party trademarks; use them only nominatively, to describe compatibility.
- **No internal hostnames, usernames, org names, or real data anywhere in the repo or its history.** Test fixtures are synthetic. Check this before the first commit, not after.
- `.gitignore` covering config, `*.kdbx`, and any local dumps.
- `SECURITY.md` with a disclosure address, and an honest "this has not been audited" line.
- CI: GitHub Actions — `go vet`, `golangci-lint`, `go test`, build on linux/macos/windows.
- `goreleaser` for binaries.

## 12. Testing

- The mapper is where the bugs will live. Table-driven tests, synthetic fixtures.
- Fake the Pleasant server with `httptest` + golden JSON files derived from the shapes in §5 (sanitized, invented data).
- kdbx round-trip: map → write → read → assert equality, including attachments and custom fields.
- Explicitly test: empty tree, deeply nested folders, an entry with no password, an entry with several attachments, unicode names, an expired entry.

## 13. Verify before you build

> This section is executed separately, first, as a throwaway spike — see `harmos-poc.md`. Its output is a `FINDINGS.md` and a set of sanitized fixtures, and this brief gets corrected against them before the real build starts. If you are reading this brief and no FINDINGS.md exists yet, go do the PoC instead.

My server is **9.3.3**, released July 2026. The documentation I have covers **API v6**, and a **v7** exists that I know nothing about. Before writing the client:

1. `GetServerInfo` — what's the real version and API version?
2. Does `/api/v7/rest/...` respond? What changed from v6?
3. `Authorization: Bearer <token>` or bare `<token>`?
4. Is 2FA required on my account?
5. Does `IsOfflineAvailable` return true? Does `IsCommentRequired`?
6. Fetch one real OfflinePackage and diff its actual shape against §5 — then **sanitize it into a fixture and never commit the original.**

Report what you find and flag anything that contradicts this brief before you start building. If §5 turns out to be wrong, the brief is wrong, not the server.

## 14. A decision I want surfaced, not made

Argon2 costs ~1s per unlock. In the TUI that's paid once per session and nobody cares. In a script, **every** `harmos get` pays it, and that will be intolerable — this is precisely why `rbw` runs a background agent.

Do not reach for an agent reflexively. It's a large security surface (a daemon holding decrypted keys, with an IPC socket to get the authz right) and a big jump in complexity for a v1. But also don't just let `get` be slow and hope I don't notice.

There is a third option worth weighing: the cache is a **derived artifact**, not my primary vault, so it doesn't have to inherit the server's KDF parameters. Deliberately lower Argon2 cost on the cache — with the reasoning written down and a threat model that says what it does and doesn't give up — is defensible in a way that "we never thought about it" is not.

Measure the real cost first, present the three options with the tradeoffs, and let me pick. The failure I'm guarding against is arriving at an agent by accident.

## 15. Explicitly later — do not build these now

Noting them so they're not reinvented, and so nobody starts them in v1:

- **`git-credential-harmos`** — once the matcher from §8b exists, a git credential helper is roughly fifty lines and I'd use it daily. It is not v1.
- **TOTP** — if Pleasant stores TOTP seeds (the PoC checks this), mapping them to KeePassXC's `otp` field convention gives working 2FA codes in the cache almost for free. Attractive, still not v1.

---

## A note on the environment

This is a corporate password server. The offline package pulls every credential I can see, in cleartext, onto local disk — that's what the endpoint is for, and the server admin can turn it off (`IsOfflineAvailable`) and can require a logged justification (`AskForCommentOnViewOffline`). Assume every offline fetch is audited. Don't add anything that fetches the package on a timer or in the background without me asking; sync is an explicit, user-initiated action.

There's a security team here, and they will eventually notice a client they don't recognize pulling the full package. The good version of that conversation happens before it starts, with me showing them an MIT-licensed, auditable, read-only tool that honors `Expiry` and `IsOfflineAvailable`; the bad version happens afterwards. That's why the constraints in §9 aren't decoration — they're the argument. Build so that reading the source is a sufficient answer.


═══════════════════════════════════════════════════════════════════════════════
# SECTION 4 — WORKFLOW (milestones, branches, gates)
═══════════════════════════════════════════════════════════════════════════════

# Working agreement

How we build this. The first half is project-independent and belongs in `AGENTS.md`; the milestone table at the end is specific to `harmos`.

---

## What a milestone is

**A milestone is something I would ship if you stopped there.**

That's the whole test. "Build the theme layer" is not a milestone, it's a phase — I can't ship a theme layer. "`harmos sync` produces a kdbx that KeePassXC opens" is a milestone: if you vanished afterwards, I'd still have a useful tool.

This rule exists because it's the only defence against the way projects like this actually die — not abandoned at 10%, abandoned at 95%, with six half-finished phases and nothing shippable. Phases produce that. Milestones can't.

Consequences:

- **One milestone = one branch = one PR.** Not one branch per file, not one giant branch to the end.
- **Milestones are ordered by dependency, not by fun.** The interesting part is usually the last thing that's needed and the first thing you want to do.
- **Don't start the next one because the current is "basically done."** Basically done is the state everything is in when a project dies.

## Merging is mechanical, not a vibe

A branch merges when CI is green **and** the milestone's own acceptance line is true. Every milestone below has one. If you can't write an acceptance line for a milestone, it isn't a milestone yet — say so instead of guessing.

I approve merges. Open the PR, report CI status and the acceptance line, and wait. Don't self-merge.

## Use the oracle

This project is a mirror of something authoritative: **KeePassXC defines what a correct kdbx is.** Don't invent a definition of correctness when an external one exists.

So the most valuable test here isn't a unit test you designed — it's a **differential test against the oracle**: generate a kdbx, then have `keepassxc-cli` open, list, and export it, and compare against the input. That single test catches the failure mode that actually matters (a corrupt or subtly wrong cache) in a way that a hundred hand-written assertions won't.

Where the spec says "open it in KeePassXC and look" (§12), automate it instead. Manual verification doesn't run on every push.

## Stop-and-wait gates

Three points where you finish and **wait for me**, rather than continuing:

1. **After the PoC (M1)** — I decide whether the spec survives contact with the real server.
2. **After M2b** — I'm going to use `harmos sync` with KeePassXC for a few weeks before any TUI exists. This is deliberate (spec §1). Do not start M3 to be helpful.
3. **After M4** — the design phase produces options, not decisions. Present the three artifacts and wait for my picks (default theme, breakpoints, column-drop order). Do not begin M5's TUI code against a guessed design.

These are judgement gates, not CI gates. Green tests don't clear them.

---

## Milestones for harmos

Four milestones, split by function. Each is still "something I'd ship if you stopped there."

| | Milestone | Acceptance |
|---|---|---|
| **M1** | **PoC** — verify the server download (`harmos-poc.md`). Throwaway, scratch dir, **no repo, no remote.** | `FINDINGS.md` answers every question in it. **Stop and wait.** |
| **M2** | **Server side** — repo + pipelines, then the whole Pleasant path: auth, `OfflinePackage`, mapper, `harmos sync` → kdbx. | `keepassxc-cli` opens the produced cache in CI. **Stop and wait.** |
| **M3** | **Local kdbx side** — read external `.kdbx` files as sources. | An external file is browsable read-only and its bytes are provably unchanged after a session. |
| **M4** | **TUI design phase** — paper only, no lipgloss code. Claude Code generates options against the *real* cache from M2/M3; I choose. | Three signed-off artifacts exist (below). **Stop and wait for my choices.** |
| **M5** | **The shared surface** — matcher, `harmos ls`/`get`, clipboard, then the TUI, over both source types at once. | Global search spans a Pleasant cache and a local file; the source badge distinguishes them; TUI renders correctly across the breakpoint set. |

**A note on M5, because the wording invites a trap.** M5 is *not* "merge the two logics together." The whole point of the architecture (spec §2) is that there are never two logics to merge: Pleasant and local kdbx are **two producers feeding one reader**. M2 and M3 both output the same thing — a kdbx that the one vault reader understands. So M5 doesn't fuse anything; it builds the surface (matcher, clipboard, TUI) *on top of* the reader that M2 and M3 already fed. If M5 turns out to need real merging work, that's the signal that M2 or M3 built its own private reader and violated §2 — stop and fix that, don't paper over it in M5.

### M2 splits into two branches

M2 is the biggest milestone; do it as two PRs, not one:

- **`m2a-scaffold`** — repo, licence, pipelines, no features. Acceptance: CI green on a repo that does nothing. Details below; this is the only cheap time to get the cgo runner and secret-scanning right.
- **`m2b-sync`** — the Pleasant path end to end. Acceptance: `keepassxc-cli` opens the cache in CI.

### M2a — repo and pipelines

Create the GitHub repo, public, MIT. Issues:

1. `.gitignore` **before** anything else exists — `*.kdbx`, config, scratch, dumps, `FINDINGS.md`
2. `LICENSE` (MIT), `NOTICE`, the non-affiliation line, `SECURITY.md` with an honest "not audited"
3. `README.md` skeleton — what it is, status: pre-alpha, no install instructions yet (they land with the release that needs them)
4. `go.mod`, module path — **and verify `gokeepasslib`'s licence before it goes in** (§4a)
5. CI: `go vet`, `golangci-lint`, `go test ./...`
6. CI: build matrix, linux + windows + macOS
7. **CI: prove the cgo path on a real macOS runner with a trivial `//go:build darwin` stub.** Do this now, at M1 cost, so M5 doesn't discover it. This is the whole reason M1 exists as a separate milestone.
8. CI: `gitleaks` on every push **and** across full history
9. `goreleaser` config, wired but tagging nothing

**Acceptance:** CI green, and the macOS runner compiles the cgo stub.

### M2b — the Pleasant path (`harmos sync`)

**No TUI. No `bubbletea` in `go.mod`.** One Pleasant profile, hardcoded-simple config. Issues:

1. `internal/secret` — redacting types whose `String()` returns `[redacted]`. First, before anything touches a password.
2. Config: minimal TOML, one profile, `0600`
3. Pleasant client: `/OAuth2/Token`, whichever auth header form M1 established
4. Pleasant client: `OfflinePackage` fetch, honouring `IsOfflineAvailable` and `IsCommentRequired`
5. Test harness: `httptest` + the sanitized fixtures from M1
6. Mapper: folders → groups, credentials → entries
7. Mapper: attachments — flat list joined by `CredentialObjectId` → per-entry binaries
8. Mapper: GUIDs — server ID verbatim into `pps.Id`, deterministic UUID derived, **not a naive 16-byte copy** (§6)
9. kdbx writer: KDBX4, master password, `0600`
10. **Enforce the package `Expiry`** (§9) — refuse an expired cache
11. `harmos sync` command
12. **Differential test: `keepassxc-cli` in CI** — the oracle, automated
13. Edge cases from M1's `FINDINGS.md`: unicode names, empty password, duplicate names in one folder, multi-attachment entries, expired entries

**Acceptance:** `keepassxc-cli` opens the produced cache in CI and lists the expected entries.

Then **stop.** I'm going to use this with KeePassXC for a few weeks. Don't start M3.

---

## M2a in detail — create the repo properly

These are cheap now and expensive forever after. This is a password tool; the history is public and permanent.

**Before the first commit:**

- **`.gitignore` first.** Before `git init` produces anything. `*.kdbx`, config, scratch, dumps, `FINDINGS.md` if it contains anything real.
- **Never `git add .`** in this repo. Ever. Explicit paths only. One stray offline package in the history and the repo is burned — you cannot un-publish it.
- **The name check** must have come back clean (`harmos-poc.md`).
- `LICENSE` (MIT), `NOTICE`, and the "not affiliated with or endorsed by Pleasant Solutions or the KeePass project" line — from commit one, not retrofitted.
- Verify `gokeepasslib`'s licence is compatible before it enters `go.mod` (spec §4a).

**Branch naming:** `m2-sync`, `m3-multisource`, `m4-matcher`. Number them; the order is the argument.

**Attribution: none.** Do not add yourself as a contributor anywhere. Specifically:

- **No commit trailers** — no `Co-Authored-By: Claude`, no "🤖 Generated with Claude Code", no tool-attribution line. Commits are authored by me, full stop. If a global git config or a default template adds these, strip them; check `git log -1 --format='%b'` on the first commit and confirm it's clean before making a second.
- **No PR/issue signatures** — no "generated by" footer in PR descriptions or issue bodies.
- **The author and committer** on every commit are my name and email, not a tool identity.
- This isn't about hiding anything — the repo is MIT and the README can say however it was built. It's that the git history's `Contributors` list should reflect people, not tools, and GitHub's contributor graph shouldn't attribute the work to an assistant.

## M2a in detail — the pipelines

Standard, and one that isn't:

- `go vet`, `golangci-lint`, `go test ./...`
- Build matrix: linux, windows, macOS. **macOS needs a real macOS runner** — the clipboard work is cgo (spec §4a), so `CGO_ENABLED=0` cross-compilation won't cover darwin. Discover this at M1, not at M5.
- **A secret scanner — `gitleaks` or equivalent — on every push and on the full history.** This is not optional in this repo. If a real dump or token ever lands in a commit, CI must scream on the push that did it, while the branch can still be deleted.
- `goreleaser` on tag. Wire it at M1, tag nothing until M2.
- Codecov or equivalent if you like, but coverage is not an acceptance line for anything here.

**CI never touches the real server.** No credentials in Actions secrets, no live Pleasant calls. Every test runs against `httptest` and the sanitized fixtures from the PoC. If a test needs the real server, it's the wrong test.

### M4 — the TUI design phase

This exists as its own milestone for one reason: **the TUI is the only part with no oracle.** M2 is validated by `keepassxc-cli`, M3 by file bytes, M5's search by the matcher — all mechanical. The TUI has nothing to diff against, so "done" is taste unless it's pinned down first. This phase makes me the oracle, *before* code exists rather than after.

It runs between M3 and M5 deliberately, so it designs against the **real** cache — my actual folder names, real tree depth, the genuinely longest entry name — not the invented data the early mockups used. The test that matters is whether *my* six `admin` entries fit at 80 columns, and that can't be known until M2/M3 have produced a real vault.

**Step zero, before any mockup: inventory what the toolkit already does.** Don't design in a vacuum and then hand-build what `bubbles` ships for free. Read what's actually available — `list` (with built-in fuzzy filtering), `table`, `viewport`, `progress` (with `WithGradient`), `help`, `spinner`, `textinput` — and design *onto* those components rather than around them. Two rules make this bite:

- **Read the version that's actually pinned.** Charm moves fast; component APIs and capabilities differ across tags, and my other project pins a specific one. "What lipgloss/bubbles can do" is version-specific — take it from the `go.mod` version's own docs and source, not from memory or a blog post. `lipgloss.Table`, for instance, only appeared at a certain version; assuming it exists is how you design something that won't compile.
- **Respect each component's grain.** `list` and `table` have opinions about what a row is, where the filter lives, how paging works. A mockup that fights those means either wrestling the component (fragile) or forking it. Design with the grain — or decide deliberately, in writing, that a given surface needs a custom widget and why. The countdown bar is a likely custom case; the results list almost certainly is not.

Then the three artifacts:

1. **A signed-off static mockup set** — a few themes rendered against real data, from which I pick the default (charm vs brass), the exact breakpoints, and the column-drop order.
2. **A filled-in design-token table** — not "brass" but the actual `AdaptiveColor` values for light/dark terminals plus the 16-colour fallback. This is what makes spec §7 executable, and once it exists theming is free: swap the tokens, get a theme.
3. **An interaction contract** — the state-machine diagram (`unlock → syncing → browsing → detail`) and the full keymap. This is the part `teatest` verifies in M5; it tests the contract, not the colours.

**What this phase must not do:** write lipgloss code. It ends on paper. The danger is a design phase becoming endless polishing — walking straight into the spec §1 failure. So it's bounded: three artifacts, my sign-off, done. Code is M5.

**Only M1 and M2 are specified in detail below, and that's deliberate.** M1's findings will change what comes after — if `keepassxc-cli` can't do what I assume, or `Expiry` arrives in days, or the corpus is 40k entries instead of 400, then M3–M5 shift. Writing their issues now is planning fiction. They get written after `FINDINGS.md`.

Inside M5 there's an ordering rule worth stating: **the matcher and the headless `harmos ls` / `harmos get` come before the TUI**, because the CLI and the TUI share one matcher (§8b) — build it where it's testable without a terminal. And **the clipboard-with-concealment work (§9, §4a) comes before the TUI's countdown**, since the countdown is meaningless if the clipboard write itself leaks.


═══════════════════════════════════════════════════════════════════════════════
# SECTION 5 — AGENTS.md (repo root contract, installed at M2a)
═══════════════════════════════════════════════════════════════════════════════

# DOX framework

- DOX is a highly performant AGENTS.md hierarchy installed here
- Agent must follow DOX instructions across any edits

## Core Contract

- AGENTS.md files are binding work contracts for their subtrees
- Work products, source materials, instructions, records, assets, and durable docs must stay understandable from the nearest applicable AGENTS.md plus every parent AGENTS.md above it

## Read Before Editing

1. Read the root AGENTS.md
2. Identify every file or folder you expect to touch
3. Walk from the repository root to each target path
4. Read every AGENTS.md found along each route
5. If a parent AGENTS.md lists a child AGENTS.md whose scope contains the path, read that child and continue from there
6. Use the nearest AGENTS.md as the local contract and parent docs for repo-wide rules
7. If docs conflict, the closer doc controls local work details, but no child doc may weaken DOX

Do not rely on memory. Re-read the applicable DOX chain in the current session before editing.

## Update After Editing

Every meaningful change requires a DOX pass before the task is done.

Update the closest owning AGENTS.md when a change affects:

- purpose, scope, ownership, or responsibilities
- durable structure, contracts, workflows, or operating rules
- required inputs, outputs, permissions, constraints, side effects, or artifacts
- user preferences about behavior, communication, process, organization, or quality
- AGENTS.md creation, deletion, move, rename, or index contents

Update parent docs when parent-level structure, ownership, workflow, or child index changes. Update child docs when parent changes alter local rules. Remove stale or contradictory text immediately. Small edits that do not change behavior or contracts may leave docs unchanged, but the DOX pass still must happen.

## Hierarchy

- Root AGENTS.md is the DOX rail: project-wide instructions, global preferences, durable workflow rules, and the top-level Child DOX Index
- Child AGENTS.md files own domain-specific instructions and their own Child DOX Index
- Each parent explains what its direct children cover and what stays owned by the parent
- The closer a doc is to the work, the more specific and practical it must be

## Child Doc Shape

- Create a child AGENTS.md when a folder becomes a durable boundary with its own purpose, rules, responsibilities, workflow, materials, or quality standards
- Work Guidance must reflect the current standards of the project or user instructions; if there are no specific standards or instructions yet, leave it empty
- Verification must reflect an existing check; if no verification framework exists yet, leave it empty and update it when one exists

Default section order: Purpose, Ownership, Local Contracts, Work Guidance, Verification, Child DOX Index

## Style

- Keep docs concise, current, and operational
- Document stable contracts, not diary entries
- Put broad rules in parent docs and concrete details in child docs
- Prefer direct bullets with explicit names
- Do not duplicate rules across many files unless each scope needs a local version
- Delete stale notes instead of explaining history
- Trim obvious statements, repeated rules, misplaced detail, and warnings for risks that no longer exist

## Closeout

1. Re-check changed paths against the DOX chain
2. Update nearest owning docs and any affected parents or children
3. Refresh every affected Child DOX Index
4. Remove stale or contradictory text
5. Run existing verification when relevant
6. Report any docs intentionally left unchanged and why

## User Preferences

- Respond in Hungarian. Code, identifiers, comments, commit messages and docs stay in English.
- No commit, PR, or generated file may credit Claude or any AI tool as author or co-author. No `Co-Authored-By` or "Generated with" trailers. Author and committer are the human on every commit.
- Pause for review after a milestone rather than running the whole brief end to end. The stop-and-wait gates in `docs/harmos-workflow.md` are hard stops.

---

# Project: harmos

## Purpose

harmos is a read-only terminal client for browsing and copying passwords from **Pleasant Password Server** (a.k.a. KeePass Hub) and from local `.kdbx` files. It syncs a Pleasant server's offline package into a local kdbx cache, and reads that cache and any local kdbx files through one shared vault reader, presented in a Bubble Tea TUI and scriptable CLI commands.

The name is coined (Greek ἁρμός, "the joint where two fitted parts meet"); it is not an acronym and stays lowercase. Not affiliated with or endorsed by Pleasant Solutions or the KeePass project.

The full brief is `docs/harmos-spec.md`. The PoC that must run first is `docs/harmos-poc.md`. The milestone plan and working agreement is `docs/harmos-workflow.md`. The visual source of truth is `docs/design/` (produced in the M4 design phase).

## Ownership

harmos is not a fork — there is no upstream engine and no three-branch sync model. Every file is ours. The important boundary here is not upstream-vs-ours but **the one shared reader vs. its two producers** (spec §2):

- `internal/vault/` — the single kdbx reader. Everything downstream (search, TUI, CLI) goes through it. It knows nothing about Pleasant.
- `internal/source/pleasant/` — the only package that knows Pleasant exists: API client + mapper. Produces a kdbx cache.
- `internal/source/localkdbx/` — reads external `.kdbx` files, **read-only**, as sources.

If two readers ever appear, that is an architecture violation, not a feature. There is one reader; the sources feed it.

## Local Contracts

- **Read-only, v1.** No writes to the Pleasant server; no writes, no `.lock` files, no timestamp rewrites to external kdbx files. Opening a user's own vault must leave its bytes and mtime unchanged — this is an invariant, tested.
- **Two producers, one reader.** Never build a general vault-abstraction interface with parallel Pleasant and kdbx implementations. The kdbx format is already the superset; both producers emit kdbx, the reader consumes kdbx.
- **The cache is encrypted at rest.** kdbx with a single harmos master password chosen at `init`; openable in KeePassXC. Never store the master password or OAuth token in the config file — token goes to the OS keyring.
- **Secrets never leak to logs.** Any secret-carrying type has a `String()` returning `[redacted]`. Never `%v` a password, token, or master password.
- **Honor the server.** Respect the offline package `Expiry`; check `IsOfflineAvailable` and `IsCommentRequired`. Every offline fetch is assumed audited; sync is always explicit and user-initiated, never on a timer or in the background.
- **Clipboard is concealed.** Copied passwords must set the platform's "do not record / do not sync" pasteboard hints (spec §9); a plain timeout is not enough.
- **License.** MIT. `NOTICE` states the non-affiliation. Third-party trademarks ("Pleasant Password Server", "KeePass") used nominatively only.
- **Themes, not hardcoded colors.** Every visual token lives in a theme; the default theme is chosen in M4, not hardcoded in the render path. Non-color cues accompany every color-coded state so meaning survives `mono`, `NO_COLOR`, and colorblindness.
- **Language is Go, and the reason is load-bearing** (spec §4a): only Go has a mature, permissively-licensed kdbx *writer*. Do not re-litigate this; the accepted costs (cgo for macOS clipboard, best-effort memory zeroing) are documented, not open questions.

## Global Workflow

- **Milestones and gates** live in `docs/harmos-workflow.md` and govern all work. A milestone is something shippable if stopped there; one milestone = one branch = one PR; merge on green CI plus the milestone's written acceptance line; the human approves merges.
- **Stop-and-wait gates are hard stops**: after the PoC, after M2b, and after the M4 design phase. Do not continue past them to be helpful.
- **The PoC runs first, outside any repo** — scratch dir, no remote. Its `FINDINGS.md` corrects the spec before the real build starts.
- **Use the oracle.** KeePassXC defines a correct kdbx; validate the cache with `keepassxc-cli` in CI rather than hand-written assertions. Where a spec says "open it and look", automate it.
- **Repo hygiene.** `.gitignore` before `git init` produces anything. Never `git add .` — explicit paths only; one stray offline package in history burns the repo. `gitleaks` on every push and across full history. CI never touches the real server: tests run against `httptest` and sanitized PoC fixtures, no live credentials in Actions.
- **Branches and commits.** Every change lands on a prefixed branch and a PR, never straight to `main`: `feat/ fix/ chore/ docs/ refactor/ test/ perf/ ci/ build/` + a short slug. Conventional Commits (`type: summary`); the body explains *why*, not what. `main` is protected: PR required, CI green, no force-push, no deletion.
- **Test/vet before done.** `go test ./...` and `go vet ./...` before considering any change complete.
- **Versioning.** SemVer `vX.Y.Z`; GoReleaser owns releases on a pushed tag. Tag only when the tree is release-ready.

## Work Guidance

- Ship vertical slices: each PR builds, runs, and is reviewable on its own.
- Ship the boring half first (`harmos sync` before any TUI); the fun part is the last thing needed and the first thing you'll want to start. Resist.
- Layout is recomputed from `tea.WindowSizeMsg` in the view; never hardcode a width, column position, or bar length; never panic on a tiny terminal. Measure display width with `charmbracelet/x/ansi` or `go-runewidth`, never `len()`.
- The matcher is shared by the TUI and the headless CLI — build it where it's testable without a terminal, before the TUI.
- Before designing the TUI, inventory what `bubbles`/`lipgloss` already provide *at the pinned version*, and design onto those components rather than reimplementing them.

## Verification

Populate as the framework lands (M2a). Expected shape:

```
go test ./...        # unit + mapper + differential tests, all green
go vet ./...
golangci-lint run    # config .golangci.yml
```

Oracle / differential check: generate a cache and confirm `keepassxc-cli` opens and lists it, in CI. Invariant check: an external kdbx source is byte- and mtime-unchanged after a full browse session.

## Child DOX Index

Populated as the tree grows; none exist yet at M1. Expected children:

- `internal/vault/AGENTS.md` — the single kdbx reader.
- `internal/source/pleasant/AGENTS.md` — Pleasant API client + mapper (the only Pleasant-aware package).
- `internal/source/localkdbx/AGENTS.md` — read-only external kdbx sources.
- `internal/tui/AGENTS.md` — the Bubble Tea interface.
- `internal/theme/AGENTS.md` — color tokens and themes.
- `internal/clip/AGENTS.md` — per-platform concealed clipboard (the cgo boundary).
- `internal/secret/AGENTS.md` — redacting types.
- `internal/config/AGENTS.md` — profiles and config loading.
- `docs/AGENTS.md` — the brief, PoC, workflow, and design mocks.
- `scripts/AGENTS.md` — the PoC dump sanitizer and operational tooling.
