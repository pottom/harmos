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
	want := []string{"admin", "administrator", "db-admin", "a-d-m-i-n", "svc-x", "zzz", "www"}
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
