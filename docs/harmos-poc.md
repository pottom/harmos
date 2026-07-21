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


