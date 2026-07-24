package main

import (
	"fmt"

	"github.com/pottom/harmos/internal/tui"
)

// runTUI unlocks every source and launches the interface. The TUI needs a
// terminal; scripts use `harmos ls` / `get` (spec §8a).
func runTUI(configPath string) error {
	if !onTTY() {
		return fmt.Errorf("the TUI needs a terminal; use `harmos ls` or `harmos get` for scripts")
	}
	cfgPath, err := configPathOrDefault(configPath)
	if err != nil {
		return err
	}
	res, cfg, err := openAll(cfgPath)
	if err != nil {
		return err
	}
	warnExcluded(res)
	if len(res.Entries) == 0 {
		return fmt.Errorf("no entries — run `harmos sync` first, or check your config")
	}
	applyConfiguredTheme(cfg, cfgPath)
	return tui.Run(res.Entries, cfgPath, cfg.ClipboardTimeout.Duration)
}
