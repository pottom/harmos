package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/session"
)

// noSourcesError guides a user who has no config yet toward adding a source. It
// is a type (not errors.New) so the multi-line guidance isn't a lint-flagged
// error string.
type noSourcesError struct{}

func (noSourcesError) Error() string {
	return `no sources configured yet.

Add one:
  harmos add-source PATH.kdbx                   a local KeePass .kdbx file
  harmos add-source --type pps --name NAME \    a Pleasant Password Server
      --url URL --user USER

See 'harmos add-source --help' for the details.`
}

func loadConfigAt(path string) (*config.Config, error) {
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	cfg, err := config.Load(path)
	if err != nil && (errors.Is(err, config.ErrNoSources) || errors.Is(err, os.ErrNotExist)) {
		return nil, noSourcesError{}
	}
	return cfg, err
}

func onTTY() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// openAll loads the config and opens every source, resolving each password from
// the keyring or a prompt and re-prompting immediately on a wrong one. With no
// TTY a password-protected source that isn't covered by env/keyring is simply
// excluded (spec §2a).
func openAll(configPath string) (*session.Result, *config.Config, error) {
	cfg, err := loadConfigAt(configPath)
	if err != nil {
		return nil, nil, err
	}

	// The master unlocks every Pleasant cache (spec §2a); resolve it once and
	// lazily — from HARMOS_MASTER, then the keyring, then a prompt — and re-prompt
	// on retry so a wrong master is caught on the spot.
	var master secret.Secret
	var masterSet bool
	masterOnce := func(retry bool) (secret.Secret, bool, error) {
		if masterSet && !retry {
			return master, false, nil
		}
		if !masterSet && !retry {
			if v, ok := os.LookupEnv("HARMOS_MASTER"); ok {
				master, masterSet = secret.New(v), true
				return master, false, nil
			}
			if pw, ok, ferr := keyring.FetchMaster(); ferr == nil && ok {
				master, masterSet = pw, true
				return master, false, nil
			}
		}
		if !onTTY() {
			return master, false, nil
		}
		if retry {
			fmt.Fprintln(os.Stderr, "wrong master password — try again")
		}
		pw, perr := promptPassword("harmos master password: ")
		if perr != nil {
			return secret.Secret{}, false, perr
		}
		master, masterSet = pw, true
		return master, true, nil
	}

	// Per source: a saved keyring password unlocks without asking; otherwise a
	// prompt, re-prompting on a wrong password.
	ask := func(p config.Source, retry bool) (secret.Secret, bool, error) {
		if p.Type == config.Pleasant {
			return masterOnce(retry)
		}
		if !retry {
			if pw, ok, ferr := keyring.Fetch(p.Name); ferr == nil && ok {
				return pw, false, nil
			}
		}
		if !onTTY() {
			return secret.Secret{}, false, nil
		}
		if retry {
			fmt.Fprintf(os.Stderr, "wrong password for %s — try again\n", p.Name)
		}
		pw, perr := promptPassword(fmt.Sprintf("password for %s: ", p.Name))
		if perr != nil {
			return secret.Secret{}, false, perr
		}
		return pw, true, nil
	}
	return session.Open(cfg, ask), cfg, nil
}

func warnExcluded(res *session.Result) {
	for _, ex := range res.Excluded {
		fmt.Fprintf(os.Stderr, "warning: source %q excluded (%s) — results may be incomplete\n", ex.Source, ex.Reason)
	}
}
