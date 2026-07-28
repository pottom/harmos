package edit

import (
	"strings"
	"testing"
	"time"

	"github.com/pottom/harmos/internal/secret"
)

// The one rule this package must never break. The Changes tab is on screen, in
// the scrollback, and in whatever the terminal is logging — a password there
// would undo every other precaution in the program.
func TestDiffNeverContainsSecrets(t *testing.T) {
	// Deliberately prose rather than anything key-shaped: a realistic-looking
	// literal here trips the secret scanner, and teaching the scanner to ignore
	// a password-shaped constant is a worse trade than picking a duller one. The
	// assertion is that these strings do not appear in the output, so their form
	// does not matter.
	const (
		oldPassword = "the first made up passphrase"
		newPassword = "the second made up passphrase"
		oldSecret   = "recovery-code-11111"
		newSecret   = "recovery-code-22222"
		totp        = "otpauth://totp/x?secret=GEZDGNBVGY3TQOJQ"
	)

	before := &Draft{
		Title:    "db-prod",
		Password: secret.New(oldPassword),
		TOTP:     totp,
		Fields: []DraftField{
			{Key: "Recovery", Value: oldSecret, Protected: true},
			{Key: "Env", Value: "prod"},
		},
	}
	after := &Draft{
		Title:    "db-prod",
		Password: secret.New(newPassword),
		TOTP:     totp + "&digits=8",
		Fields: []DraftField{
			{Key: "Recovery", Value: newSecret, Protected: true},
			{Key: "Env", Value: "staging"},
		},
	}

	s := stage(Op{Kind: EditEntry, Target: "e1", Before: before, After: after})

	var rendered strings.Builder
	for _, c := range s.Diff() {
		rendered.WriteString(c.Title + " " + c.Detail + "\n")
		for _, l := range c.Lines {
			rendered.WriteString(l.Field + " " + l.Old + " → " + l.New + "\n")
		}
	}
	out := rendered.String()

	for _, forbidden := range []string{oldPassword, newPassword, oldSecret, newSecret, "GEZDGNBVGY3TQOJQ"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("a secret reached the diff: %q\n%s", forbidden, out)
		}
	}

	// It still has to say that they changed, or the review is useless.
	for _, want := range []string{"Password", "TOTP", "Recovery"} {
		if !strings.Contains(out, want) {
			t.Errorf("the diff should report that %s changed:\n%s", want, out)
		}
	}
	// And an unprotected field is shown in full, because that is the point.
	if !strings.Contains(out, "prod") || !strings.Contains(out, "staging") {
		t.Errorf("an ordinary field should be shown:\n%s", out)
	}
}

func TestDiffReportsFieldLevelChanges(t *testing.T) {
	before := &Draft{Title: "old", Username: "alice", URL: "https://a", Tags: "one"}
	after := &Draft{Title: "new", Username: "alice", URL: "", Tags: "one;two"}

	s := stage(Op{Kind: EditEntry, Target: "e1", Before: before, After: after})
	lines := s.Diff()[0].Lines

	got := map[string]Line{}
	for _, l := range lines {
		got[l.Field] = l
	}

	if l, ok := got["Title"]; !ok || l.Op != LineChanged || l.Old != "old" || l.New != "new" {
		t.Errorf("title line = %+v", l)
	}
	if _, ok := got["Username"]; ok {
		t.Error("an unchanged field should not appear in the diff")
	}
	if l, ok := got["URL"]; !ok || l.Op != LineRemoved {
		t.Errorf("a cleared field should read as removed, got %+v", l)
	}
	if l, ok := got["Tags"]; !ok || l.Op != LineChanged {
		t.Errorf("tags line = %+v", l)
	}
}

func TestDiffOfACreationShowsEverything(t *testing.T) {
	s := stage(Op{
		Kind:   CreateEntry,
		Target: "e1",
		Parent: "g1",
		After:  &Draft{Title: "brand new", Username: "someone"},
	})
	c := s.Diff()[0]
	if c.State != New {
		t.Errorf("state = %v, want New", c.State)
	}
	if len(c.Lines) != 2 {
		t.Errorf("a creation should list its non-empty fields, got %+v", c.Lines)
	}
	for _, l := range c.Lines {
		if l.Op != LineAdded {
			t.Errorf("creation line %q should read as added, got %v", l.Field, l.Op)
		}
	}
}

func TestDiffDescribesMovesAndDeletes(t *testing.T) {
	s := stage(
		Op{Kind: MoveEntry, Target: "e1", Parent: "g2", Before: &Draft{Title: "moved"}},
		Op{Kind: DeleteEntry, Target: "e2", Perm: false, Before: &Draft{Title: "binned"}},
		Op{Kind: DeleteEntry, Target: "e3", Perm: true, Before: &Draft{Title: "gone"}},
	)
	byTitle := map[string]Change{}
	for _, c := range s.Diff() {
		byTitle[c.Title] = c
	}

	if !strings.Contains(byTitle["binned"].Detail, "recycle bin") {
		t.Errorf("a bin delete should say so: %q", byTitle["binned"].Detail)
	}
	if !strings.Contains(byTitle["gone"].Detail, "permanently") {
		t.Errorf("a permanent delete should say so: %q", byTitle["gone"].Detail)
	}
	if byTitle["moved"].Detail == "" {
		t.Error("a move should carry a description")
	}
}

func TestDiffFlattensLongNotes(t *testing.T) {
	long := strings.Repeat("word ", 100)
	s := stage(Op{
		Kind:   EditEntry,
		Target: "e1",
		Before: &Draft{Notes: "before"},
		After:  &Draft{Notes: "line one\nline two\n" + long},
	})
	for _, l := range s.Diff()[0].Lines {
		if l.Field != "Notes" {
			continue
		}
		if strings.Contains(l.New, "\n") {
			t.Error("a diff line must not contain a newline; it would break the layout")
		}
		if len(l.New) > 130 {
			t.Errorf("a long note should be truncated for the diff, got %d chars", len(l.New))
		}
	}
}

func TestDiffReportsExpiry(t *testing.T) {
	when := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	s := stage(Op{
		Kind:   EditEntry,
		Target: "e1",
		Before: &Draft{},
		After:  &Draft{Expires: true, ExpiryTime: when},
	})
	found := false
	for _, l := range s.Diff()[0].Lines {
		if l.Field == "Expiry" {
			found = true
			if l.Old != "never" || !strings.Contains(l.New, "2027-03-01") {
				t.Errorf("expiry line = %+v", l)
			}
		}
	}
	if !found {
		t.Error("setting an expiry should show in the diff")
	}
}

func TestEmptySetSummary(t *testing.T) {
	var s Set
	if !s.Empty() {
		t.Error("a new set should be empty")
	}
	if s.Summary() != "nothing pending" {
		t.Errorf("summary = %q", s.Summary())
	}
	if len(s.Diff()) != 0 {
		t.Error("an empty set has no changes to show")
	}
}
