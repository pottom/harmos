// Package config loads and validates the harmos TOML configuration.
//
// The data model assumes N sources from the start (spec §2a): several Pleasant
// servers and several local kdbx files may coexist. Source names are unique and
// qualify everything downstream. Secrets never live here — the master password
// and OAuth token go to the OS keyring, not this file (spec §9).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ErrNoSources is returned by Load when the config exists but has no sources.
var ErrNoSources = errors.New("no sources configured")

// Type is the kind of a source.
type Type string

const (
	// Pleasant is a Pleasant Password Server source: it has a cache and syncs.
	Pleasant Type = "pleasant"
	// Kdbx is a local .kdbx file source: the file is the source, never synced.
	// Read-only unless the user unlocks it for writing during a run — that
	// unlock is not persisted here or anywhere else.
	Kdbx Type = "kdbx"
)

// Duration wraps time.Duration so TOML can carry it as a string like "30s".
type Duration struct{ time.Duration }

// UnmarshalText parses a Go duration string (e.g. "30s", "2m").
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// Source is one configured source — a Pleasant server or a local kdbx file. The
// two are asymmetric (spec §2a): a Pleasant source has URL/User/Cache, a kdbx
// source has Path.
type Source struct {
	Name string `toml:"name"`
	Type Type   `toml:"type"`

	// Pleasant only:
	URL      string `toml:"url"`
	User     string `toml:"user"`
	Cache    string `toml:"cache"`
	CABundle string `toml:"ca_bundle"`

	// kdbx only:
	Path    string `toml:"path"`
	Keyfile string `toml:"keyfile"`
	// Writable opts this source in to editing. Absent means read-only, so a
	// config that has never heard of the feature keeps the old behaviour, and
	// enabling it is always something somebody did on purpose.
	Writable bool `toml:"writable"`
}

// Config is the whole configuration file.
type Config struct {
	Default          string   `toml:"default"`
	Theme            string   `toml:"theme"`    // built-in name or a themes/<name>.toml
	NerdFont         *bool    `toml:"nerdfont"` // nil = default on; set false for terminals without a Nerd Font
	ClipboardTimeout Duration `toml:"clipboard_timeout"`
	CacheStaleAfter  Duration `toml:"cache_stale_after"` // when a Pleasant cache is flagged stale
	Sources          []Source `toml:"source"`

	// Password generator preferences (nil / 0 = built-in default).
	GenLength  int    `toml:"gen_length"`
	GenLower   *bool  `toml:"gen_lower"`
	GenUpper   *bool  `toml:"gen_upper"`
	GenDigit   *bool  `toml:"gen_digit"`
	GenSymbol  *bool  `toml:"gen_symbol"`
	GenNoAmbig *bool  `toml:"gen_no_ambiguous"`
	GenOneEach *bool  `toml:"gen_one_each"`
	GenExclude string `toml:"gen_exclude"`
}

// DefaultClipboardTimeout is used when the file omits clipboard_timeout.
const DefaultClipboardTimeout = 30 * time.Second

// DefaultCacheStaleAfter is used when the file omits cache_stale_after.
const DefaultCacheStaleAfter = 24 * time.Hour

// DefaultPath returns $XDG_CONFIG_HOME/harmos/config.toml, falling back to
// ~/.config/harmos/config.toml.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "harmos", "config.toml"), nil
}

// Load reads, validates, and normalizes the config at path. It refuses a file
// with permissions looser than 0600 (spec §9) and returns descriptive errors
// for any invalid source.
func Load(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	// POSIX permission bits are not meaningful on Windows (it uses ACLs), where
	// files report 0666 regardless; only enforce 0600 where it applies.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("config %s is group/world accessible (mode %#o); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}

	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.ClipboardTimeout.Duration == 0 {
		c.ClipboardTimeout.Duration = DefaultClipboardTimeout
	}
	if c.CacheStaleAfter.Duration == 0 {
		c.CacheStaleAfter.Duration = DefaultCacheStaleAfter
	}
	if err := c.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) normalizeAndValidate() error {
	if len(c.Sources) == 0 {
		return ErrNoSources
	}

	seen := make(map[string]bool, len(c.Sources))
	for i := range c.Sources {
		p := &c.Sources[i]
		if p.Name == "" {
			return fmt.Errorf("source %d has no name", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate source name %q", p.Name)
		}
		seen[p.Name] = true

		switch p.Type {
		case Pleasant:
			if p.URL == "" || p.User == "" || p.Cache == "" {
				return fmt.Errorf("pleasant source %q needs url, user, and cache", p.Name)
			}
		case Kdbx:
			if p.Path == "" {
				return fmt.Errorf("kdbx source %q needs path", p.Name)
			}
		case "":
			return fmt.Errorf("source %q has no type (want %q or %q)", p.Name, Pleasant, Kdbx)
		default:
			return fmt.Errorf("source %q has unknown type %q", p.Name, p.Type)
		}

		for _, pp := range []*string{&p.Cache, &p.Path, &p.CABundle, &p.Keyfile} {
			expanded, err := expandHome(*pp)
			if err != nil {
				return err
			}
			*pp = expanded
		}
	}

	// A default pointing at a missing source (e.g. one that was removed) is not
	// fatal — the field is optional and nothing breaks without it, so drop it
	// rather than refusing to load the whole config.
	if c.Default != "" && !seen[c.Default] {
		c.Default = ""
	}
	return nil
}

// Source returns the named source, or nil if it does not exist.
func (c *Config) Source(name string) *Source {
	for i := range c.Sources {
		if c.Sources[i].Name == name {
			return &c.Sources[i]
		}
	}
	return nil
}

// expandHome expands a leading ~ or ~/ to the user's home directory. ~user is
// not supported and is returned unchanged.
func expandHome(p string) (string, error) {
	if p == "" || p[0] != '~' {
		return p, nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, p[1:]), nil
	}
	return p, nil
}
