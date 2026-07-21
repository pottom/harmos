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
- **Read-only.** harmos never writes to a Pleasant server, and never writes to,
  locks, or rewrites a local `.kdbx` file.
