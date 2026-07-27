package search

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// A query is a boolean expression over search terms:
//
//	db prod            title/… contains "db" AND "prod"
//	db | web           "db" OR "web"        (| splits OR-groups; space is AND)
//	url:vault          only the url field contains "vault"
//	ansible -recycle   contains "ansible" but NOT "recycle"
//	"db prod"          the literal phrase "db prod"
//
// Field prefixes: title, user (username), url, notes, tag(s), path, field (any
// custom field). An unknown prefix is treated as part of the value (so a URL's
// "https:" is not mistaken for a field).

type qterm struct {
	field  string // "", title, user, url, notes, tags, path, field
	value  string // lowercased
	negate bool
}

// Query is a parsed search: OR of AND-groups. An entry matches if any group's
// terms are all satisfied.
type Query struct {
	groups [][]qterm
}

// Empty reports whether the query has no terms (matches everything).
func (q Query) Empty() bool { return len(q.groups) == 0 }

// Terms returns the distinct positive term values, for highlighting.
func (q Query) Terms() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range q.groups {
		for _, t := range g {
			if !t.negate && t.value != "" && !seen[t.value] {
				seen[t.value] = true
				out = append(out, t.value)
			}
		}
	}
	return out
}

// HighlightTerms parses query and returns its positive term values — what the UI
// should emphasize in results and fields.
func HighlightTerms(query string) []string { return Parse(query).Terms() }

// Parse turns a raw query string into a Query.
func Parse(raw string) Query {
	var groups [][]qterm
	var cur []qterm
	for _, tk := range tokenize(raw) {
		if tk == "|" {
			if len(cur) > 0 {
				groups = append(groups, cur)
				cur = nil
			}
			continue
		}
		if t, ok := parseTerm(tk); ok {
			cur = append(cur, t)
		}
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return Query{groups: groups}
}

// tokenize splits on whitespace and & (both AND), keeps "quoted phrases" whole,
// and emits | as its own token (OR).
func tokenize(raw string) []string {
	var toks []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range raw {
		switch {
		case r == '"':
			inQuote = !inQuote
		case inQuote:
			cur.WriteRune(r)
		case r == '|':
			flush()
			toks = append(toks, "|")
		case r == '&' || unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

func parseTerm(tok string) (qterm, bool) {
	var t qterm
	if strings.HasPrefix(tok, "-") {
		t.negate = true
		tok = tok[1:]
	}
	if pre, rest, found := strings.Cut(tok, ":"); found && pre != "" {
		if f, ok := normField(pre); ok {
			t.field = f
			tok = rest
		}
	}
	t.value = strings.ToLower(tok)
	return t, t.value != ""
}

// normField maps a field keyword to its canonical name (or ok=false).
func normField(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "title":
		return "title", true
	case "user", "username":
		return "user", true
	case "url":
		return "url", true
	case "notes", "note":
		return "notes", true
	case "tag", "tags":
		return "tags", true
	case "path", "folder":
		return "path", true
	case "field":
		return "field", true
	case "file", "attachment", "attachments":
		return "file", true
	}
	return "", false
}

// eval reports whether e matches the query, with the best (lowest) score, the
// title match indices for that term, and the field badge.
func (q Query) eval(e indexed) (ok bool, score int, idx []int, field string) {
	best := noMatch
	for _, g := range q.groups {
		if gok, gs, gidx, gf := evalGroup(g, e); gok {
			if best == noMatch || gs < best {
				best, idx, field = gs, gidx, gf
			}
		}
	}
	if best == noMatch {
		return false, 0, nil, ""
	}
	return true, best, idx, field
}

func evalGroup(g []qterm, e indexed) (ok bool, score int, idx []int, field string) {
	best := noMatch
	for _, t := range g {
		tier, f, ti, matched := termScore(t, e)
		if t.negate {
			if matched {
				return false, 0, nil, "" // an excluded term is present
			}
			continue
		}
		if !matched {
			return false, 0, nil, "" // a required term is absent
		}
		if best == noMatch || tier < best {
			best, idx, field = tier, ti, f
		}
	}
	if best == noMatch {
		best = scoreAll // a group of only negated terms, all satisfied
	}
	return true, best, idx, field
}

// termScore scores one positive term against an entry: the best field it matches
// (respecting a field: scope), its tier, and the title indices for highlighting.
func termScore(t qterm, e indexed) (tier int, field string, idx []int, ok bool) {
	v := t.value
	switch t.field {
	case "", "title":
		// A real title match (exact / prefix / substring) always wins.
		if tr, ix, hit := titleSubstr(e.title, v); hit {
			return tr, "", ix, true
		}
		if t.field == "title" { // explicit title: allow a tight fuzzy fallback
			if ix, hit := titleFuzzy(e.title, v); hit {
				return scoreFuzzy, "", ix, true
			}
			return noMatch, "", nil, false
		}
		// Bare term: try a substring in every other field before falling back to a
		// fuzzy title, so entries that really contain the term rank above a loose
		// "p…p…k" subsequence hit.
		switch {
		case strings.Contains(e.user, v):
			return scoreUser, "user", nil, true
		case strings.Contains(e.tags, v):
			return scoreTags, "tags", nil, true
		case strings.Contains(e.path, v):
			return scorePath, "path", nil, true
		case strings.Contains(e.url, v):
			return scoreURL, "url", nil, true
		}
		if f, hit := fieldMatch(e, v); hit {
			return scoreField, f, nil, true
		}
		if strings.Contains(e.notes, v) {
			return scoreNotes, "notes", nil, true
		}
		if strings.Contains(e.files, v) {
			return scoreFile, "file", nil, true
		}
		if ix, hit := titleFuzzy(e.title, v); hit { // last resort
			return scoreFuzzy, "", ix, true
		}
	case "user":
		if strings.Contains(e.user, v) {
			return scoreUser, "user", nil, true
		}
	case "url":
		if strings.Contains(e.url, v) {
			return scoreURL, "url", nil, true
		}
	case "notes":
		if strings.Contains(e.notes, v) {
			return scoreNotes, "notes", nil, true
		}
	case "tags":
		if strings.Contains(e.tags, v) {
			return scoreTags, "tags", nil, true
		}
	case "path":
		if strings.Contains(e.path, v) {
			return scorePath, "path", nil, true
		}
	case "field":
		if f, hit := fieldMatch(e, v); hit {
			return scoreField, f, nil, true
		}
	case "file":
		if strings.Contains(e.files, v) {
			return scoreFile, "file", nil, true
		}
	}
	return noMatch, "", nil, false
}

// fieldMatch checks the custom fields: names always, values only when not
// protected (so a search never surfaces a secret).
func fieldMatch(e indexed, v string) (string, bool) {
	for _, f := range e.fields {
		if strings.Contains(f.name, v) || (!f.protected && strings.Contains(f.value, v)) {
			return f.display, true
		}
	}
	return "", false
}

// titleSubstr scores a real (exact / prefix / substring) title match — the strong
// signals. It never does fuzzy matching; that is titleFuzzy's job.
func titleSubstr(title, q string) (tier int, idx []int, ok bool) {
	qr := []rune(q)
	switch {
	case title == q:
		return scoreExact, seq(0, len(qr)), true
	case strings.HasPrefix(title, q):
		return scorePrefix, seq(0, len(qr)), true
	}
	if before, _, found := strings.Cut(title, q); found {
		return scoreSub, seq(utf8.RuneCountInString(before), len(qr)), true
	}
	return noMatch, nil, false
}

// titleFuzzy matches the query as a subsequence of the title, but only a *tight*
// one: at least two chars, and no more filler than query chars inside the matched
// span. Without the tightness gate "ppk" subsequence-matches "GRPPHVC04K" and
// floods the results with noise (a scattered p…p…k), which is exactly what makes
// the search feel useless.
func titleFuzzy(title, q string) (idx []int, ok bool) {
	qr := []rune(q)
	if len(qr) < 2 {
		return nil, false // a single-char subsequence is meaningless
	}
	pos, hit := subsequence(title, qr)
	if !hit {
		return nil, false
	}
	span := pos[len(pos)-1] - pos[0] + 1
	if span-len(qr) > len(qr) { // more filler than matched chars → too loose
		return nil, false
	}
	return pos, true
}
