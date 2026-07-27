package search

import (
	"testing"

	"github.com/pottom/harmos/internal/vault"
)

func TestQueryAND(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "db prod server"},
		{Title: "db staging"},
		{Title: "web prod"},
	})
	got := titles(m.Match("db prod"))
	if len(got) != 1 || got[0] != "db prod server" {
		t.Errorf("AND should need both terms, got %v", got)
	}
}

func TestQueryOR(t *testing.T) {
	m := New([]vault.Entry{{Title: "db"}, {Title: "web"}, {Title: "other"}})
	got := titles(m.Match("db | web"))
	if len(got) != 2 {
		t.Errorf("OR should match db and web, got %v", got)
	}
}

// (db AND prod) OR web
func TestQueryMixedPrecedence(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "db prod"}, // matches the AND group
		{Title: "db only"}, // db but not prod → no
		{Title: "web app"}, // matches the OR
		{Title: "nope"},
	})
	got := titles(m.Match("db prod | web"))
	if len(got) != 2 {
		t.Errorf("mixed precedence, got %v", got)
	}
}

func TestQueryFieldScope(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "a", URL: "https://vault.example.com"},
		{Title: "vault-thing"}, // "vault" is in the title, not the url
	})
	got := titles(m.Match("url:vault"))
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("url: scope should only match the url field, got %v", got)
	}
}

func TestQueryNegation(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "ansible prod"},
		{Title: "ansible recycle bin"},
	})
	got := titles(m.Match("ansible -recycle"))
	if len(got) != 1 || got[0] != "ansible prod" {
		t.Errorf("-recycle should exclude, got %v", got)
	}
}

func TestQueryPhrase(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "db prod"},
		{Title: "prod db"}, // both words, but not the phrase
	})
	got := titles(m.Match(`"db prod"`))
	if len(got) != 1 || got[0] != "db prod" {
		t.Errorf("a quoted phrase should match the exact sequence, got %v", got)
	}
}

func TestQueryTerms(t *testing.T) {
	terms := HighlightTerms(`db url:vault -recycle "a b"`)
	want := map[string]bool{"db": true, "vault": true, "a b": true}
	if len(terms) != len(want) {
		t.Fatalf("positive terms = %v, want %v", terms, want)
	}
	for _, tm := range terms {
		if !want[tm] {
			t.Errorf("unexpected highlight term %q", tm)
		}
	}
}

func TestQueryFieldBadge(t *testing.T) {
	m := New([]vault.Entry{{Title: "x", URL: "https://runbook.x"}})
	res := m.Match("runbook")
	if len(res) != 1 || res[0].Field != "url" {
		t.Errorf("badge should say url, got %+v", res)
	}
}

// A loose subsequence ("ppk" scattered across a title) must NOT match, or the
// results flood with noise; a real substring (in the URL) must, and reports the
// field so the row can show an excerpt.
func TestFuzzyIsTightAndLastResort(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "GRPPHVC04K"},                         // p…p…k scattered — too loose
		{Title: "gateway", URL: "https://ppk.host/x"}, // real substring in the URL
	})
	res := m.Match("ppk")
	if len(res) != 1 {
		t.Fatalf("only the real match should survive, got %d: %v", len(res), titles(res))
	}
	if res[0].Entry.Title != "gateway" || res[0].Field != "url" {
		t.Errorf("expected the URL substring match, got %+v", res[0])
	}

	// A genuinely tight subsequence still matches (typo/separator tolerance).
	m2 := New([]vault.Entry{{Title: "a-d-m-i-n"}})
	if got := titles(m2.Match("admin")); len(got) != 1 {
		t.Errorf("a tight subsequence should still match, got %v", got)
	}
}

// Search matches attachment file names, bare and via the file: scope.
func TestSearchAttachmentNames(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "jump-host", Files: []vault.Attachment{{Name: "id.ppk"}, {Name: "readme.txt"}}},
		{Title: "other"},
	})
	res := m.Match("id.ppk")
	if len(res) != 1 || res[0].Entry.Title != "jump-host" || res[0].Field != "file" {
		t.Errorf("bare attachment-name match failed: %+v", res)
	}
	if got := titles(m.Match("file:readme")); len(got) != 1 || got[0] != "jump-host" {
		t.Errorf("file: scope should match the attachment name, got %v", got)
	}
	if got := m.Match("file:nope"); len(got) != 0 {
		t.Errorf("file: scope should not match a missing name, got %v", titles(got))
	}
}

// src: narrows the results to one source, on its own and as an AND term next to
// a real query — the "same entry in two sources" case the badge alone can't fix.
func TestQuerySourceScope(t *testing.T) {
	m := New([]vault.Entry{
		{Source: "own", Title: "svc-admin"},
		{Source: "work", Title: "svc-admin"},
		{Source: "own", Title: "db prod"},
	})

	res := m.Match("src:own svc-admin")
	if len(res) != 1 || res[0].Entry.Source != "own" {
		t.Fatalf("src:own svc-admin should keep only the own entry, got %v", titles(res))
	}
	// The real term, not the filter, decides where the hit ranks and what the
	// badge says.
	if res[0].Score != scoreExact || res[0].Field != "" {
		t.Errorf("score/badge should come from the title term, got %d/%q", res[0].Score, res[0].Field)
	}

	if got := titles(m.Match("src:own")); len(got) != 2 || got[0] != "db prod" {
		t.Errorf("src:own alone should list that source alphabetically, got %v", got)
	}
	if got := titles(m.Match("source:work")); len(got) != 1 || got[0] != "svc-admin" {
		t.Errorf("source: is the long form of src:, got %v", got)
	}
	if got := titles(m.Match("svc-admin -src:work")); len(got) != 1 {
		t.Errorf("-src: should exclude a source, got %v", got)
	}
}

// A source term filters; it is not something to look for inside an entry, so it
// must not highlight (it would paint stray hits in unrelated titles).
func TestQuerySourceIsNotHighlighted(t *testing.T) {
	terms := HighlightTerms("src:own svc-admin")
	if len(terms) != 1 || terms[0] != "svc-admin" {
		t.Errorf("source term should not be highlighted, got %v", terms)
	}
}

// A term scoped to a field also drives the AND across different fields.
func TestQueryScopedAND(t *testing.T) {
	m := New([]vault.Entry{
		{Title: "prod", URL: "https://vault.x"},
		{Title: "prod", URL: "https://other.x"},  // no vault in url
		{Title: "stage", URL: "https://vault.x"}, // no prod
	})
	got := titles(m.Match("prod url:vault"))
	if len(got) != 1 {
		t.Errorf("prod AND url:vault, got %v", got)
	}
}
