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
	if m.locked {
		return m.unlockView()
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

	leftW := min(42, max(18, m.w*2/5))
	rightW := m.w - leftW - 1 // 1-column gap between panels

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

	panels := lipgloss.JoinHorizontal(lipgloss.Top, folders, " ", right)
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

	panelsH := max(3, m.h-1)
	leftW := 22
	rightW := m.w - leftW - 1

	left := box("Settings", "", m.catLines(leftW-2), leftW, panelsH, m.focus == 0)

	var right string
	switch m.setCat {
	case catTheme:
		right = box("Theme  ·  live preview", m.themeName, m.themeLines(rightW-2), rightW, panelsH, m.focus == 1)
	default:
		profs := m.sources()
		right = box("Sources", fmt.Sprintf("%d", len(profs)), m.sourceLines(rightW-2, profs), rightW, panelsH, m.focus == 1)
	}

	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return panels + "\n" + m.footer(m.settingsHint())
}

// catLines renders the Settings category list (left pane).
func (m Model) catLines(w int) []string {
	var out []string
	for k, name := range settingsCats {
		if k == m.setCat {
			st := theme.Hi
			if m.focus == 0 {
				st = theme.SelRow.Width(w)
			}
			out = append(out, st.Render(trunc("▸ "+name, w)))
			continue
		}
		out = append(out, "  "+theme.Strong.Render(name))
	}
	return out
}

// sourceLines renders the Sources table (right pane).
func (m Model) sourceLines(w int, profs []config.Profile) []string {
	i := ic()
	nameW, typeW, kfW := 18, 9, 14
	locW := max(10, w-nameW-typeW-kfW-8-3)
	out := []string{theme.Dimmed.Render(pad("NAME", nameW) + " " + pad("TYPE", typeW) + " " + pad("LOCATION", locW) + " " + pad("KEYFILE", kfW) + " KEYRING")}
	if len(profs) == 0 {
		out = append(out, "", theme.Faded.Render("  no sources yet — press 'a' to add one"))
		return out
	}
	for k, p := range profs {
		sicon := i.kdbx
		loc := p.Path
		if p.Type == config.Pleasant {
			sicon, loc = i.pps, p.URL
		}
		kf := i.none
		if p.Keyfile != "" {
			kf = filepath.Base(p.Keyfile)
		}
		if k == m.setSel && m.focus == 1 {
			kr := "—"
			if m.setKeyring[p.Name] {
				kr = "saved"
			}
			plain := pad("▸ "+p.Name, nameW) + " " + pad(string(p.Type), typeW) + " " + pad(trunc(loc, locW), locW) + " " + pad(trunc(kf, kfW), kfW) + " " + kr
			out = append(out, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		out = append(out,
			theme.Acc.Render(sicon)+" "+theme.Strong.Render(pad(trunc(p.Name, nameW-2), nameW-2))+" "+
				theme.Dimmed.Render(pad(string(p.Type), typeW))+" "+
				theme.Dimmed.Render(pad(trunc(loc, locW), locW))+" "+
				theme.Faded.Render(pad(trunc(kf, kfW), kfW))+" "+
				krCell(m.setKeyring[p.Name]))
	}
	return out
}

// settingsHint is the footer for the Settings tab, contextual to focus/category.
func (m Model) settingsHint() string {
	if m.setStatus != "" {
		return theme.Ok.Render(m.setStatus)
	}
	switch {
	case m.focus == 0:
		return theme.Faded.Render("↑↓ pick · →/↵ open · 1 Vault")
	case m.setCat == catTheme:
		return theme.Faded.Render("↑↓ preview · ↵ save · ← back")
	default:
		return theme.Faded.Render("↑↓ select · a add · e edit · s sync · p save-pw · x clear-pw · d remove · ← back")
	}
}

// krCell renders the KEYRING cell: a green "saved" or a muted dash.
func krCell(saved bool) string {
	if saved {
		return theme.Ok.Render("saved")
	}
	return theme.Faded.Render("—")
}

// brand is the small two-tone "harmos" wordmark.
func brand() string {
	return theme.Acc.Bold(true).Render("har") + theme.Hi.Bold(true).Render("mos")
}

// plural formats a count with a singular/plural word.
func plural(n int, one, many string) string {
	w := many
	if n == 1 {
		w = one
	}
	return fmt.Sprintf("%d %s", n, w)
}

func (m Model) searchLine() string {
	glyph := theme.Faded.Render("  /  ")
	if m.searchMode {
		glyph = theme.Acc.Render("  /  ")
	}
	left := brand() + glyph + m.input.View()
	right := theme.Faded.Render(plural(m.nSrc, "source", "sources"))
	if m.showResults() {
		right = theme.Dimmed.Render(plural(len(m.results), "match", "matches"))
	}
	return spread(left, right, m.w)
}

func (m Model) treeLines(w, rows int) []string {
	flat := m.visible()
	i := ic()
	dim := m.showResults() // the tree loses emphasis while results are showing
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
		count := ""
		if len(n.entries) > 0 {
			count = fmt.Sprintf(" %d", len(n.entries))
		}
		if k == m.tsel {
			st := theme.Hi
			if m.focus == 0 && !m.showResults() {
				st = theme.SelRow.Width(w)
			}
			out = append(out, st.Render(trunc(indent+icon+" "+n.name+count, w)))
			continue
		}
		nameStyle, iconStyle := theme.Strong, theme.Acc
		if dim {
			nameStyle, iconStyle = theme.Dimmed, theme.Faded
		}
		out = append(out, iconStyle.Render(indent+icon)+" "+nameStyle.Render(trunc(n.name, max(1, w-dw(indent)-2-dw(count))))+theme.Faded.Render(count))
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
			plain := pad("▸ "+e.Title, titleW) + " " + pad(e.Username, userW) + " ••••••••"
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
			plain := pad("▸ "+e.Title, titleW) + " " + loc
			out = append(out, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		out = append(out, theme.Faded.Render(i.entry+" ")+highlight(pad(trunc(e.Title, titleW-2), titleW-2), q, theme.Strong)+" "+theme.Dimmed.Render(trunc(loc, max(4, w-titleW-3))))
	}
	return out
}

// detailView shows the selected entry inside a titled box.
func (m Model) detailView() string {
	searchLine := m.searchLine()
	hint := theme.Faded.Render("↵ copy pw · ctrl+r reveal · ctrl+u user · ctrl+o url · esc back")
	bottom := m.countdown() + "\n" + m.footer(hint)
	boxH := max(3, m.h-3)

	e := m.selEntry()
	if e == nil {
		return searchLine + "\n" + box("Entry", "", nil, m.w, boxH, true) + "\n" + bottom
	}
	inW := m.w - 2
	field := func(l, v string, st lipgloss.Style) string {
		return theme.Dimmed.Render(pad(l, 12)) + st.Render(trunc(v, max(4, inW-14)))
	}
	pw := theme.Acc.Render(strings.Repeat("•", 12)) + theme.Dimmed.Render("   ctrl+r reveal")
	if m.reveal {
		pw = theme.Hi.Render(trunc(e.Password.Reveal(), max(4, inW-18))) + theme.Dimmed.Render("   ctrl+r hide")
	}
	loc := e.Source
	if e.Path != "" {
		loc += " · " + e.Path
	}
	b := []string{
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
	return searchLine + "\n" + box(e.Title, loc, b, m.w, boxH, true) + "\n" + bottom
}

func (m Model) countdown() string {
	if m.remaining <= 0 {
		// idle: show where the selected entry lives, rather than a dead line
		if e := m.selEntry(); e != nil {
			loc := e.Source
			if e.Path != "" {
				loc += " · " + e.Path
			}
			return theme.Faded.Render(trunc("  "+loc, m.w))
		}
		return ""
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

type helpGroup struct {
	title string
	rows  [][2]string
}

func (m Model) helpView() string {
	var groups []helpGroup
	if m.tab == 1 {
		groups = []helpGroup{
			{"Settings", [][2]string{
				{"↑ / ↓", "Move in the sources list"},
				{"a / e", "Add / edit a source"},
				{"s", "Sync a Pleasant source"},
				{"p / x", "Save / clear a keyring password"},
				{"d", "Remove a source"},
				{"t", "Change the color theme (live)"},
			}},
		}
	} else {
		groups = []helpGroup{
			{"Navigate", [][2]string{
				{"↑ / ↓", "Move — tree, table, results"},
				{"→ / tab", "Enter folder · move to the table"},
				{"←", "Collapse folder · back to the tree"},
				{"enter", "Expand folder · open entry details"},
			}},
			{"Search", [][2]string{
				{"/", "Search every source"},
				{"enter", "Apply the filter, leave the box"},
				{"esc", "Cancel search · clear the filter"},
			}},
			{"Entry under cursor", [][2]string{
				{"ctrl+r", "Reveal the password (in details)"},
				{"ctrl+y / u / o", "Copy password / username / URL"},
			}},
		}
	}
	groups = append(groups, helpGroup{"General", [][2]string{
		{"1 / 2", "Switch tab — Vault / Settings"},
		{"?", "Toggle this help"},
		{"q / ctrl+c", "Quit — clears the clipboard"},
	}})

	const keyW = 14
	var b strings.Builder
	fmt.Fprint(&b, brand()+theme.Dimmed.Render("  ·  keys"))
	for _, g := range groups {
		fmt.Fprint(&b, "\n\n"+theme.Acc.Render(g.title))
		for _, r := range g.rows {
			fmt.Fprint(&b, "\n"+theme.Strong.Render(padLeft(r[0], keyW))+"    "+theme.Dimmed.Render(r[1]))
		}
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(max(1, m.w), max(1, m.h), lipgloss.Center, lipgloss.Center, panel)
}
