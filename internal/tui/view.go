package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/config"
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
	bar := m.tabBar() + "\n"
	switch {
	case m.tab == 1:
		return bar + m.settingsView()
	case m.detail:
		return bar + m.detailView()
	default:
		return bar + m.vaultBody()
	}
}

// tabBar is the top row: the two tabs (active highlighted) and a global hint.
func (m Model) tabBar() string {
	tab := func(n int, name string) string {
		s := fmt.Sprintf(" %d %s ", n+1, name)
		if m.tab == n {
			return theme.SelRow.Render(s)
		}
		return theme.Dimmed.Render(s)
	}
	left := tab(0, "Vault") + " " + tab(1, "Settings")
	right := theme.Faded.Render("1/2 switch · ? keys · q quit")
	gap := m.w - dw(left) - dw(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) vaultBody() string {
	top := m.searchLine() + "\n" + rule(m.w) + "\n"
	bottom := "\n" + rule(m.w) + "\n" + m.countdown() + "\n" + m.hints()
	rows := m.h - 7
	if rows < 1 {
		rows = 1
	}

	leftW := m.w * 2 / 5
	if leftW > 40 {
		leftW = 40
	}
	if leftW < 16 {
		leftW = 16
	}
	rightW := m.w - leftW - 3

	right := m.tablePane(rightW, rows)
	if m.showResults() {
		right = m.resultsPane(rightW, rows)
	}
	sep := theme.Rule.Render(" │ ")
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Height(rows).Render(m.treePane(leftW, rows)),
		sep,
		lipgloss.NewStyle().Width(rightW).Height(rows).Render(right),
	)
	return top + body + bottom
}

func (m Model) settingsView() string {
	profs := m.sources()
	title := theme.Strong.Render("Sources") + theme.Dimmed.Render(fmt.Sprintf("  %d configured", len(profs)))
	top := title + "\n" + rule(m.w) + "\n"
	footer := "\n" + rule(m.w) + "\n" + theme.Faded.Render(trunc(
		"↑↓ select · a add · e edit · s sync · p save-pw · x clear-pw · d remove", m.w))

	rowsArea := m.h - 6 // tab bar + title + 2 rules + footer hint
	if rowsArea < 1 {
		rowsArea = 1
	}

	nameW, typeW, kfW, krW := 16, 8, 12, 8
	locW := m.w - nameW - typeW - kfW - krW - 6
	if locW < 10 {
		locW = 10
	}

	lines := []string{theme.Dimmed.Render(
		pad("NAME", nameW) + " " + pad("TYPE", typeW) + " " + pad("LOCATION", locW) + " " + pad("KEYFILE", kfW) + " KEYRING")}
	if len(profs) == 0 {
		lines = append(lines, "", theme.Faded.Render("  no sources yet — press 'a' to add one"))
	}
	for i, p := range profs {
		loc := p.Path
		if p.Type == config.Pleasant {
			loc = p.URL
		}
		kf := p.Keyfile
		if kf == "" {
			kf = "—"
		}
		if i == m.setSel {
			kr := "—"
			if m.setKeyring[p.Name] {
				kr = "saved"
			}
			plain := pad(p.Name, nameW) + " " + pad(string(p.Type), typeW) + " " + pad(trunc(loc, locW), locW) + " " + pad(trunc(kf, kfW), kfW) + " " + kr
			lines = append(lines, theme.SelRow.Width(m.w).Render(trunc(plain, m.w)))
			continue
		}
		lines = append(lines,
			theme.Strong.Render(pad(p.Name, nameW))+" "+
				theme.Dimmed.Render(pad(string(p.Type), typeW))+" "+
				theme.Dimmed.Render(pad(trunc(loc, locW), locW))+" "+
				theme.Faded.Render(pad(trunc(kf, kfW), kfW))+" "+
				krCell(m.setKeyring[p.Name]))
	}
	for len(lines) < rowsArea {
		lines = append(lines, "")
	}
	if len(lines) > rowsArea {
		lines = lines[:rowsArea]
	}
	return top + strings.Join(lines, "\n") + footer
}

// krCell renders the KEYRING cell: a green "saved" or a muted dash.
func krCell(saved bool) string {
	if saved {
		return theme.Ok.Render("saved")
	}
	return theme.Faded.Render("—")
}

func (m Model) searchLine() string {
	glyph := theme.Faded.Render("/  ")
	if m.searchMode {
		glyph = theme.Acc.Render("/  ")
	}
	left := glyph + m.input.View()
	right := theme.Faded.Render(fmt.Sprintf("%d sources", m.nSrc))
	if m.showResults() {
		right = theme.Dimmed.Render(fmt.Sprintf("%d matches", len(m.results)))
	}
	gap := m.w - dw(left) - dw(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) treePane(w, rows int) string {
	flat := m.visible()
	start := 0
	if m.tsel >= rows {
		start = m.tsel - rows + 1
	}
	end := min(start+rows, len(flat))

	var lines []string
	for i := start; i < end; i++ {
		n := flat[i].node
		indent := strings.Repeat("  ", flat[i].depth)
		caret := "  "
		if len(n.children) > 0 {
			if n.expanded {
				caret = "▾ "
			} else {
				caret = "▸ "
			}
		}
		if i == m.tsel {
			st := theme.Hi
			if m.focus == 0 {
				st = theme.SelRow.Width(w)
			}
			lines = append(lines, st.Render(trunc(indent+caret+n.name, w)))
			continue
		}
		lines = append(lines, theme.Acc.Render(indent+caret)+theme.Strong.Render(trunc(n.name, max(1, w-dw(indent)-2))))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) tablePane(w, rows int) string {
	folder := m.currentFolder()
	if folder == nil {
		return ""
	}
	titleW := w * 4 / 10
	userW := w * 3 / 10
	if titleW < 8 {
		titleW = 8
	}
	if userW < 6 {
		userW = 6
	}
	lines := []string{
		theme.Strong.Render(trunc(folder.name, max(4, w-14))) + theme.Dimmed.Render(fmt.Sprintf("  %d entries", len(folder.entries))),
		theme.Dimmed.Render(pad("Title", titleW)) + " " + theme.Dimmed.Render(pad("Username", userW)) + " " + theme.Dimmed.Render("Password"),
		rule(w),
	}
	if len(folder.entries) == 0 {
		lines = append(lines, theme.Faded.Render("  (no entries here — open a sub-folder)"))
	}
	avail := max(1, rows-3)
	start := 0
	if m.esel >= avail {
		start = m.esel - avail + 1
	}
	end := min(start+avail, len(folder.entries))
	for i := start; i < end; i++ {
		e := folder.entries[i]
		if i == m.esel && m.focus == 1 {
			plain := pad("• "+e.Title, titleW+2) + " " + pad(e.Username, userW) + " " + "••••••••"
			lines = append(lines, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		lines = append(lines, theme.Faded.Render("• ")+theme.Strong.Render(pad(trunc(e.Title, titleW-2), titleW))+" "+theme.Dimmed.Render(pad(e.Username, userW))+" "+theme.Acc.Render("••••••••"))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// resultsPane replaces the folder table while a search is active.
func (m Model) resultsPane(w, rows int) string {
	titleW := w * 4 / 10
	if titleW < 8 {
		titleW = 8
	}
	lines := []string{
		theme.Strong.Render("Search results") + theme.Dimmed.Render(fmt.Sprintf("  %d matches", len(m.results))),
		theme.Dimmed.Render(pad("Title", titleW)) + " " + theme.Dimmed.Render("Location"),
		rule(w),
	}
	if len(m.results) == 0 {
		lines = append(lines, theme.Dimmed.Render("  nothing matches — esc clears"))
	}
	avail := max(1, rows-3)
	start := 0
	if m.sel >= avail {
		start = m.sel - avail + 1
	}
	end := min(start+avail, len(m.results))
	q := m.input.Value()
	for i := start; i < end; i++ {
		e := m.results[i].Entry
		loc := e.Source
		if e.Path != "" {
			loc += " · " + e.Path
		}
		if i == m.sel {
			plain := pad("• "+e.Title, titleW+2) + " " + loc
			lines = append(lines, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		lines = append(lines, theme.Faded.Render("• ")+highlight(pad(e.Title, titleW), q, theme.Strong)+" "+theme.Dimmed.Render(trunc(loc, max(4, w-titleW-3))))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// detailView is the full-screen entry details (Pleasant's "Entry Details").
func (m Model) detailView() string {
	top := m.searchLine() + "\n" + rule(m.w) + "\n"
	hint := theme.Faded.Render(trunc("↵ copy pw · ctrl+r reveal · ctrl+u user · ctrl+o url · esc back", m.w))
	bottom := "\n" + rule(m.w) + "\n" + m.countdown() + "\n" + hint
	rows := m.h - 7
	if rows < 1 {
		rows = 1
	}

	e := m.selEntry()
	if e == nil {
		return top + strings.Repeat("\n", max(0, rows-1)) + bottom
	}
	w := m.w
	field := func(l, v string, st lipgloss.Style) string {
		return theme.Dimmed.Render(pad(l, 12)) + st.Render(trunc(v, max(4, w-14)))
	}
	pw := theme.Acc.Render(strings.Repeat("•", 12)) + theme.Dimmed.Render("   ctrl+r reveal")
	if m.reveal {
		pw = theme.Hi.Render(trunc(e.Password.Reveal(), max(4, w-18))) + theme.Dimmed.Render("   ctrl+r hide")
	}
	loc := e.Source
	if e.Path != "" {
		loc += " · " + e.Path
	}
	b := []string{
		theme.Brand.Render(trunc(e.Title, w)),
		theme.Dimmed.Render(trunc(loc, w)),
		"",
		field("Username", e.Username, theme.Strong),
		theme.Dimmed.Render(pad("Password", 12)) + pw,
	}
	if e.URL != "" {
		b = append(b, field("URL", e.URL, theme.Dimmed))
	}
	if len(e.Tags) > 0 {
		b = append(b, field("Tags", strings.Join(e.Tags, ", "), theme.Dimmed))
	}
	for len(b) < rows {
		b = append(b, "")
	}
	if len(b) > rows {
		b = b[:rows]
	}
	return top + strings.Join(b, "\n") + bottom
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
	case m.searchMode:
		full = "type to filter · ↑↓ pick · ↵ apply · esc cancel"
	case m.showResults():
		full = "↑↓ results · ↵ details · / edit · ctrl+y copy · esc clear"
	default:
		full = "↑↓ move · →/⇥ into · ← back · ↵ open · / search · q quit · ?"
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

// padLeft right-aligns s within width w (display-width aware).
func padLeft(s string, w int) string {
	if d := dw(s); d < w {
		return strings.Repeat(" ", w-d) + s
	}
	return trunc(s, w)
}

func (m Model) helpView() string {
	groups := []struct {
		title string
		rows  [][2]string
	}{
		{"Navigate", [][2]string{
			{"↑ / ↓", "Move up / down — tree, table, results"},
			{"→ / tab", "Enter folder · move to the entry table"},
			{"←", "Collapse folder · back to the tree"},
			{"enter", "Expand folder · open entry details"},
		}},
		{"Search", [][2]string{
			{"/", "Search every source"},
			{"enter", "Apply the filter, leave the search box"},
			{"esc", "Cancel search · clear the filter"},
		}},
		{"Entry under cursor", [][2]string{
			{"ctrl+r", "Reveal the password (in details)"},
			{"ctrl+y", "Copy password"},
			{"ctrl+u", "Copy username"},
			{"ctrl+o", "Copy URL"},
		}},
		{"General", [][2]string{
			{"?", "Toggle this help"},
			{"q / ctrl+c", "Quit — clears the clipboard"},
		}},
	}

	const keyW = 9
	var b strings.Builder
	fmt.Fprint(&b, theme.Brand.Render("harmos")+theme.Dimmed.Render("  ·  keys"))
	for _, g := range groups {
		fmt.Fprint(&b, "\n\n"+theme.Acc.Render(g.title))
		for _, r := range g.rows {
			fmt.Fprint(&b, "\n"+theme.Strong.Render(padLeft(r[0], keyW))+"    "+theme.Dimmed.Render(r[1]))
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Faint).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(max(1, m.w), max(1, m.h), lipgloss.Center, lipgloss.Center, box)
}
