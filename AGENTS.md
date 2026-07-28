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

harmos is a terminal client for browsing and copying passwords from **Pleasant Password Server** (a.k.a. KeePass Hub) and from local `.kdbx` files. It is read-only against Pleasant, and read-only by default against local files — M6 added staged, confirmed editing for a local kdbx the user explicitly unlocks. It syncs a Pleasant server's offline package into a local kdbx cache, and reads that cache and any local kdbx files through one shared vault reader, presented in a Bubble Tea TUI and scriptable CLI commands.

The name is coined (Greek ἁρμός, "the joint where two fitted parts meet"); it is not an acronym and stays lowercase. Not affiliated with or endorsed by Pleasant Solutions or the KeePass project.

The full brief is `docs/harmos-spec.md`. The PoC that must run first is `docs/harmos-poc.md`. The milestone plan and working agreement is `docs/harmos-workflow.md`. The visual source of truth is `docs/design/` (produced in the M4 design phase).

## Ownership

harmos is not a fork — there is no upstream engine and no three-branch sync model. Every file is ours. The important boundary here is not upstream-vs-ours but **the one shared reader vs. its two producers** (spec §2):

- `internal/vault/` — the single kdbx reader. Everything downstream (search, TUI, CLI) goes through it. It knows nothing about Pleasant.
- `internal/source/pleasant/` — the only package that knows Pleasant exists: API client + mapper. Produces a kdbx cache.
- `internal/source/localkdbx/` — reads external `.kdbx` files as sources, read-only unless the user unlocks one for writing this run (M6).

If two readers ever appear, that is an architecture violation, not a feature. There is one reader; the sources feed it.

## Local Contracts

- **Read-only by default; writing is narrow, explicit and confirmed (M6).** No writes to the Pleasant server, ever — its cache is regenerated by `sync`, so an edit there would be silently discarded, and this is enforced structurally (no code path builds a write handle for it). A local kdbx source is read-only until the user unlocks it for the current run. **Browsing still never writes**: bytes and mtime are unchanged after a session with no explicit save, no `.lock` file is ever created, and that remains an invariant, tested. Only `vault.Handle.Save` writes, only after passing the refusals in `refuseWriteBecause` (KDBX 3.1, KDBX 4.1, several root groups, a read-only file), and every save regenerates the file's nonces — reusing them would be a ChaCha20 keystream reuse. The full model is `docs/design/harmos-write-model.md`.
- **Two producers, one reader.** Never build a general vault-abstraction interface with parallel Pleasant and kdbx implementations. The kdbx format is already the superset; both producers emit kdbx, the reader consumes kdbx.
- **The cache is encrypted at rest.** A KDBX4 file locked with a composite key — the shared harmos master password *and* a random per-source keyfile in the config dir (`<name>.key`), kept apart from the cache so a copied cache is useless with the master alone (spec §15); openable in KeePassXC with both. The master, per-source, and server passwords, and any OAuth token, go to the OS keyring — never the config file.
- **Secrets never leak to logs.** Any secret-carrying type has a `String()` returning `[redacted]`. Never `%v` a password, token, or master password.
- **Honor the server.** Respect the offline package `Expiry`; check `IsOfflineAvailable` and `IsCommentRequired`. Every offline fetch is assumed audited; sync is always explicit and user-initiated, never on a timer or in the background.
- **Clipboard is concealed.** Copied passwords must set the platform's "do not record / do not sync" pasteboard hints (spec §9); a plain timeout is not enough.
- **License.** MIT. `NOTICE` states the non-affiliation. Third-party trademarks ("Pleasant Password Server", "KeePass") used nominatively only.
- **Themes, not hardcoded colors.** Every visual token lives in `internal/theme` as a light/dark pair; `theme.Apply` rebuilds the package-level styles the TUI renders through, never hardcoded in the render path. Ten built-ins ship (charm default, brass, nord, dracula, gruvbox, solarized, tokyonight, catppuccin, rosepine, everforest); a config `theme = "<name>"` or a custom `themes/<name>.toml` selects one, and the Settings tab previews live. Non-color cues accompany every color-coded state so meaning survives `mono`, `NO_COLOR`, and colorblindness.
- **The kdbx library is patched, and that is temporary by design.** `go.mod` carries a `replace` for `github.com/tobischo/gokeepasslib/v3` pointing at our branch, which adds the KDBX 4.1 elements upstream does not model (`docs/design/kdbx-4.1-support.md`). Without it, saving a 4.1 file silently drops data — the format's minor version round-trips verbatim while the unknown elements are discarded. The patch is offered upstream and written to be merged there: match the project's style, add no unrelated refactoring, reorder no existing field. **When upstream merges it, delete the `replace`.** Until then we own its security maintenance and must track upstream releases.
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

```
go test ./...        # unit, mapper, oracle, and the teatest interaction contract
go vet ./...
golangci-lint run    # config .golangci.yml
```

Oracle check: CI generates a cache and confirms `keepassxc-cli` opens and lists it. Invariant check: an external kdbx source is byte- and mtime-unchanged after a full browse session, including one that stages edits and does not save; only an explicit `Save` may write. The TUI's state machine is pinned by a `teatest` interaction-contract test (`internal/tui/teatest_test.go`). Local builds and releases stamp their version from `git describe` / GoReleaser via `internal/version`; `make build` is the development build.

## Child DOX Index

- `internal/theme/AGENTS.md` — the color tokens and the ten built-in themes.
- `internal/tui/AGENTS.md` — the Bubble Tea interface (unlock, tabs, settings, chrome).
