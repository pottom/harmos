# Security Policy

## Reporting a vulnerability

Please report security issues privately by email to **basipottom@gmail.com**.
Do not open a public issue for a suspected vulnerability.

Include what you found, how to reproduce it, and the impact you expect. You
will get an acknowledgement as soon as possible.

## Status — not audited

**harmos has not been independently security-audited.** It is pre-alpha
software. Read the source before trusting it with credentials; that is a
deliberate design goal (the codebase is meant to be small and auditable).

## At-rest protection of the cache

Each Pleasant cache is a KDBX4 file encrypted with ChaCha20-256, keyed from your
harmos master password via Argon2d. The cache is a re-syncable derived artifact,
so the KDF cost is a deliberate middle ground (spec §14): **19 MiB memory, 2
iterations** — the OWASP Argon2 baseline, far dearer to brute-force than a
library default, while keeping unlock well under a second.

## Honest limitations

- **Memory zeroing is best-effort only.** Go's garbage collector may move
  objects and its strings are immutable, so secrets cannot be reliably wiped
  from memory. Where practical, secrets are held in `[]byte` and zeroed on a
  best-effort basis. No stronger claim is made.
- **Never writes to a Pleasant server**, and never creates a `.lock` file
  beside any kdbx.
- **Local kdbx files are read-only by default.** Writing requires an explicit
  opt-in per source (`writable = true`, set from the interface and remembered),
  an explicit save, and a confirmation. A config without that key cannot write at
  all. Browsing — including a session that stages
  edits and does not save — leaves the file byte- and mtime-unchanged.
- **A save cannot corrupt the file it replaces.** The original is copied aside
  first, the new contents go to a temp file that is decoded back to prove it is
  readable, and only then does an atomic rename put it in place. If the file
  changed on disk since it was opened, the save is refused.
