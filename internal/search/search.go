// Package search ranks vault entries against a query. It is the one matcher
// shared by the TUI and the headless CLI (harmos ls / get), so both rank
// identically (spec §8b) — build it here, where it is testable without a
// terminal.
//
// No index (spec §8b): entries are flattened once, their fields lowercased once,
// and every query is a synchronous linear scan. A vault is hundreds to ~10k
// entries; a scan is sub-millisecond and has none of an index's staleness bugs.
package search

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pottom/harmos/internal/vault"
)

// Ranking tiers, best (lowest) to worst (spec §8b). A plain fuzzy match with no
// ranking buries the exact hit under noise — this is what keeps it from feeling
// bad.
const (
	scoreExact  = 0  // title == query
	scorePrefix = 10 // title starts with query
	scoreSub    = 20 // title contains query
	scoreFuzzy  = 30 // title contains query as a subsequence
	scoreUser   = 40 // username contains query
	scorePath   = 50 // folder path or tags contain query
	scoreURL    = 60 // url contains query
	scoreAll    = 70 // empty query: everything, alphabetical

	noMatch = -1
)

// Result is one ranked hit. Matched holds the rune indices in Entry.Title that
// the query matched, for highlighting; it is nil for non-title matches.
type Result struct {
	Entry   vault.Entry
	Score   int
	Matched []int
}

type indexed struct {
	v                            vault.Entry
	title, user, url, path, tags string // lowercased once, never in the loop
}

// Matcher holds the flattened, pre-lowercased entries.
type Matcher struct {
	entries []indexed
}

// New flattens the entries and precomputes lowercased copies once.
func New(entries []vault.Entry) *Matcher {
	es := make([]indexed, len(entries))
	for i, e := range entries {
		es[i] = indexed{
			v:     e,
			title: strings.ToLower(e.Title),
			user:  strings.ToLower(e.Username),
			url:   strings.ToLower(e.URL),
			path:  strings.ToLower(e.Path),
			tags:  strings.ToLower(strings.Join(e.Tags, " ")),
		}
	}
	return &Matcher{entries: es}
}

// FromVaults builds a Matcher over every entry in the given vaults.
func FromVaults(vaults ...*vault.Vault) *Matcher {
	var all []vault.Entry
	for _, vl := range vaults {
		all = append(all, vl.Entries...)
	}
	return New(all)
}

// Match returns the ranked results for query. An empty query returns every
// entry, alphabetical by title. Ties within a tier break by shorter title (the
// closer match), then alphabetically — stable and predictable.
func (m *Matcher) Match(query string) []Result {
	q := strings.ToLower(strings.TrimSpace(query))

	var res []Result
	if q == "" {
		res = make([]Result, len(m.entries))
		for i, e := range m.entries {
			res[i] = Result{Entry: e.v, Score: scoreAll}
		}
	} else {
		for _, e := range m.entries {
			if score, matched := scoreEntry(e, q); score != noMatch {
				res = append(res, Result{Entry: e.v, Score: score, Matched: matched})
			}
		}
	}

	sort.SliceStable(res, func(i, j int) bool {
		a, b := res[i], res[j]
		if a.Score != b.Score {
			return a.Score < b.Score
		}
		// within a real match tier, the shorter (closer) title ranks first;
		// an empty query is plain alphabetical.
		if a.Score != scoreAll {
			if la, lb := len(a.Entry.Title), len(b.Entry.Title); la != lb {
				return la < lb
			}
		}
		return a.Entry.Title < b.Entry.Title
	})
	return res
}

func scoreEntry(e indexed, q string) (int, []int) {
	qr := []rune(q)
	switch {
	case e.title == q:
		return scoreExact, seq(0, len(qr))
	case strings.HasPrefix(e.title, q):
		return scorePrefix, seq(0, len(qr))
	}
	if i := strings.Index(e.title, q); i >= 0 {
		start := utf8.RuneCountInString(e.title[:i])
		return scoreSub, seq(start, len(qr))
	}
	if pos, ok := subsequence(e.title, qr); ok {
		return scoreFuzzy, pos
	}
	switch {
	case strings.Contains(e.user, q):
		return scoreUser, nil
	case strings.Contains(e.path, q), strings.Contains(e.tags, q):
		return scorePath, nil
	case strings.Contains(e.url, q):
		return scoreURL, nil
	}
	return noMatch, nil
}

// subsequence reports whether qr appears in s in order, and where (rune indices
// in s). Both are already lowercased.
func subsequence(s string, qr []rune) ([]int, bool) {
	sr := []rune(s)
	pos := make([]int, 0, len(qr))
	j := 0
	for i := 0; i < len(sr) && j < len(qr); i++ {
		if sr[i] == qr[j] {
			pos = append(pos, i)
			j++
		}
	}
	if j == len(qr) {
		return pos, true
	}
	return nil, false
}

func seq(start, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = start + i
	}
	return out
}
