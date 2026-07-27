package main

import (
	"errors"
	"fmt"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/tui"
)

// runTUI launches the interface, which unlocks every source itself: the master
// password (and any per-source password not already saved) is entered on the
// in-TUI unlock screen, not on stdin. The TUI needs a terminal; scripts use
// `harmos ls` / `get` (spec §8a).
func runTUI(configPath string) error {
	if !onTTY() {
		return fmt.Errorf("the TUI needs a terminal; use `harmos ls` or `harmos get` for scripts")
	}
	cfgPath, err := configPathOrDefault(configPath)
	if err != nil {
		return err
	}
	cfg, err := loadConfigAt(cfgPath)
	if err != nil {
		// No config yet: instead of erroring, open the TUI in a first-run flow that
		// walks the user through adding their first source.
		var nse noSourcesError
		if errors.As(err, &nse) {
			return tui.RunOnboarding(cfgPath, config.DefaultClipboardTimeout)
		}
		return err
	}
	applyConfiguredTheme(cfg, cfgPath)
	return tui.RunLocked(cfg, cfgPath, cfg.ClipboardTimeout.Duration)
}
