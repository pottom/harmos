// Package config loads and validates the harmos TOML configuration.
//
// The data model assumes N sources from the start (spec §2a): several Pleasant
// servers and several local kdbx files may coexist. Profile names are unique and
// qualify everything downstream. Secrets never live here — the master password
// and OAuth token go to the OS keyring, not this file (spec §9).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Type is the kind of a source.
type Type string

const (
	// Pleasant is a Pleasant Password Server source: it has a cache and syncs.
	Pleasant Type = "pleasant"
	// Kdbx is a local .kdbx file source: the file is the source, read-only,
	// never synced.
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

// Profile is one source. Pleasant and kdbx profiles are asymmetric (spec §2a):
// a Pleasant profile has URL/User/Cache, a kdbx profile has Path.
type Profile struct {
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
}

// Config is the whole configuration file.
type Config struct {
	Default          string    `toml:"default"`
	ClipboardTimeout Duration  `toml:"clipboard_timeout"`
	Profiles         []Profile `toml:"profile"`
}

// DefaultClipboardTimeout is used when the file omits clipboard_timeout.
const DefaultClipboardTimeout = 30 * time.Second

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
// for any invalid profile.
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
	if err := c.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) normalizeAndValidate() error {
	if len(c.Profiles) == 0 {
		return fmt.Errorf("no profiles configured")
	}

	seen := make(map[string]bool, len(c.Profiles))
	for i := range c.Profiles {
		p := &c.Profiles[i]
		if p.Name == "" {
			return fmt.Errorf("profile %d has no name", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate profile name %q", p.Name)
		}
		seen[p.Name] = true

		switch p.Type {
		case Pleasant:
			if p.URL == "" || p.User == "" || p.Cache == "" {
				return fmt.Errorf("pleasant profile %q needs url, user, and cache", p.Name)
			}
		case Kdbx:
			if p.Path == "" {
				return fmt.Errorf("kdbx profile %q needs path", p.Name)
			}
		case "":
			return fmt.Errorf("profile %q has no type (want %q or %q)", p.Name, Pleasant, Kdbx)
		default:
			return fmt.Errorf("profile %q has unknown type %q", p.Name, p.Type)
		}

		for _, pp := range []*string{&p.Cache, &p.Path, &p.CABundle, &p.Keyfile} {
			expanded, err := expandHome(*pp)
			if err != nil {
				return err
			}
			*pp = expanded
		}
	}

	if c.Default != "" && !seen[c.Default] {
		return fmt.Errorf("default profile %q is not defined", c.Default)
	}
	return nil
}

// Profile returns the named profile, or nil if it does not exist.
func (c *Config) Profile(name string) *Profile {
	for i := range c.Profiles {
		if c.Profiles[i].Name == name {
			return &c.Profiles[i]
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
