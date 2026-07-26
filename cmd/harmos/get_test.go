package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// captureStderr runs fn with os.Stderr redirected to a buffer and returns what
// it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestEmitEntryQuiet(t *testing.T) {
	e := vault.Entry{Source: "s", Path: "p", Username: "u", Password: secret.New("pw")}

	var out bytes.Buffer
	stderr := captureStderr(t, func() {
		_ = emitEntry(e, "ref", false, false, true, &config.Config{}, &out)
	})
	if stderr != "" {
		t.Errorf("--quiet should write nothing to stderr, got %q", stderr)
	}
	if strings.TrimSpace(out.String()) != "pw" {
		t.Errorf("stdout should be just the password, got %q", out.String())
	}

	// without --quiet, the provenance line is on stderr
	out.Reset()
	stderr = captureStderr(t, func() {
		_ = emitEntry(e, "ref", false, false, false, &config.Config{}, &out)
	})
	if !strings.Contains(stderr, "s · p · u") {
		t.Errorf("non-quiet should print provenance to stderr, got %q", stderr)
	}
}

func TestResolveByPath(t *testing.T) {
	ents := []vault.Entry{
		{Source: "s", Path: "a/b", Title: "X", Username: "u1", Password: secret.New("p1")},
		{Source: "s", Path: "a/b", Title: "X", Username: "u2", Password: secret.New("p2")}, // dup path
		{Source: "s", Path: "", Title: "Y", Username: "u3", Password: secret.New("p3")},
	}
	if e, err := resolveByPath(ents, "s/Y", ""); err != nil || e.Username != "u3" {
		t.Errorf("s/Y should resolve to u3, got %+v / %v", e, err)
	}
	if _, err := resolveByPath(ents, "s/a/b/X", ""); err == nil {
		t.Error("a duplicated path must be ambiguous without --user")
	}
	if e, err := resolveByPath(ents, "s/a/b/X", "u2"); err != nil || e.Password.Reveal() != "p2" {
		t.Errorf("--user u2 should resolve, got %+v / %v", e, err)
	}
	if _, err := resolveByPath(ents, "s/nope", ""); err == nil {
		t.Error("a missing path must error")
	}
}

func TestEmitOTP(t *testing.T) {
	e := vault.Entry{
		Source: "s", Path: "p",
		TOTP: "otpauth://totp/s:x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&digits=6&period=30",
	}
	var buf bytes.Buffer
	if err := emitOTP(e, "x", false, false, time.Second, &buf); err != nil {
		t.Fatal(err)
	}
	code := strings.TrimSpace(buf.String())
	if len(code) != 6 {
		t.Errorf("want a 6-digit code on stdout, got %q", code)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Errorf("code %q is not all digits", code)
		}
	}
}

func TestEmitOTPNoTOTP(t *testing.T) {
	var buf bytes.Buffer
	if err := emitOTP(vault.Entry{Source: "s"}, "x", false, false, time.Second, &buf); err == nil {
		t.Error("emitOTP on an entry without a TOTP should error")
	}
}
