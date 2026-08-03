# harmos

> ἁρμός (*harmós*) — a joint: the place where two fitted parts meet.

A terminal password client for **Pleasant Password Server** (a.k.a. KeePass Hub)
and local `.kdbx` files. It syncs a Pleasant server's
offline package into a local, encrypted kdbx cache and reads that cache — plus
any local kdbx files — through one shared reader, in a TUI and a scriptable CLI.

- **Read-only until you say otherwise.** Browse, search, copy. A vault is
  editable only after you unlock it, once — a config that has never heard of the
  feature keeps the old read-only behaviour.
- **Never writes to the server.** No push, no bidirectional sync. A Pleasant
  cache is rebuilt by `sync`, so it is structurally unwritable: no code path
  can produce a write handle for one.
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
set `HARMOS_NO_UPDATE_CHECK=1` to disable it. A package-managed install
(`.deb`/`.rpm` under a root-owned path) is left to the package manager — `harmos
update` says so instead of fighting it.

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

## Concepts

- **Source** — one place credentials come from: a Pleasant server (`type = pps`)
  or a local `.kdbx` file (`type = kdbx`). You can configure many; harmos reads
  them all at once into one searchable set, each entry tagged with its source.
- **Cache** — a Pleasant source is read through a local **KDBX4 cache** that
  `harmos sync` writes from the server's offline package. Reads are always
  against the cache, so browsing is instant and works offline; `sync` is the
  only moment harmos talks to the server, and it is always explicit.
- **Master password** — one password unlocks every Pleasant cache. It comes from
  the `HARMOS_MASTER` environment variable, the OS keyring (if you saved it), or
  an interactive prompt — never the config file.
- **Keyring** — master, per-source, and server passwords live in the OS-native
  secret store (macOS Keychain, Windows Credential Manager, Linux Secret
  Service). Storing a password is always opt-in (`--save-password`).
- **Two keyfiles, don't confuse them:**
  - Your **own** `.kdbx` may use a KeePass *keyfile* — pass it with
    `add-source --keyfile` (or the TUI's Keyfile field).
  - Each **Pleasant cache** is additionally locked with an automatic,
    machine-local keyfile harmos generates on first sync (see [Security](#security)).
    There is nothing to configure — it just protects the cache at rest.

## Usage

### TUI

Running `harmos` with no subcommand opens the terminal UI. Four tabs, switched
with `1` / `2` / `3` / `4`:

- **Vault** — a collapsible source / folder tree, an entry table with a live
  search bar, and a detail pane (notes, custom fields, dates, attachments, TOTP).
- **Changes** — edits you have staged but not written, as a diff, with per-change
  revert. Empty until you unlock a source and change something.
- **Generate** — a `crypto/rand` password generator with a strength bar, class
  breakdown, and recent-roll history (the same engine as `harmos gen`).
- **Settings** — sources (add / edit / sync / save-password / remove), the live
  theme picker, Nerd Font toggle, and preferences (clipboard timeout, cache
  staleness), all persisted to the config.

Press `?` for the full key map. The staples:

| Key | Action |
| --- | --- |
| `/` | Search (supports `field:`, `\|` for OR, `"phrase"`, `-exclude`) |
| `→` | Open entry detail · `esc` / `←` to go back |
| `enter` | Expand a folder · **copy** the selected password |
| `ctrl+y` | Copy the password (concealed, auto-cleared) |
| `ctrl+u` / `ctrl+o` | Copy the username / URL |
| `ctrl+t` | Copy the current TOTP code |
| `c` | Copy a `harmos get …` command for the selected entry |
| `s` | Save attachments (entry detail) |
| `z` / `Z` | Fold the selected branch / every folder, all the way down (`shift+←`/`shift+→` too, where the terminal sends them) |
| `ctrl+b` | Hide / show the folder tree pane |
| `1` / `2` / `3` / `4` | Switch Vault / Changes / Generate / Settings tab |
| `ctrl+w` | Unlock this source for editing (or lock it again) |
| `e` · `r` · `n` / `N` · `d` / `D` · `m` | Edit an entry · rename in place · new entry / folder · delete to bin / permanently (press again to undo) · move |
| `ctrl+s` | Review the staged changes and write them |
| `?` / `q` | Help · quit (clears the clipboard) |

### CLI

Every read the TUI does is also a command, for scripts and pipes.

**`harmos ls [source]`** — list entries in an aligned table.

```sh
harmos ls                      # every source
harmos ls work                 # just one
harmos ls --no-headers         # for piping into awk/cut
```

**`harmos get <query>`** — print or copy one password; it refuses to guess when
a query is ambiguous.

```sh
harmos get "aws root"          # print the password to stdout
harmos get "aws root" --copy   # copy it (concealed, auto-cleared)
harmos get github --otp        # the current TOTP code instead
harmos get --path "work/Infra/db-prod" --user svc_admin   # exact, for scripts
harmos get db-prod -q          # value only — no provenance line on stderr
```

**`harmos gen`** — generate passwords with `crypto/rand`.

```sh
harmos gen                     # one, using your saved options
harmos gen -n 32 -c 5          # five 32-char passwords
harmos gen -n 24 -a -e         # no ambiguous glyphs, one of each class
harmos gen -LUS                # digits only (drop lower/upper/symbol)
harmos gen -x '0Oo' -y         # exclude some chars, copy to the clipboard
```

Short flags: `-n` length, `-c` count, `-x` exclude, `-a` no-ambiguous,
`-e` one-each, `-y` copy, and `-L/-U/-D/-S` to drop lower/upper/digit/symbol.

**`harmos sync [source]`** — pull each Pleasant source's offline package into its
cache (kdbx sources are skipped).

```sh
harmos sync                    # every Pleasant source
harmos sync work               # just one
harmos sync work --save-password
```

**Managing sources and passwords:**

```sh
harmos sources                 # list configured sources (no unlock needed)
harmos add-source ~/vault.kdbx --keyfile ~/vault.key
harmos remove-source work
harmos save-password work      # store a source's password in the keyring
harmos remove-password work
harmos themes                  # list the built-in color themes
```

Run any command with `--help` for its full flag set.

## Search

The search bar (TUI `/`) and `harmos get` share one matcher. Beyond plain
substring matching, the query language understands:

| Query | Matches |
| --- | --- |
| `db prod` | entries matching **both** words (implicit AND) |
| `db \| cache` | either word (`\|` is OR; a space is AND) |
| `url:ssh` | only the URL field (also `user:`, `title:`, `notes:`, `tag:`) |
| `src:own svc-admin` | `svc-admin`, narrowed to the source named `own` |
| `"db prod"` | the exact phrase |
| `db -staging` | `db` but **not** `staging` |
| `ppk` | falls back to fuzzy matching when nothing else hits |

Result rows show **what** matched and **where** (title · source · the matched
field excerpt), so a wrong-source copy is obvious before you make it.

### Editing

Local `.kdbx` files can be edited. Pleasant sources cannot: their cache is
rebuilt by `sync`, so an edit would be silently discarded — harmos has no code
path that could write one.

Editing is deliberately several steps, and reversible until the last:

1. **Unlock** the source with `ctrl+w`. Sources are read-only until you do, and
   the choice is remembered (`writable = true` in the config), so you are asked
   once rather than every launch. `ctrl+w` again locks it.
2. **Change** things: `e` edits, `n` and `N` create an entry or a folder, `d`
   sends to the recycle bin (`D` deletes permanently). `m` picks a row up and
   leaves the tree live: walk to the folder you want with the keys you browse
   with, and `↵` puts it there — a drag, done with the keyboard. `N` makes a
   folder on its own row, where you can see which folder it goes in; `n` opens
   a form for an entry, and the frame names the folder it will land in. `r` renames
   in place: the row under the cursor turns into a field — a folder in the tree,
   an entry's title in the list — so the vault you are renaming inside stays on
   screen. `e` is the whole form, which only an entry has; on a folder row it
   points you at `r`. `ctrl+g` rolls a password in the editor using your
   Generate-tab settings.
3. **Look at it.** Staged rows are coloured where they are — teal for new, amber
   for changed, rust for deleted — and every state carries a glyph as well as a
   colour. The two deletions are separate signals throughout: `-` for the
   recycle bin, `✕` in bold for a permanent one, on the row, in the review, in
   the tally and in the confirmation. Everything inside a permanently deleted
   folder wears `✕` too, because that is where it is going. The Changes tab
   shows your vault's own tree with a git-style diff under each item, and `x`
   reverts one. No password ever appears there.
4. **Write** with `ctrl+s`, which names the file, the backup it will take, and
   what the changes come to — counted in folders and entries, with anything
   going permanently on a line of its own.

Nothing reaches your file until that last confirmation. Quitting with unsaved
changes asks first.

**What a save does.** It copies your file aside, regenerates the encryption
nonces (reusing them would be a keystream reuse), writes to a temp file, decodes
that back to prove it is readable, checks nothing was lost, and only then renames
it into place. If something else changed the file in the meantime, the save is
refused rather than overwriting it. KeePass history is written the way other
clients expect, and deletions leave the tombstones a synchronising client needs.

**KDBX 4.1** is supported through a patched copy of the kdbx library
(now upstream in gokeepasslib v3.7.0). A file harmos cannot round-trip
without losing something is refused for writing, and it says which element it
would have lost.

## Configuration

Config lives at `$XDG_CONFIG_HOME/harmos/config.toml` (override with
`--config`). `add-source` writes it for you; you rarely edit it by hand.
**Passwords never go here** — they live in the OS keyring or come from
`HARMOS_MASTER` / a prompt.

```toml
default = "work"              # source opened first
theme = "nord"                # a built-in name, or themes/<name>.toml
clipboard_timeout = "20s"     # how long a copied secret lingers
cache_stale_after = "24h"     # when a Pleasant cache is flagged stale

# Generator defaults (also editable in the Generate tab)
gen_length = 24
gen_no_ambiguous = true

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
keyfile = "~/vault.key"       # only if your kdbx uses one
writable = true               # opt in to editing; absent means read-only
```

**Environment variables:**

| Variable | Effect |
| --- | --- |
| `HARMOS_MASTER` | the master password (skips the prompt/keyring) |
| `HARMOS_NO_UPDATE_CHECK` | set to disable the background release check |
| `HARMOS_NERDFONT=0` | force the plain-Unicode fallback (no Nerd Font) |
| `HARMOS_VERSION` / `HARMOS_INSTALL_DIR` | pin / place the installer's download |
| `XDG_CONFIG_HOME` / `XDG_DATA_HOME` | config and cache locations |

## Scripting

harmos is TTY-aware: `ls` / `get` / `sources` emit no ANSI when piped, and
launching the TUI without a TTY exits with a clear message.

```sh
# Feed a password into another tool without it ever touching the clipboard:
psql "postgres://svc_admin@db.internal/app" \
  --password "$(harmos get --path 'work/Infra/db-prod' --user svc_admin -q)"

# Non-interactive unlock (CI, cron) — master from the environment:
HARMOS_MASTER="$MASTER" harmos ls work --no-headers
```

## Not affiliated

Not affiliated with or endorsed by Pleasant Solutions or the KeePass project.
"Pleasant Password Server" and "KeePass" are third-party trademarks, used only
nominatively to describe compatibility. See `NOTICE`.

## Security

Not yet audited. The design:

- **Browsing never writes.** harmos opens your kdbx `O_RDONLY` and leaves its
  bytes and mtime unchanged; a session that stages edits and does not save
  changes nothing either. That is an invariant, tested.
- **Writing is narrow and deliberate.** Only an explicitly unlocked local kdbx
  can be written, only by an explicit save, and only after a confirmation that
  names the file and the backup it will take. Every save regenerates the file's
  nonces — reusing them would be a keystream reuse — writes to a temp file,
  decodes that back to prove it is readable, and only then replaces the
  original. A file changed by something else in the meantime is refused rather
  than overwritten.
- **Secrets off disk** — master, per-source, and server passwords go to the OS
  keyring; the config file never holds a secret. A copied password is written to
  a **concealed** clipboard (the platform's do-not-record hints) and cleared
  after the timeout.
- **Cache at rest** — the Pleasant cache is a KDBX4 file (Argon2d, ChaCha20)
  locked with a **composite key**: your master password *and* a random keyfile
  harmos generates on first sync at `$XDG_CONFIG_HOME/harmos/<name>.key` (mode
  `0600`), kept in the config dir, apart from the cache under `$XDG_DATA_HOME`.
  A cache copied off the machine therefore cannot be opened with the master
  alone. You can verify it after a sync: `ls -l ~/.config/harmos/*.key`.
- **Explicit sync** — harmos talks to the server only during `harmos sync`, never
  on a timer or in the background, and honors the offline package's expiry.

Report issues per `SECURITY.md`. Licensed MIT — see `LICENSE`.
