package tui

import (
	"fmt"
	"path/filepath"
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
	switch {
	case m.tab == 1:
		return m.settingsView()
	case m.detail:
		return m.detailView()
	default:
		return m.vaultBody()
	}
}

// tabIndicator is the small "1 Vault · 2 Settings" marker shown bottom-right, the
// active tab in accent.
func (m Model) tabIndicator() string {
	tab := func(n int, name string) string {
		s := fmt.Sprintf("%d %s", n+1, name)
		if m.tab == n {
			return theme.Acc.Render(s)
		}
		return theme.Faded.Render(s)
	}
	return tab(0, "Vault") + theme.Faded.Render(" · ") + tab(1, "Settings")
}

// spread puts left at the start and right at the end of a width-w line.
func spread(left, right string, w int) string {
	gap := w - dw(left) - dw(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// footer is the bottom line: a hint on the left, the tab indicator on the right,
// with the hint truncated so the two never collide.
func (m Model) footer(left string) string {
	ti := m.tabIndicator()
	return spread(trunc(left, max(4, m.w-dw(ti)-2)), ti, m.w)
}

func (m Model) vaultBody() string {
	searchLine := m.searchLine()
	bottom := m.countdown() + "\n" + m.footer(m.hints())
	panelsH := max(3, m.h-3) // search line + countdown + footer

	leftW := m.w * 2 / 5
	if leftW > 42 {
		leftW = 42
	}
	if leftW < 18 {
		leftW = 18
	}
	rightW := m.w - leftW

	flat := m.visible()
	folders := box("Folders", cursorInfo(m.tsel, len(flat)),
		m.treeLines(leftW-2, panelsH-2), leftW, panelsH, m.focus == 0 && !m.showResults())

	var right string
	if m.showResults() {
		right = box("Search results", fmt.Sprintf("%d", len(m.results)),
			m.resultLines(rightW-2, panelsH-2), rightW, panelsH, true)
	} else {
		f := m.currentFolder()
		title, info := "Entries", ""
		if f != nil {
			title, info = f.name, fmt.Sprintf("%d", len(f.entries))
		}
		right = box(title, info, m.entryLines(rightW-2, panelsH-2), rightW, panelsH, m.focus == 1)
	}

	panels := lipgloss.JoinHorizontal(lipgloss.Top, folders, right)
	return searchLine + "\n" + panels + "\n" + bottom
}

// cursorInfo is the "3/42" position marker for a panel's top border.
func cursorInfo(sel, n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", sel+1, n)
}

func (m Model) settingsView() string {
	switch m.setMode {
	case setRemove:
		return m.removeConfirmView()
	case setForm:
		return m.formView()
	case setPrompt:
		return m.promptView()
	case setSyncing:
		return m.syncView()
	}
	profs := m.sources()
	title := theme.Strong.Render("Sources") + theme.Dimmed.Render(fmt.Sprintf("  %d configured", len(profs)))
	top := title + "\n" + rule(m.w) + "\n"

	hint := theme.Faded.Render("↑↓ select · a add · e edit · s sync · p save-pw · x clear-pw · d remove")
	if m.setStatus != "" {
		hint = theme.Ok.Render(m.setStatus)
	}
	footer := "\n" + rule(m.w) + "\n" + m.footer(hint)

	rowsArea := m.h - 4 // title + 2 rules + footer
	if rowsArea < 1 {
		rowsArea = 1
	}

	// Fixed columns; LOCATION takes the remaining width. KEYFILE shows just the
	// file name (the full path never fits and is unreadable when truncated).
	nameW, typeW, kfW, krW := 18, 9, 16, 7
	locW := m.w - nameW - typeW - kfW - krW - 4
	if locW < 12 {
		locW = 12
	}

	header := pad("NAME", nameW) + " " + pad("TYPE", typeW) + " " + pad("LOCATION", locW) + " " + pad("KEYFILE", kfW) + " KEYRING"
	lines := []string{theme.Dimmed.Render(header)}
	if len(profs) == 0 {
		lines = append(lines, "", theme.Faded.Render("  no sources yet — press 'a' to add one"))
	}
	for i, p := range profs {
		loc := p.Path
		if p.Type == config.Pleasant {
			loc = p.URL
		}
		kf := "—"
		if p.Keyfile != "" {
			kf = filepath.Base(p.Keyfile)
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

func (m Model) treeLines(w, rows int) []string {
	flat := m.visible()
	i := ic()
	start := 0
	if m.tsel >= rows {
		start = m.tsel - rows + 1
	}
	end := min(start+rows, len(flat))

	var out []string
	for k := start; k < end; k++ {
		n := flat[k].node
		indent := strings.Repeat("  ", flat[k].depth)
		icon := i.folder
		if len(n.children) > 0 && n.expanded {
			icon = i.folderOpen
		}
		if k == m.tsel {
			st := theme.Hi
			if m.focus == 0 && !m.showResults() {
				st = theme.SelRow.Width(w)
			}
			out = append(out, st.Render(trunc(indent+icon+" "+n.name, w)))
			continue
		}
		out = append(out, theme.Acc.Render(indent+icon)+" "+theme.Strong.Render(trunc(n.name, max(1, w-dw(indent)-2))))
	}
	return out
}

func (m Model) entryLines(w, rows int) []string {
	f := m.currentFolder()
	i := ic()
	titleW := max(8, w*4/10)
	userW := max(6, w*3/10)
	out := []string{theme.Dimmed.Render(pad("Title", titleW) + " " + pad("Username", userW) + " Password")}
	if f == nil || len(f.entries) == 0 {
		out = append(out, theme.Faded.Render("  (no entries here — open a sub-folder)"))
		return out
	}
	avail := max(1, rows-1)
	start := 0
	if m.esel >= avail {
		start = m.esel - avail + 1
	}
	end := min(start+avail, len(f.entries))
	for k := start; k < end; k++ {
		e := f.entries[k]
		if k == m.esel && m.focus == 1 {
			plain := pad(i.entry+" "+e.Title, titleW) + " " + pad(e.Username, userW) + " ••••••••"
			out = append(out, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		out = append(out, theme.Faded.Render(i.entry+" ")+theme.Strong.Render(pad(trunc(e.Title, titleW-2), titleW-2))+" "+theme.Dimmed.Render(pad(e.Username, userW))+" "+theme.Acc.Render("••••••••"))
	}
	return out
}

// resultLines are the ranked search results shown in the right panel.
func (m Model) resultLines(w, rows int) []string {
	i := ic()
	titleW := max(8, w*4/10)
	out := []string{theme.Dimmed.Render(pad("Title", titleW) + " Location")}
	if len(m.results) == 0 {
		out = append(out, theme.Faded.Render("  nothing matches — esc clears"))
		return out
	}
	avail := max(1, rows-1)
	start := 0
	if m.sel >= avail {
		start = m.sel - avail + 1
	}
	end := min(start+avail, len(m.results))
	q := m.input.Value()
	for k := start; k < end; k++ {
		e := m.results[k].Entry
		loc := e.Source
		if e.Path != "" {
			loc += " · " + e.Path
		}
		if k == m.sel {
			plain := pad(i.entry+" "+e.Title, titleW) + " " + loc
			out = append(out, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		out = append(out, theme.Faded.Render(i.entry+" ")+highlight(pad(trunc(e.Title, titleW-2), titleW-2), q, theme.Strong)+" "+theme.Dimmed.Render(trunc(loc, max(4, w-titleW-3))))
	}
	return out
}

// detailView is the full-screen entry details (Pleasant's "Entry Details").
func (m Model) detailView() string {
	top := m.searchLine() + "\n" + rule(m.w) + "\n"
	hint := theme.Faded.Render("↵ copy pw · ctrl+r reveal · ctrl+u user · ctrl+o url · esc back")
	bottom := "\n" + rule(m.w) + "\n" + m.countdown() + "\n" + m.footer(hint)
	rows := m.h - 6
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
