package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestGenCommand(t *testing.T) {
	var buf bytes.Buffer
	cmd := newGenCmd()
	cmd.SetOut(&buf)
	// --config points nowhere so we exercise the built-in defaults, never the
	// developer's real config.
	cmd.SetArgs([]string{"--count", "3", "--length", "24", "--config", "/no/such/config.toml"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(got) != 3 {
		t.Fatalf("want 3 passwords, got %d: %q", len(got), buf.String())
	}
	for _, p := range got {
		if len([]rune(p)) != 24 {
			t.Errorf("length = %d, want 24 (%q)", len([]rune(p)), p)
		}
		for _, r := range p {
			if r <= ' ' {
				t.Errorf("password contains whitespace/control %q: %q", r, p)
			}
		}
	}
}

func TestGenExclude(t *testing.T) {
	var buf bytes.Buffer
	cmd := newGenCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"-n", "40", "-x", "aeiou", "-c", "5", "--config", "/no/such/config.toml"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, p := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.ContainsAny(p, "aeiou") {
			t.Errorf("excluded chars leaked into %q", p)
		}
	}
}

func TestGenNoClassesErrors(t *testing.T) {
	cmd := newGenCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--no-lower", "--no-upper", "--no-digit", "--no-symbol", "--config", "/no/such/config.toml"})
	if err := cmd.Execute(); err == nil {
		t.Error("disabling every class should error")
	}
}
