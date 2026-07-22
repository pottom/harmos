package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/theme"
)

// display-width aware helpers (spec §8a — never len()).
func dw(s string) int { return ansi.StringWidth(s) }

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func pad(s string, w int) string {
	s = trunc(s, w)
	if d := dw(s); d < w {
		return s + strings.Repeat(" ", w-d)
	}
	return s
}

func rule(w int) string { return theme.Rule.Render(strings.Repeat("─", max(1, w))) }

func highlight(s, q string, base lipgloss.Style) string {
	if q == "" {
		return base.Render(s)
	}
	i := strings.Index(strings.ToLower(s), strings.ToLower(q))
	if i < 0 {
		return base.Render(s)
	}
	j := i + len(q)
	return base.Render(s[:i]) + theme.Hi.Render(s[i:j]) + base.Render(s[j:])
}

func (m Model) View() string {
	if m.w < 40 || m.h < 10 {
		return m.tooSmall()
	}
	if m.help {
		return m.helpView()
	}
	top := m.searchLine() + "\n" + rule(m.w) + "\n"
	bottom := "\n" + rule(m.w) + "\n" + m.countdown() + "\n" + m.hints()
	rows := m.h - 6
	if rows < 1 {
		rows = 1
	}

	var body string
	switch {
	case m.input.Value() == "":
		body = m.console(rows)
	case m.peek && m.w >= 100:
		body = m.split(rows)
	case m.peek:
		body = m.detailScreen(rows)
	default:
		body = m.list(m.w, rows)
	}
	return top + body + bottom
}

func (m Model) searchLine() string {
	left := theme.Acc.Render("⌕  ") + m.input.View()
	right := theme.Dimmed.Render(fmt.Sprintf("%d matches", len(m.results)))
	if m.input.Value() == "" {
		right = theme.Faded.Render(fmt.Sprintf("%d sources", len(m.sources)))
	}
	gap := m.w - dw(left) - dw(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) console(rows int) string {
	b := []string{theme.Dimmed.Render("  your sources"), ""}
	for _, s := range m.sources {
		b = append(b, "  "+theme.Strong.Render(pad(s.name, 14))+theme.Dimmed.Render(fmt.Sprintf("%d entries", s.count)))
	}
	b = append(b, "", "  "+theme.Faded.Render("start typing to search everything"))
	for len(b) < rows {
		b = append(b, "")
	}
	if len(b) > rows {
		b = b[:rows]
	}
	return strings.Join(b, "\n")
}

func (m Model) list(w, rows int) string {
	if len(m.results) == 0 {
		return theme.Dimmed.Render(pad("  nothing matches — esc clears", w))
	}
	start := 0
	if m.sel >= rows {
		start = m.sel - rows + 1
	}
	end := start + rows
	if end > len(m.results) {
		end = len(m.results)
	}
	q := m.input.Value()
	titleW, userW := 22, 14
	if w < 70 {
		userW = 0
	}

	var lines []string
	for i := start; i < end; i++ {
		e := m.results[i].Entry
		prov := e.Source + " · " + e.Path
		provW := w - 3 - titleW - userW
		if userW > 0 {
			provW--
		}
		if provW < 6 {
			provW = 6
		}
		if i == m.sel {
			line := " → " + pad(e.Title, titleW) + " " + pad(prov, provW)
			if userW > 0 {
				line += " " + pad(e.Username, userW)
			}
			lines = append(lines, theme.SelRow.Width(w).Render(trunc(line, w)))
			continue
		}
		seg := "   " + highlight(pad(e.Title, titleW), q, theme.Strong) + " " + theme.Dimmed.Render(pad(prov, provW))
		if userW > 0 {
			seg += " " + theme.Acc.Render(pad(e.Username, userW))
		}
		lines = append(lines, seg)
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) detail(w int) string {
	if len(m.results) == 0 {
		return ""
	}
	e := m.results[m.sel].Entry
	field := func(l, v string, st lipgloss.Style) string {
		return theme.Dimmed.Render(pad(l, 10)) + st.Render(trunc(v, max(4, w-10)))
	}
	pw := theme.Acc.Render(strings.Repeat("•", 11)) + theme.Dimmed.Render("   ctrl+r reveal")
	if m.reveal {
		pw = theme.Hi.Render(trunc(e.Password.Reveal(), max(4, w-16))) + theme.Dimmed.Render("  ctrl+r hide")
	}
	lines := []string{
		theme.Strong.Render(trunc(e.Title, w)),
		theme.Dimmed.Render(trunc(e.Source+" · "+e.Path, w)),
		"",
		field("Username", e.Username, theme.Strong),
		theme.Dimmed.Render(pad("Password", 10)) + pw,
	}
	if e.URL != "" {
		lines = append(lines, field("URL", e.URL, theme.Dimmed))
	}
	if len(e.Tags) > 0 {
		lines = append(lines, field("Tags", strings.Join(e.Tags, ", "), theme.Dimmed))
	}
	lines = append(lines, "",
		theme.Dimmed.Render(pad("copy", 10))+theme.Dimmed.Render("↵ pw · ")+theme.Acc.Render("ctrl+u")+theme.Dimmed.Render(" user · ")+theme.Acc.Render("ctrl+o")+theme.Dimmed.Render(" url"))
	return strings.Join(lines, "\n")
}

func (m Model) split(rows int) string {
	leftW := m.w*3/5 - 2
	rightW := m.w - leftW - 3
	sep := theme.Rule.Render(" │ ")
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Height(rows).Render(m.list(leftW, rows)),
		sep,
		lipgloss.NewStyle().Width(rightW).Height(rows).Render(m.detail(rightW)),
	)
}

func (m Model) detailScreen(rows int) string {
	lines := strings.Split(m.detail(m.w), "\n")
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) countdown() string {
	if m.remaining <= 0 {
		return theme.Faded.Render(trunc("  clipboard empty", m.w))
	}
	what := theme.Dimmed.Render("copied ") + theme.Hi.Render(m.copiedWhat) + theme.Dimmed.Render(" · ")
	total := int(m.timeout.Seconds())
	if total < 1 {
		total = 1
	}
	barW := m.w - 40
	if barW < 6 {
		return theme.Acc.Render(fmt.Sprintf(" %ds ", m.remaining)) + what + theme.Strong.Render(trunc(m.copied, max(4, m.w-20)))
	}
	filled := m.remaining * barW / total
	if filled > barW {
		filled = barW
	}
	sec := theme.Acc
	if m.remaining <= 5 {
		sec = theme.Bad
	}
	bar := theme.Acc.Render("▐"+strings.Repeat("█", filled)) + theme.Faded.Render(strings.Repeat("░", barW-filled))
	return " " + bar + sec.Render(fmt.Sprintf(" %2ds ", m.remaining)) + " " + what + theme.Strong.Render(trunc(m.copied, max(4, 26)))
}

func (m Model) hints() string {
	var full string
	switch {
	case m.input.Value() == "":
		full = "type to search · ? keys · ctrl+c quit"
	case m.peek:
		full = "↵ copy · ctrl+r reveal · ctrl+u user · ctrl+o url · esc back"
	default:
		full = "↵ copy · ⇥ peek · esc clear · ? keys"
	}
	return theme.Faded.Render(trunc(full, m.w))
}

func (m Model) tooSmall() string {
	msg := lipgloss.JoinVertical(lipgloss.Center,
		theme.Brand.Render("harmos"), "",
		theme.Strong.Render("Terminal too small"), "",
		theme.Bad.Render(fmt.Sprintf("%d × %d", m.w, m.h))+theme.Dimmed.Render(" — need ")+theme.Strong.Render("40 × 10"), "",
		theme.Faded.Render("Widen the window."),
	)
	return lipgloss.Place(max(1, m.w), max(1, m.h), lipgloss.Center, lipgloss.Center, msg)
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"type", "search everything, live"},
		{"↑ / ↓", "move selection"},
		{"enter", "copy password + start countdown"},
		{"tab / →", "peek: expand into detail"},
		{"esc", "back from peek · clear search"},
		{"ctrl+r", "reveal password (in peek)"},
		{"ctrl+u / o", "copy username / url"},
		{"?", "toggle this help"},
		{"ctrl+c", "quit (clears the clipboard)"},
	}
	var b strings.Builder
	b.WriteString(theme.Brand.Render("harmos") + theme.Dimmed.Render("  keys") + "\n\n")
	for _, r := range rows {
		b.WriteString("  " + theme.Acc.Render(pad(r[0], 12)) + theme.Dimmed.Render(r[1]) + "\n")
	}
	b.WriteString("\n" + theme.Faded.Render("  any key closes this"))
	return lipgloss.Place(max(1, m.w), max(1, m.h), lipgloss.Center, lipgloss.Center, b.String())
}
