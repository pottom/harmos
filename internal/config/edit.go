package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrProfileExists is returned by WriteKdbxProfile/WritePleasantProfile when a
// profile of that name already exists and overwrite was not requested.
var ErrProfileExists = errors.New("profile already exists")

// DeriveProfileName is the default profile name for a path: the file name
// without its extension.
func DeriveProfileName(path string) string {
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

// ProfileExists reports whether the config at path already has a profile named
// name. A missing or profile-less config reports false — it is appendable.
func ProfileExists(path, name string) (bool, error) {
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return false, nil
	}
	cfg, err := Load(path)
	if err != nil {
		if errors.Is(err, ErrNoProfiles) {
			return false, nil
		}
		return false, err
	}
	return cfg.Profile(name) != nil, nil
}

// WriteKdbxProfile adds a local kdbx profile to the config at path, or replaces
// an existing one when overwrite is true (rewriting only that block). It returns
// "added" or "updated".
func WriteKdbxProfile(path, name, kdbxPath, keyfile string, overwrite bool) (string, error) {
	return upsert(path, name, buildKdbxBlock(name, kdbxPath, keyfile), overwrite)
}

// WritePleasantProfile is WriteKdbxProfile for a Pleasant source.
func WritePleasantProfile(path, name, url, user, cache, caBundle string, overwrite bool) (string, error) {
	return upsert(path, name, buildPleasantBlock(name, url, user, cache, caBundle), overwrite)
}

// RemoveProfile deletes a profile's block (and a top-level default that named it),
// leaving the rest of the file verbatim. It returns how many profiles remain.
func RemoveProfile(path, name string) (int, error) {
	cfg, err := Load(path)
	if err != nil {
		return 0, err
	}
	if cfg.Profile(name) == nil {
		return 0, fmt.Errorf("no profile named %q", name)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	next, ok := removeProfileBlock(string(content), name)
	if !ok {
		return 0, fmt.Errorf("could not locate the %q profile block in %s", name, path)
	}
	if cfg.Default == name {
		next = removeTopLevelKey(next, "default")
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return 0, err
	}
	remaining := len(cfg.Profiles) - 1
	if remaining >= 1 {
		if _, err := Load(path); err != nil {
			return 0, fmt.Errorf("config is invalid after removing %q: %w", name, err)
		}
	}
	return remaining, nil
}

// upsert inserts block as a new profile, or — if a profile of that name exists —
// rewrites just that block (leaving the rest of the file verbatim). A file that
// parses but has no profiles yet is appendable. Returns "added" or "updated".
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
		if !errors.Is(err, ErrNoProfiles) {
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
	if cfg.Profile(name) != nil {
		if !overwrite {
			return "", ErrProfileExists
		}
		replaced, ok := replaceProfileBlock(string(content), name, strings.TrimRight(block, "\n"))
		if !ok {
			return "", fmt.Errorf("could not locate the %q profile block to overwrite in %s", name, path)
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
	fmt.Fprintf(&b, "[[profile]]\n")
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
	fmt.Fprintf(&b, "[[profile]]\n")
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

// replaceProfileBlock rewrites the [[profile]] block whose name matches, leaving
// every other line — comments, blank lines, other profiles, top-level keys —
// exactly as it was. A block is the header line plus the contiguous run of key
// lines under it, up to the first blank line or the next table header.
func replaceProfileBlock(content, name, newBlock string) (string, bool) {
	lines := strings.Split(content, "\n")
	isTableHeader := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

	for i := range lines {
		if strings.TrimSpace(lines[i]) != "[[profile]]" {
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

// removeProfileBlock deletes the [[profile]] block whose name matches (plus one
// trailing blank line so no double gap is left), leaving everything else verbatim.
func removeProfileBlock(content, name string) (string, bool) {
	lines := strings.Split(content, "\n")
	isTableHeader := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

	for i := range lines {
		if strings.TrimSpace(lines[i]) != "[[profile]]" {
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

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".harmos-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
