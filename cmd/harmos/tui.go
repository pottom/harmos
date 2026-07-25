package main

import (
	"fmt"

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
		return err
	}
	if len(cfg.Profiles) == 0 {
		return fmt.Errorf("no sources configured — run `harmos sync` first, or add one in Settings")
	}
	applyConfiguredTheme(cfg, cfgPath)
	return tui.RunLocked(cfg, cfgPath, cfg.ClipboardTimeout.Duration)
}
