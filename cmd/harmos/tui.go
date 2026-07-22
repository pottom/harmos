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
	res, cfg, err := openAll(configPath)
	if err != nil {
		return err
	}
	warnExcluded(res)
	if len(res.Entries) == 0 {
		return fmt.Errorf("no entries — run `harmos sync` first, or check your config")
	}
	return tui.Run(res.Entries, cfg.ClipboardTimeout.Duration)
}
