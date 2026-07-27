package search

import (
	"fmt"
	"testing"

	"github.com/pottom/harmos/internal/vault"
)

func titles(res []Result) []string {
	out := make([]string, len(res))
	for i, r := range res {
		out[i] = r.Entry.Title
	}
	return out
}

func TestSearchFieldsAndBadges(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "runbook"}, // title
		{Title: "b", URL: "https://runbook.example.com"},                                                  // url
		{Title: "c", Custom: []vault.Field{{Name: "Environment", Value: "runbook-prod"}}},                 // field value
		{Title: "d", Custom: []vault.Field{{Name: "Recovery", Value: "runbook-secret", Protected: true}}}, // protected → value not searched
		{Title: "e", Notes: "restore the runbook from the wiki"},                                          // notes
	})
	field, score := map[string]string{}, map[string]int{}
	for _, r := range m.Match("runbook") {
		field[r.Entry.Title] = r.Field
		score[r.Entry.Title] = r.Score
	}

	if field["runbook"] != "" {
		t.Errorf("a title match should have no field badge, got %q", field["runbook"])
	}
	if field["b"] != "url" {
		t.Errorf("b should match on url, got %q", field["b"])
	}
	if field["c"] != "Environment" {
		t.Errorf("c should match the Environment field, got %q", field["c"])
	}
	if field["e"] != "notes" {
		t.Errorf("e should match on notes, got %q", field["e"])
	}
	if _, ok := field["d"]; ok {
		t.Error("a protected field's value must not be searched")
	}
	// ranking: title < url < field < notes
	if score["runbook"] >= score["b"] || score["b"] >= score["c"] || score["c"] >= score["e"] {
		t.Errorf("field ranking wrong: %v", score)
	}
}

// A protected field is still findable by its name.
func TestSearchProtectedFieldByName(t *testing.T) {
	m := New([]vault.Entry{{Title: "x", Custom: []vault.Field{{Name: "Recovery PIN", Value: "8842", Protected: true}}}})
	res := m.Match("recovery")
	if len(res) != 1 || res[0].Field != "Recovery PIN" {
		t.Errorf("protected field should match by name, got %+v", res)
	}
}

func TestRankingOrder(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "admin"},                                 // exact
		{Title: "administrator"},                         // prefix
		{Title: "db-admin"},                              // substring
		{Title: "a-d-m-i-n"},                             // fuzzy subsequence
		{Title: "svc-x", Username: "admin-user"},         // username
		{Title: "zzz", Path: "Infra/admin-things"},       // path
		{Title: "www", URL: "https://admin.example.com"}, // url
		{Title: "unrelated"},                             // no match
	})

	got := titles(m.Match("admin"))
	// A fuzzy subsequence ("a-d-m-i-n") is the weakest signal — it ranks below every
	// real substring match (username, path, url), not above them.
	want := []string{"admin", "administrator", "db-admin", "svc-x", "zzz", "www", "a-d-m-i-n"}
	if len(got) != len(want) {
		t.Fatalf("got %d results %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rank %d = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestMatchPositions(t *testing.T) {
	m := New([]vault.Entry{{Title: "db-admin"}})
	res := m.Match("admin")
	if len(res) != 1 {
		t.Fatalf("got %d results", len(res))
	}
	// "admin" starts at rune index 3 in "db-admin"
	want := []int{3, 4, 5, 6, 7}
	if fmt.Sprint(res[0].Matched) != fmt.Sprint(want) {
		t.Errorf("matched = %v, want %v", res[0].Matched, want)
	}
}

func TestUnicodePositions(t *testing.T) {
	m := New([]vault.Entry{{Title: "őrző-kód"}})
	res := m.Match("kód")
	if len(res) != 1 || res[0].Score != scoreSub {
		t.Fatalf("unexpected: %+v", res)
	}
	// rune indices, not byte indices: "kód" starts at rune 5 in "őrző-kód"
	want := []int{5, 6, 7}
	if fmt.Sprint(res[0].Matched) != fmt.Sprint(want) {
		t.Errorf("matched = %v, want %v", res[0].Matched, want)
	}
}

func TestEmptyQueryReturnsAllAlphabetical(t *testing.T) {
	m := New([]vault.Entry{{Title: "gamma"}, {Title: "alpha"}, {Title: "beta"}})
	got := titles(m.Match(""))
	want := []string{"alpha", "beta", "gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("empty[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCaseInsensitive(t *testing.T) {
	m := New([]vault.Entry{{Title: "DB-Prod"}})
	if len(m.Match("db-prod")) != 1 || len(m.Match("DB-PROD")) != 1 {
		t.Error("matching should be case-insensitive")
	}
}

func TestNoMatchExcluded(t *testing.T) {
	m := New([]vault.Entry{{Title: "alpha"}, {Title: "beta"}})
	if got := m.Match("zzz"); len(got) != 0 {
		t.Errorf("expected no matches, got %v", titles(got))
	}
}

func TestTieBreakShorterTitleFirst(t *testing.T) {
	// both are prefix matches for "db"; the shorter, closer title ranks first
	m := New([]vault.Entry{{Title: "db-staging-replica"}, {Title: "db"}})
	got := titles(m.Match("db"))
	if got[0] != "db" {
		t.Errorf("expected 'db' first, got %v", got)
	}
}

// §8b requires the benchmark in the repo: prove the linear scan is fast on a
// realistic corpus before anyone reaches for an index.
func BenchmarkMatch10k(b *testing.B) {
	es := make([]vault.Entry, 10000)
	for i := range es {
		es[i] = vault.Entry{
			Title:    fmt.Sprintf("service-%05d-admin", i),
			Username: "svc_admin",
			Path:     "Infra/db",
			URL:      "https://service.example.internal:8443/endpoint",
			Tags:     []string{"infra", "prod"},
		}
	}
	m := New(es)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.Match("admin")
	}
}
