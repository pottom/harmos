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

	"github.com/pottom/harmos/internal/vault"
)

// Ranking tiers, best (lowest) to worst (spec §8b). A plain fuzzy match with no
// ranking buries the exact hit under noise — this is what keeps it from feeling
// bad.
const (
	scoreExact  = 0  // title == query
	scorePrefix = 10 // title starts with query
	scoreSub    = 20 // title contains query
	scoreUser   = 40 // username contains query
	scoreTags   = 45 // a tag contains query
	scorePath   = 50 // folder path contains query
	scoreURL    = 60 // url contains query
	scoreField  = 65 // a custom field name/value contains query
	scoreNotes  = 70 // notes contain query
	scoreFile   = 72 // an attachment's file name contains query
	scoreFuzzy  = 75 // title contains query as a (tight) subsequence — the weakest,
	//                   last-resort signal, below every real substring match, or a
	//                   loose "p…p…k" would bury the entries that actually match.
	scoreAll = 80 // empty query: everything, alphabetical

	noMatch = -1
)

// Result is one ranked hit. Matched holds the rune indices in Entry.Title that
// the query matched, for highlighting (nil for non-title matches). Field names
// where the match was found — "" for a title match, else "user"/"tags"/"path"/
// "url"/"notes" or a custom field's name — for a badge in the results list.
type Result struct {
	Entry   vault.Entry
	Score   int
	Matched []int
	Field   string
}

// idxField is a custom field prepared for search: name/value lowercased, the
// original name kept for the badge. Protected fields are matched by name only,
// so a search never surfaces a secret value.
type idxField struct {
	name, value, display string
	protected            bool
}

type indexed struct {
	v                                          vault.Entry
	title, user, url, path, tags, notes, files string // lowercased once, never in the loop
	fields                                     []idxField
}

// Matcher holds the flattened, pre-lowercased entries.
type Matcher struct {
	entries []indexed
}

// New flattens the entries and precomputes lowercased copies once.
func New(entries []vault.Entry) *Matcher {
	es := make([]indexed, len(entries))
	for i, e := range entries {
		fields := make([]idxField, len(e.Custom))
		for j, f := range e.Custom {
			fields[j] = idxField{
				name:      strings.ToLower(f.Name),
				value:     strings.ToLower(f.Value),
				display:   f.Name,
				protected: f.Protected,
			}
		}
		names := make([]string, len(e.Files))
		for j, f := range e.Files {
			names[j] = f.Name
		}
		es[i] = indexed{
			v:      e,
			title:  strings.ToLower(e.Title),
			user:   strings.ToLower(e.Username),
			url:    strings.ToLower(e.URL),
			path:   strings.ToLower(e.Path),
			tags:   strings.ToLower(strings.Join(e.Tags, " ")),
			notes:  strings.ToLower(e.Notes),
			files:  strings.ToLower(strings.Join(names, " ")),
			fields: fields,
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
	q := Parse(query)

	var res []Result
	if q.Empty() {
		res = make([]Result, len(m.entries))
		for i, e := range m.entries {
			res[i] = Result{Entry: e.v, Score: scoreAll}
		}
	} else {
		for _, e := range m.entries {
			if ok, score, idx, field := q.eval(e); ok {
				res = append(res, Result{Entry: e.v, Score: score, Matched: idx, Field: field})
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
