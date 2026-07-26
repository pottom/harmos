package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/pottom/harmos/internal/vault"
)

func TestEmitOTP(t *testing.T) {
	e := vault.Entry{
		Source: "s", Path: "p",
		TOTP: "otpauth://totp/s:x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&digits=6&period=30",
	}
	var buf bytes.Buffer
	if err := emitOTP(e, "x", false, time.Second, &buf); err != nil {
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
	if err := emitOTP(vault.Entry{Source: "s"}, "x", false, time.Second, &buf); err == nil {
		t.Error("emitOTP on an entry without a TOTP should error")
	}
}
