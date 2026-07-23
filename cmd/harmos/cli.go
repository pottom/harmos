package main

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/session"
)

func loadConfigAt(path string) (*config.Config, error) {
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	return config.Load(path)
}

func onTTY() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// needsMaster reports whether any source requires the harmos master password
// (only Pleasant caches do).
func needsMaster(cfg *config.Config) bool {
	for _, p := range cfg.Profiles {
		if p.Type == config.Pleasant {
			return true
		}
	}
	return false
}

// resolveMaster gets the harmos master password: from HARMOS_MASTER for scripts
// (non-TTY), else a prompt on a terminal. It is never read from the config file.
func resolveMaster() (secret.Secret, error) {
	if v, ok := os.LookupEnv("HARMOS_MASTER"); ok {
		return secret.New(v), nil
	}
	if onTTY() {
		return promptPassword("harmos master password: ")
	}
	return secret.Secret{}, fmt.Errorf("no terminal to prompt for the master password; set HARMOS_MASTER for scripting")
}

// openAll loads the config, resolves the master, and opens every source. A kdbx
// source's own password comes from a prompt on a TTY; with no TTY a
// password-protected external file is simply excluded (spec §2a).
func openAll(configPath string) (*session.Result, *config.Config, error) {
	cfg, err := loadConfigAt(configPath)
	if err != nil {
		return nil, nil, err
	}
	// The master password unlocks Pleasant caches only; a config with just local
	// kdbx sources never needs it (spec §2a).
	var master secret.Secret
	if needsMaster(cfg) {
		if master, err = resolveMaster(); err != nil {
			return nil, nil, err
		}
	}
	ask := func(p config.Profile) (secret.Secret, error) {
		if onTTY() {
			return promptPassword(fmt.Sprintf("password for %s: ", p.Name))
		}
		return secret.Secret{}, nil
	}
	return session.Open(cfg, master, ask), cfg, nil
}

func warnExcluded(res *session.Result) {
	for _, ex := range res.Excluded {
		fmt.Fprintf(os.Stderr, "warning: source %q excluded (%s) — results may be incomplete\n", ex.Source, ex.Reason)
	}
}
