# harmos

> ἁρμός (*harmós*) — a joint: the place where two fitted parts meet.

A read-only terminal password client for **Pleasant Password Server**
(a.k.a. KeePass Hub) and local `.kdbx` files. It syncs a Pleasant server's
offline package into a local, encrypted kdbx cache and reads that cache — plus
any local kdbx files — through one shared reader, in a TUI and a scriptable CLI.

- **Read-only.** Browse, search, copy. No writing back to the server, no entry
  editing, no bidirectional sync. harmos never modifies your kdbx files.
- **Two producers, one reader.** A Pleasant server and a local kdbx file both
  feed the same kdbx-based reader; the cache is a real KDBX4 file (Argon2,
  ChaCha20) openable in KeePassXC.
- **Provenance first.** Every entry shows which source it came from, always.
- **Secrets stay off disk.** Passwords go to the OS keyring, never the config
  file; clipboard copies are concealed and auto-cleared after a timeout.

## Install

**Quick install** (macOS / Linux) — download the latest release, verify its
checksum, and drop `harmos` on your `PATH`:

```sh
curl -fsSL https://raw.githubusercontent.com/pottom/harmos/main/install.sh | sh
```

Set `HARMOS_VERSION` to pin a tag or `HARMOS_INSTALL_DIR` to choose where it
lands; it never invokes `sudo` on its own.

**From a release** — download the archive for your OS/arch from the
[releases page](https://github.com/pottom/harmos/releases), unpack it, and put
`harmos` on your `PATH`:

```sh
tar xzf harmos_*_darwin_arm64.tar.gz
sudo mv harmos /usr/local/bin/
```

**Debian / Ubuntu / Fedora / RHEL** — grab the `.deb` or `.rpm` for your arch
from the releases page:

```sh
sudo dpkg -i harmos_*_linux_amd64.deb   # Debian / Ubuntu
sudo rpm -i  harmos_*_linux_amd64.rpm   # Fedora / RHEL
```

**Docker** — a minimal multiarch image (distroless, ~19 MB) on the GitHub
Container Registry:

```sh
# a CLI verb
docker run --rm ghcr.io/pottom/harmos:latest gen -n 24

# the TUI (needs a TTY), with your config and cache mounted in
docker run -it --rm \
  -v ~/.config/harmos:/home/nonroot/.config/harmos \
  -v ~/.local/share/harmos:/home/nonroot/.local/share/harmos \
  ghcr.io/pottom/harmos:latest
```

**With Go** (1.26+):

```sh
go install github.com/pottom/harmos/cmd/harmos@latest
```

**Self-update** — once installed from a release, harmos updates itself in place
(checksum-verified, atomic, with rollback):

```sh
harmos update
```

The TUI header shows a yellow `⬆` marker when a newer release exists. The
background check makes one request to the GitHub releases API and nothing else;
set `HARMOS_NO_UPDATE_CHECK=1` to disable it.

## Quick start

```sh
# A local kdbx file — harmos only ever reads it.
harmos add-source ~/vault.kdbx

# …or a Pleasant Password Server, then pull its offline package into the cache.
harmos add-source --type pps --name work \
  --url https://pps.example.internal --user alice --save-password
harmos sync work

# Then browse in the TUI…
harmos

# …or script it.
harmos ls work
harmos get "aws root" --copy
```

Run `harmos` with no config and it opens the TUI on a first-run onboarding
screen that walks you through adding your first source.

## Usage

### TUI

Running `harmos` with no subcommand opens the terminal UI: a collapsible source
/ folder tree, an entry table with a live search bar, a detail pane (notes,
custom fields, dates, attachments, TOTP), and a password **Generate** tab.
Press `?` for the full key map. A few staples:

| Key | Action |
| --- | --- |
| `/` | Search (supports `field:`, `AND`/`OR`, `"phrase"`, `-exclude`) |
| `→` / `enter` | Open entry detail · `esc` / `←` to go back |
| `ctrl+y` | Copy the password (concealed, auto-cleared) |
| `ctrl+u` / `ctrl+o` | Copy the username / URL |
| `ctrl+t` | Copy the current TOTP code |
| `c` | Copy a `harmos get …` command for the selected entry |
| `s` | Save attachments (detail) · sync a source (tree) |
| `ctrl+b` | Collapse / expand the source tree |
| `1` / `2` / `3` | Switch Browse / Generate / Settings tab |
| `?` / `q` | Help · quit (clears the clipboard) |

### CLI

Every read the TUI does is also a command, for scripts and pipes:

| Command | What it does |
| --- | --- |
| `harmos ls [source]` | List entries in an aligned table (`--no-headers` for scripts) |
| `harmos get <query>` | Print (or `--copy`) a password; `--otp` for TOTP; `--path` for an exact, unambiguous selector; `-q` for value-only |
| `harmos gen` | Generate passwords with `crypto/rand` (short flags: `-n` length, `-c` count, `-x` exclude, `-a` no-ambiguous, `-e` one-each, `-y` copy, `-L/-U/-D/-S` drop a class) |
| `harmos sync [source]` | Pull each Pleasant source's OfflinePackage into its cache |
| `harmos sources` | List configured sources (no unlock needed) |
| `harmos add-source` / `remove-source` | Register or drop a source |
| `harmos save-password` / `remove-password` | Manage keyring passwords |
| `harmos themes` | List the built-in color themes |
| `harmos update` | Self-update to the latest release |

Run any command with `--help` for its full flag set.

## Configuration

Config lives at `$XDG_CONFIG_HOME/harmos/config.toml` (override with
`--config`). `add-source` writes it for you; you rarely edit it by hand.
**Passwords never go here** — they live in the OS keyring, or come from the
`HARMOS_MASTER` environment variable / an interactive prompt at unlock.

```toml
default = "work"
theme = "nord"
clipboard_timeout = "20s"
cache_stale_after = "24h"

[[source]]
name = "work"
type = "pps"
url = "https://pps.example.internal"
user = "alice"
cache = "~/.local/share/harmos/work.kdbx"

[[source]]
name = "personal"
type = "kdbx"
path = "~/vault.kdbx"
```

Generator defaults (`gen_length`, `gen_no_ambiguous`, …) and preferences
(clipboard timeout, cache staleness) are editable in the TUI Settings tab and
persisted here. Set `nerdfont = false` for terminals without a Nerd Font
(or `HARMOS_NERDFONT=0`).

## Not affiliated

Not affiliated with or endorsed by Pleasant Solutions or the KeePass project.
"Pleasant Password Server" and "KeePass" are third-party trademarks, used only
nominatively to describe compatibility. See `NOTICE`.

## Security

Not yet audited. Read-only w.r.t. your kdbx files; secrets go to the OS keyring
and concealed, auto-clearing clipboard, never to the config file. Report issues
per `SECURITY.md`. Licensed MIT — see `LICENSE`.
