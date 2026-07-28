package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

func TestGetCommand(t *testing.T) {
	ents := []vault.Entry{
		{Source: "4ig", Path: "F/S", Title: "ANDOC", Username: "admin", Password: secret.New("p")},
		{Source: "4ig", Path: "F", Title: "Atlas", Username: "a", Password: secret.New("p")},
		{Source: "4ig", Path: "F", Title: "Atlas", Username: "b", Password: secret.New("p")}, // dup path
	}
	m := New(ents, nil, "", 30*time.Second)

	if got := m.getCommand(&ents[0]); got != "harmos get --path '4ig/F/S/ANDOC' --quiet" {
		t.Errorf("unique entry: got %q", got)
	}
	got := m.getCommand(&ents[1])
	if !strings.Contains(got, "--path '4ig/F/Atlas'") || !strings.Contains(got, "--user 'a'") || !strings.HasSuffix(got, "--quiet") {
		t.Errorf("duplicate path should add --user and --quiet, got %q", got)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("HS Expert/x"); got != "'HS Expert/x'" {
		t.Errorf("space value: got %q", got)
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("embedded quote: got %q", got)
	}
}
