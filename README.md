# harmos

> ἁρμός (*harmós*) — a joint: the place where two fitted parts meet.

A read-only terminal password client for **Pleasant Password Server**
(a.k.a. KeePass Hub) and local `.kdbx` files. It syncs a Pleasant server's
offline package into a local, encrypted kdbx cache and reads that cache — plus
any local kdbx files — through one shared reader, in a TUI and scriptable CLI.

**Status: pre-alpha.** Nothing to install yet; this repository is currently the
scaffold and the design. Install instructions land with the release that needs
them.

## What it is

- **Read-only.** Browse, search, copy. No writing back to the server, no entry
  editing, no bidirectional sync.
- **Two producers, one reader.** A Pleasant server and a local kdbx file both
  feed the same kdbx-based reader; the cache is a real KDBX4 file (Argon2,
  ChaCha20) openable in KeePassXC.
- **Provenance first.** Every entry shows which source it came from, always.

## Not affiliated

Not affiliated with or endorsed by Pleasant Solutions or the KeePass project.
"Pleasant Password Server" and "KeePass" are third-party trademarks, used only
nominatively to describe compatibility. See `NOTICE`.

## Security

Not yet audited. See `SECURITY.md`. Licensed MIT — see `LICENSE`.
