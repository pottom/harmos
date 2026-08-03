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

Each Pleasant cache is a KDBX4 file encrypted with ChaCha20-256, keyed from a
**composite key** via Argon2d: your harmos master password *and* a random
per-source keyfile that lives apart from the cache — `$XDG_CONFIG_HOME/harmos/`
`<name>.key`, mode 0600, while the cache is under `$XDG_DATA_HOME`. A cache
copied off the machine cannot be opened with the master password alone.

The cache is a re-syncable derived artifact, so the KDF cost is a deliberate middle ground (spec §14): **19 MiB memory, 2
iterations** — the OWASP Argon2 baseline, far dearer to brute-force than a
library default, while keeping unlock well under a second.

## Honest limitations

- **Memory zeroing is best-effort only.** Go's garbage collector may move
  objects and its strings are immutable, so secrets cannot be reliably wiped
  from memory. Secrets are held in `[]byte` behind a type that redacts itself
  through every formatting path, and zeroed where ownership of the buffer is
  unambiguous — a downloaded Pleasant package, for instance, is zeroed as soon
  as it has been mapped. The master password is **not** zeroed: it is kept for
  the length of the session, because a sync or a re-unlock needs it. No stronger
  claim is made.
- **A Pleasant offline package never touches a filesystem.** It is every
  credential on the server in the clear; it is downloaded into memory, mapped,
  and zeroed. It used to be fetched to a `0600` temp file beside the cache and
  deleted afterwards, which left it on disk for the length of the mapping and
  the write, and left it there entirely if the process was killed.
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
