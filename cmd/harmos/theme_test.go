package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
)

func TestApplyConfiguredTheme(t *testing.T) {
	defer theme.Apply(theme.Charm)

	// a built-in by name
	applyConfiguredTheme(&config.Config{Theme: "gruvbox"}, "")
	if theme.Accent.Dark != "#fe8019" {
		t.Errorf("gruvbox accent = %q", theme.Accent.Dark)
	}

	// a custom themes/<name>.toml next to the config
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "name = \"x\"\n[accent]\ndark = \"#123456\"\n"
	if err := os.WriteFile(filepath.Join(dir, "themes", "x.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	applyConfiguredTheme(&config.Config{Theme: "x"}, filepath.Join(dir, "config.toml"))
	if theme.Accent.Dark != "#123456" {
		t.Errorf("custom accent = %q, want #123456", theme.Accent.Dark)
	}
}
