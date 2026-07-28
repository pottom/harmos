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

Four points where you finish and **wait for me**, rather than continuing:

1. **After the PoC (M1)** — I decide whether the spec survives contact with the real server.
2. **After M2b** — I'm going to use `harmos sync` with KeePassXC for a few weeks before any TUI exists. This is deliberate (spec §1). Do not start M3 to be helpful.
3. **After M4** — the design phase produces options, not decisions. Present the three artifacts and wait for my picks (default theme, breakpoints, column-drop order). Do not begin M5's TUI code against a guessed design.
4. **After M6's PR3** — the write engine and the change model are done and proven headless, before any editing UI exists. Writing to my own vault is the highest-consequence thing harmos does; I want to exercise the engine through its tests and the oracle before a keystroke can trigger it. Do not start the TUI slices to be helpful.

These are judgement gates, not CI gates. Green tests don't clear them.

---

## Milestones for harmos

Milestones split by function. Each is still "something I'd ship if you stopped there."

| | Milestone | Acceptance |
|---|---|---|
| **M1** | **PoC** — verify the server download (`harmos-poc.md`). Throwaway, scratch dir, **no repo, no remote.** | `FINDINGS.md` answers every question in it. **Stop and wait.** |
| **M2** | **Server side** — repo + pipelines, then the whole Pleasant path: auth, `OfflinePackage`, mapper, `harmos sync` → kdbx. | `keepassxc-cli` opens the produced cache in CI. **Stop and wait.** |
| **M3** | **Local kdbx side** — read external `.kdbx` files as sources. | An external file is browsable read-only and its bytes are provably unchanged after a session. |
| **M4** | **TUI design phase** — paper only, no lipgloss code. Claude Code generates options against the *real* cache from M2/M3; I choose. | Three signed-off artifacts exist (below). **Stop and wait for my choices.** |
| **M5** | **The shared surface** — matcher, `harmos ls`/`get`, clipboard, then the TUI, over both source types at once. | Global search spans a Pleasant cache and a local file; the source badge distinguishes them; TUI renders correctly across the breakpoint set. |
| **M6** | **Writable local kdbx** — create/edit/delete/move entries and folders in local `.kdbx` sources, staged and confirmed before any write. Ten PRs; see `docs/design/harmos-write-model.md`. | A source unlocked for writing takes an edit, shows it staged, and on confirmation writes a file `keepassxc-cli` opens — with the recycle bin, the history record and the tombstone all readable back. A locked source, and every Pleasant source, remains byte-unchanged. **Stop and wait after PR3.** |

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


