package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pottom/harmos/internal/atomicfile"
)

// ErrSourceExists is returned by WriteKdbxSource/WritePleasantSource when a
// source of that name already exists and overwrite was not requested.
var ErrSourceExists = errors.New("source already exists")

// DeriveSourceName is the default source name for a path: the file name
// without its extension.
func DeriveSourceName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// AbsPath expands a leading ~ and makes the path absolute.
func AbsPath(p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		switch {
		case p == "~":
			p = home
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(home, p[2:])
		}
	}
	return filepath.Abs(p)
}

// DefaultCachePath is where a Pleasant source's cache lives when no path is
// given: $XDG_DATA_HOME/harmos/<name>.kdbx, falling back to ~/.local/share.
func DefaultCachePath(name string) (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "harmos", name+".kdbx"), nil
}

// DefaultCacheKeyfilePath is where a Pleasant source's cache keyfile lives:
// $XDG_CONFIG_HOME/harmos/<name>.key (falling back to ~/.config). It sits in the
// config directory, deliberately apart from the cache under $XDG_DATA_HOME, so a
// copied-away cache file is not accompanied by the key that (with the master)
// decrypts it. The path is derived from the source name, so writer and reader
// agree without recording it in the config.
func DefaultCacheKeyfilePath(name string) (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "harmos", name+".key"), nil
}

// EnsureKeyfile creates a random cache keyfile at path (0600, parent 0700) if one
// is not already there, and is a no-op when it exists — so a source keeps the same
// key across syncs. The content is 48 random bytes: KeePass hashes keyfile bytes
// whose length is neither 32 nor 64, so this can never be mistaken for a raw or
// hex-encoded key, and it composes with the master to encrypt the cache.
func EnsureKeyfile(path string) error {
	if path == "" {
		return fmt.Errorf("empty keyfile path")
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	key := make([]byte, 48)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("generate cache keyfile: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return fmt.Errorf("write cache keyfile: %w", err)
	}
	return nil
}

// SourceExists reports whether the config at path already has a source named
// name. A missing or source-less config reports false — it is appendable.
func SourceExists(path, name string) (bool, error) {
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return false, nil
	}
	cfg, err := Load(path)
	if err != nil {
		if errors.Is(err, ErrNoSources) {
			return false, nil
		}
		return false, err
	}
	return cfg.Source(name) != nil, nil
}

// WriteKdbxSource adds a local kdbx source to the config at path, or replaces
// an existing one when overwrite is true (rewriting only that block). It returns
// "added" or "updated".
func WriteKdbxSource(path, name, kdbxPath, keyfile string, overwrite bool) (string, error) {
	if err := ValidSourceName(name); err != nil {
		return "", err
	}
	return upsert(path, name, buildKdbxBlock(name, kdbxPath, keyfile), overwrite)
}

// WritePleasantSource is WriteKdbxSource for a Pleasant source.
func WritePleasantSource(path, name, url, user, cache, caBundle string, overwrite bool) (string, error) {
	if err := ValidSourceName(name); err != nil {
		return "", err
	}
	return upsert(path, name, buildPleasantBlock(name, url, user, cache, caBundle), overwrite)
}

// ValidSourceName rejects a name that would break something downstream.
//
// A source name is not only a label: every identity harmos hands out is
// "<name>:<base64 uuid>" for an entry and "<name>:g:<…>" for a folder, with a
// "#n" suffix for a duplicated UUID. A name containing ":" or "#" makes every
// one of those ambiguous, and the failure is delayed and total — the interface
// stages changes happily and then every save of that source fails forever with
// "malformed id", with nothing in the program able to repair it but an edit to
// the config file.
//
// The rest are the ordinary ones: a name is used in messages, in the keyring
// account, and as a directory-adjacent filename for the cache keyfile.
func ValidSourceName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("a source needs a name")
	case name != strings.TrimSpace(name):
		return errors.New("a source name cannot start or end with a space")
	case strings.ContainsAny(name, ":#"):
		return errors.New(`a source name cannot contain ":" or "#" — they separate the parts of an entry's identity`)
	case strings.ContainsAny(name, "/\\"):
		return errors.New("a source name cannot contain a path separator")
	case strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }):
		return errors.New("a source name cannot contain control characters")
	}
	return nil
}

// RemoveSource deletes a source's block (and a top-level default that named it),
// leaving the rest of the file verbatim. It returns how many sources remain.
func RemoveSource(path, name string) (int, error) {
	cfg, err := Load(path)
	if err != nil {
		return 0, err
	}
	if cfg.Source(name) == nil {
		return 0, fmt.Errorf("no source named %q", name)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	next, ok := removeSourceBlock(string(content), name)
	if !ok {
		return 0, fmt.Errorf("could not locate the %q source block in %s", name, path)
	}
	if cfg.Default == name {
		next = removeTopLevelKey(next, "default")
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return 0, err
	}
	remaining := len(cfg.Sources) - 1
	if remaining >= 1 {
		if _, err := Load(path); err != nil {
			return 0, fmt.Errorf("config is invalid after removing %q: %w", name, err)
		}
	}
	return remaining, nil
}

// SetTopLevelKey sets a top-level `key = "value"` in the config, updating it in
// place if present, else inserting it before the first table — leaving the rest
// of the file verbatim.
func SetTopLevelKey(path, key, value string) error {
	return setTopLevel(path, key, fmt.Sprintf("%s = %q", key, value))
}

// SetTopLevelBool sets a top-level boolean key (unquoted TOML).
func SetTopLevelBool(path, key string, v bool) error {
	return setTopLevel(path, key, fmt.Sprintf("%s = %t", key, v))
}

// SetPreferences persists the clipboard timeout and cache-stale threshold (both
// TOML duration strings, e.g. "30s", "24h") in one write.
func SetPreferences(path, clipboardTimeout, cacheStaleAfter string) error {
	return setTopLevelMany(path, map[string]string{
		"clipboard_timeout": strconv.Quote(clipboardTimeout),
		"cache_stale_after": strconv.Quote(cacheStaleAfter),
	})
}

// SetGenerator persists the password-generator preferences in one write.
func SetGenerator(path string, length int, lower, upper, digit, symbol, noAmbig, oneEach bool, exclude string) error {
	return setTopLevelMany(path, map[string]string{
		"gen_length":       strconv.Itoa(length),
		"gen_lower":        strconv.FormatBool(lower),
		"gen_upper":        strconv.FormatBool(upper),
		"gen_digit":        strconv.FormatBool(digit),
		"gen_symbol":       strconv.FormatBool(symbol),
		"gen_no_ambiguous": strconv.FormatBool(noAmbig),
		"gen_one_each":     strconv.FormatBool(oneEach),
		"gen_exclude":      strconv.Quote(exclude),
	})
}

// setTopLevelMany updates several top-level keys (before the first table) in a
// single read-modify-write, inserting any that are absent. Creates the file if it
// does not exist yet.
func setTopLevelMany(path string, kv map[string]string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		content = nil
	}
	lines := strings.Split(string(content), "\n")

	firstTable := len(lines)
	seen := map[string]bool{}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			firstTable = i
			break
		}
		if k, _, ok := parseKV(t); ok {
			if v, want := kv[k]; want {
				lines[i] = fmt.Sprintf("%s = %s", k, v)
				seen[k] = true
			}
		}
	}

	missing := make([]string, 0, len(kv))
	for k, v := range kv {
		if !seen[k] {
			missing = append(missing, fmt.Sprintf("%s = %s", k, v))
		}
	}
	sort.Strings(missing) // deterministic order for inserted keys

	out := make([]string, 0, len(lines)+len(missing)+1)
	out = append(out, lines[:firstTable]...)
	out = append(out, missing...)
	if len(missing) > 0 && firstTable < len(lines) {
		out = append(out, "")
	}
	out = append(out, lines[firstTable:]...)
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")))
}

// setTopLevel updates key in place before the first table, or inserts newLine
// there if it's absent.
func setTopLevel(path, key, newLine string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")

	firstTable := len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			firstTable = i
			break
		}
		if k, _, ok := parseKV(t); ok && k == key {
			lines[i] = newLine
			return writeFileAtomic(path, []byte(strings.Join(lines, "\n")))
		}
	}

	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:firstTable]...)
	out = append(out, newLine, "")
	out = append(out, lines[firstTable:]...)
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")))
}

// upsert inserts block as a new source, or — if a source of that name exists —
// rewrites just that block (leaving the rest of the file verbatim). A file that
// parses but has no sources yet is appendable. Returns "added" or "updated".
func upsert(path, name, block string, overwrite bool) (string, error) {
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if err := writeFileAtomic(path, []byte(block)); err != nil {
			return "", err
		}
		return "added", nil
	} else if statErr != nil {
		return "", statErr
	}

	cfg, err := Load(path)
	if err != nil {
		if !errors.Is(err, ErrNoSources) {
			return "", err
		}
		cfg = &Config{}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	verb := "added"
	var next string
	if cfg.Source(name) != nil {
		if !overwrite {
			return "", ErrSourceExists
		}
		replaced, ok := replaceSourceBlock(string(content), name, strings.TrimRight(block, "\n"))
		if !ok {
			return "", fmt.Errorf("could not locate the %q source block to overwrite in %s", name, path)
		}
		next, verb = replaced, "updated"
	} else {
		base := string(content)
		if !strings.HasSuffix(base, "\n") {
			base += "\n"
		}
		next = base + "\n" + block
	}

	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return "", err
	}
	if _, err := Load(path); err != nil {
		return "", fmt.Errorf("config is invalid after %s %q: %w", verb, name, err)
	}
	return verb, nil
}

func buildKdbxBlock(name, path, keyfile string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[source]]\n")
	fmt.Fprintf(&b, "name = %q\n", name)
	fmt.Fprintf(&b, "type = %q\n", string(Kdbx))
	fmt.Fprintf(&b, "path = %q\n", path)
	if keyfile != "" {
		fmt.Fprintf(&b, "keyfile = %q\n", keyfile)
	}
	return b.String()
}

func buildPleasantBlock(name, url, user, cache, caBundle string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[source]]\n")
	fmt.Fprintf(&b, "name = %q\n", name)
	fmt.Fprintf(&b, "type = %q\n", string(Pleasant))
	fmt.Fprintf(&b, "url = %q\n", url)
	fmt.Fprintf(&b, "user = %q\n", user)
	fmt.Fprintf(&b, "cache = %q\n", cache)
	if caBundle != "" {
		fmt.Fprintf(&b, "ca_bundle = %q\n", caBundle)
	}
	return b.String()
}

// replaceSourceBlock rewrites the [[source]] block whose name matches, leaving
// every other line — comments, blank lines, other sources, top-level keys —
// exactly as it was. A block is the header line plus the contiguous run of key
// lines under it, up to the first blank line or the next table header.
func replaceSourceBlock(content, name, newBlock string) (string, bool) {
	lines := strings.Split(content, "\n")
	isTableHeader := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

	for i := range lines {
		if strings.TrimSpace(lines[i]) != "[[source]]" {
			continue
		}
		j := i + 1
		blockName := ""
		for j < len(lines) {
			t := strings.TrimSpace(lines[j])
			if t == "" || isTableHeader(lines[j]) {
				break
			}
			if k, v, ok := parseKV(t); ok && k == "name" {
				blockName = v
			}
			j++
		}
		if blockName != name {
			continue
		}
		out := make([]string, 0, len(lines))
		out = append(out, lines[:i]...)
		out = append(out, strings.Split(newBlock, "\n")...)
		out = append(out, lines[j:]...)
		return strings.Join(out, "\n"), true
	}
	return content, false
}

// removeSourceBlock deletes the [[source]] block whose name matches (plus one
// trailing blank line so no double gap is left), leaving everything else verbatim.
func removeSourceBlock(content, name string) (string, bool) {
	lines := strings.Split(content, "\n")
	isTableHeader := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

	for i := range lines {
		if strings.TrimSpace(lines[i]) != "[[source]]" {
			continue
		}
		j := i + 1
		blockName := ""
		for j < len(lines) {
			t := strings.TrimSpace(lines[j])
			if t == "" || isTableHeader(lines[j]) {
				break
			}
			if k, v, ok := parseKV(t); ok && k == "name" {
				blockName = v
			}
			j++
		}
		if blockName != name {
			continue
		}
		if j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++ // swallow the separating blank line
		}
		out := make([]string, 0, len(lines))
		out = append(out, lines[:i]...)
		out = append(out, lines[j:]...)
		return strings.Join(out, "\n"), true
	}
	return content, false
}

// removeTopLevelKey drops a top-level `key = ...` line (the region before the
// first table header), leaving everything else verbatim.
func removeTopLevelKey(content, key string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inTop := true
	for _, l := range lines {
		if inTop {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "[") {
				inTop = false
			} else if k, _, ok := parseKV(t); ok && k == key {
				continue
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// parseKV splits a `key = value` line, unquoting a string value. It ignores
// comment lines.
func parseKV(line string) (key, val string, ok bool) {
	if strings.HasPrefix(line, "#") {
		return "", "", false
	}
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(k)
	val = strings.TrimSpace(v)
	if len(val) >= 2 && val[0] == '"' {
		if uq, err := strconv.Unquote(val); err == nil {
			val = uq
		}
	} else if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
		val = val[1 : len(val)-1]
	}
	return key, val, true
}

// writeFileAtomic replaces the config in one step, at 0600 — a half-written
// config is a config that will not load.
func writeFileAtomic(path string, data []byte) error {
	return atomicfile.WriteBytes(path, 0o600, data)
}

// SetSourceWritable records whether a source may be edited, leaving the rest of
// its block verbatim.
//
// Turning it off removes the key rather than writing `writable = false`: absent
// is already the safe default, and a config carrying every setting at its
// default value is harder to read than one that only says what differs.
func SetSourceWritable(path, name string, writable bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, ok := setSourceKey(string(content), name, "writable", writable)
	if !ok {
		return fmt.Errorf("no source named %q", name)
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return err
	}
	// A config that no longer parses is worse than one that never changed, and
	// this is the only place a boolean is threaded into a hand-edited file.
	if _, err := Load(path); err != nil {
		return fmt.Errorf("config is invalid after setting writable on %q: %w", name, err)
	}
	return nil
}

// setSourceKey sets or clears a boolean key inside one [[source]] block.
func setSourceKey(content, name, key string, on bool) (string, bool) {
	lines := strings.Split(content, "\n")
	isTableHeader := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

	for i := range lines {
		if strings.TrimSpace(lines[i]) != "[[source]]" {
			continue
		}
		j, blockName, keyAt := i+1, "", -1
		for j < len(lines) {
			t := strings.TrimSpace(lines[j])
			if t == "" || isTableHeader(lines[j]) {
				break
			}
			if k, v, ok := parseKV(t); ok {
				if k == "name" {
					blockName = v
				}
				if k == key {
					keyAt = j
				}
			}
			j++
		}
		if blockName != name {
			continue
		}

		switch {
		case on && keyAt >= 0:
			lines[keyAt] = fmt.Sprintf("%s = true", key)
		case on:
			out := append([]string{}, lines[:j]...)
			out = append(out, fmt.Sprintf("%s = true", key))
			out = append(out, lines[j:]...)
			lines = out
		case keyAt >= 0:
			lines = append(lines[:keyAt], lines[keyAt+1:]...)
		}
		return strings.Join(lines, "\n"), true
	}
	return content, false
}
