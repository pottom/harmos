package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/otp"
	"github.com/pottom/harmos/internal/search"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
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

// highlight renders s in the base style with every case-insensitive occurrence of
// q emphasized — grep --color for the search query.
func highlight(s, q string, base lipgloss.Style) string {
	if q == "" {
		return base.Render(s)
	}
	lq := strings.ToLower(q)
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(strings.ToLower(rest), lq)
		if i < 0 {
			b.WriteString(base.Render(rest))
			return b.String()
		}
		j := i + len(q)
		b.WriteString(base.Render(rest[:i]))
		b.WriteString(theme.Hi.Render(rest[i:j]))
		rest = rest[j:]
	}
}

// highlightTerms renders s in base with every case-insensitive occurrence of any
// term emphasized — grep --color over a whole query (all its AND/OR terms), not
// just the raw input. Overlapping hits are merged so nested terms don't double up.
func highlightTerms(s string, terms []string, base lipgloss.Style) string {
	if len(terms) == 0 {
		return base.Render(s)
	}
	ls := strings.ToLower(s)
	type span struct{ a, b int }
	var spans []span
	for _, t := range terms {
		if t == "" {
			continue
		}
		for from := 0; ; {
			i := strings.Index(ls[from:], t)
			if i < 0 {
				break
			}
			a := from + i
			spans = append(spans, span{a, a + len(t)})
			from = a + len(t)
		}
	}
	if len(spans) == 0 {
		return base.Render(s)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })

	var b strings.Builder
	pos := 0
	for _, sp := range spans {
		if sp.b <= pos { // fully inside an already-emphasized run
			continue
		}
		if sp.a > pos {
			b.WriteString(base.Render(s[pos:sp.a]))
			pos = sp.a
		}
		b.WriteString(theme.Hi.Render(s[pos:sp.b]))
		pos = sp.b
	}
	if pos < len(s) {
		b.WriteString(base.Render(s[pos:]))
	}
	return b.String()
}

func (m Model) View() string {
	if m.w < 40 || m.h < 10 {
		return m.tooSmall()
	}
	if m.locked {
		return m.unlockView()
	}
	if m.attach != attachNone {
		return m.attachView()
	}
	if m.help {
		return m.helpView()
	}
	if m.tab == 1 {
		return m.settingsView()
	}
	if m.tab == 2 {
		return m.generateView()
	}
	return m.vaultBody() // browse, search, and entry detail share this frame
}

// tabIndicator is the small "Vault · Settings" marker shown bottom-right, the
// active tab in accent (switched with 1/2).
func (m Model) tabIndicator() string {
	tab := func(n int, name string) string {
		if m.tab == n {
			return theme.Acc.Render(name)
		}
		return theme.Faded.Render(name)
	}
	// Display order Vault · Generate · Settings (Settings last); the number keys
	// follow this order (1/2/3), though the internal tab indices are 0/1/2.
	sep := theme.Faded.Render(" · ")
	return tab(0, "Vault") + sep + tab(2, "Generate") + sep + tab(1, "Settings")
}

// spread puts left at the start and right at the end of a width-w line.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// windowStart is the first visible index of a list of total items, rows tall,
// that keeps cursor in view — the scroll offset shared by the line builders and
// their scrollbars.
func windowStart(cursor, rows, total int) int {
	if rows <= 0 || total <= rows || cursor < rows {
		return 0
	}
	start := cursor - rows + 1
	if start > total-rows {
		start = total - rows
	}
	if start < 0 {
		start = 0
	}
	return start
}

// treeRailW is the width of the collapsed folder rail.
const treeRailW = 5

// leftPaneW is the width of the Vault tab's left pane — the folder tree, or the
// thin rail when collapsed. Shared by the layout and the mouse hit-tests so they
// always agree.
func (m Model) leftPaneW() int {
	if m.treeCollapsed {
		return treeRailW
	}
	return min(42, max(18, m.w*2/5))
}

// treeRail renders the collapsed folder pane: a thin bordered rail with the source
// icons stacked, so the tree is clearly still there (reopen with ctrl+b or a
// click).
func (m Model) treeRail(h int) string {
	i := ic()
	var lines []string
	for _, root := range m.roots {
		icon := i.folder
		if si, ok := m.sourceIcon(root.name); ok {
			icon = si
		}
		lines = append(lines, theme.Acc.Render(icon))
	}
	return box(i.folder, "", lines, treeRailW, h, false)
}

// panelRows is the visible row count of the folder-tree pane and of the
// table/results pane (which spends one row on its header) — the page size a
// PageUp/PageDown jump moves by.
func (m Model) panelRows() (tree, list int) {
	panelsH := max(3, m.h-3)
	return panelsH - 2, panelsH - 3
}

// clampIndex keeps a cursor inside [0, n) for a list of n items (0 when empty).
func clampIndex(i, n int) int {
	if n <= 0 || i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func spread(left, right string, w int) string {
	gap := w - dw(left) - dw(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// modal renders a centered, content-sized panel with a hint beneath it — the
// shared superfile-style frame for the save-password and sync screens (echoing
// the unlock screen's look).
func (m Model) modal(title, info string, lines []string, hint string) string {
	inner := 0
	for _, ln := range lines {
		if d := dw(ln); d > inner {
			inner = d
		}
	}
	if t := dw(title) + dw(info) + 6; t > inner {
		inner = t
	}
	boxW := max(40, min(inner+4, m.w-4))
	panel := box(title, info, lines, boxW, len(lines)+2, true)
	block := panel
	if hint != "" {
		block = lipgloss.JoinVertical(lipgloss.Center, panel, "", theme.Faded.Render(hint))
	}
	return lipgloss.Place(max(1, m.w), max(1, m.h), lipgloss.Center, lipgloss.Center, block)
}

// footer is the bottom line: a hint on the left, the tab indicator on the right,
// with the hint truncated so the two never collide.
func (m Model) footer(left string) string {
	ti := m.tabIndicator()
	return spread(trunc(left, max(4, m.w-dw(ti)-2)), ti, m.w)
}

func (m Model) vaultBody() string {
	searchLine := m.searchLine()
	hint := m.hints()
	if m.detail {
		var parts []string
		if e := m.selEntry(); e != nil {
			w, vis := m.detailViewport()
			if len(m.detailLines(e, w)) > vis {
				parts = append(parts, "↑↓ scroll")
			}
			parts = append(parts, "↵ copy password")
			if e.TOTP != "" {
				parts = append(parts, "ctrl+t copy totp")
			}
			if len(e.Files) > 0 {
				parts = append(parts, "s save files")
			}
		} else {
			parts = append(parts, "↵ copy password")
		}
		parts = append(parts, "c copy get-cmd", "ctrl+r reveal", "esc back")
		hint = theme.Faded.Render(strings.Join(parts, " · "))
	}
	bottom := m.countdown() + "\n" + m.footer(hint)
	panelsH := max(3, m.h-3) // search line + countdown + footer

	leftW := m.leftPaneW()
	rightW := m.w - leftW - 1 // 1-column gap between panels

	flat := m.visible()
	treeRows := panelsH - 2
	var folders string
	if m.treeCollapsed {
		folders = m.treeRail(panelsH)
	} else {
		treeSB := boolToInt(len(flat) > treeRows) // scrollbar steals one content column
		folders = boxV("Folders", cursorInfo(m.tsel, len(flat)),
			m.treeLines(leftW-2-treeSB, treeRows), leftW, panelsH, m.focus == 0 && !m.showResults() && !m.detail,
			len(flat), windowStart(m.tsel, treeRows, len(flat)), 0)
	}

	var right string
	listRows := panelsH - 3 // one row is the table header
	switch {
	case m.detail:
		right = m.detailPane(rightW, panelsH)
	case m.showResults():
		sb := boolToInt(len(m.results) > listRows)
		right = boxV("Search results", fmt.Sprintf("%d", len(m.results)),
			m.resultLines(rightW-2-sb, panelsH-2), rightW, panelsH, true,
			len(m.results), windowStart(m.sel, listRows, len(m.results)), 1)
	default:
		f := m.currentFolder()
		title, info, n := "Entries", "", 0
		if f != nil {
			title, info, n = m.folderCrumb(flat, m.tsel), fmt.Sprintf("%d", len(f.entries)), len(f.entries)
		}
		sb := boolToInt(n > listRows)
		right = boxV(title, info, m.entryLines(rightW-2-sb, panelsH-2), rightW, panelsH, m.focus == 1,
			n, windowStart(m.esel, listRows, n), 1)
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

	// Same vertical frame as the Vault tab so nothing jumps when switching:
	// header line, panels, a context line, then the hints footer.
	panelsH := max(3, m.h-3)
	leftW := 22
	rightW := m.w - leftW - 1

	left := box("Settings", "", m.catLines(leftW-2), leftW, panelsH, m.focus == 0)

	var right string
	switch m.setCat {
	case catTheme:
		right = box("Theme  ·  live preview", m.themeName, m.themeLines(rightW-2), rightW, panelsH, m.focus == 1)
	case catIcons:
		right = box("Icons", onOff(nerd), m.iconsLines(rightW-2), rightW, panelsH, m.focus == 1)
	case catPrefs:
		right = box("Preferences", "", m.prefsLines(rightW-2), rightW, panelsH, m.focus == 1)
	default:
		profs := m.sources()
		right = box("Sources", fmt.Sprintf("%d", len(profs)), m.sourceLines(rightW-2, profs), rightW, panelsH, m.focus == 1)
	}

	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return m.settingsHeader() + "\n" + panels + "\n" + m.settingsContext() + "\n" + m.footer(m.settingsHint())
}

// settingsHeader mirrors the Vault tab's search line: the wordmark plus a source
// count, so the top line sits at the same row on both tabs.
func (m Model) settingsHeader() string {
	left := brand() + theme.Faded.Render("  ·  settings")
	right := theme.Faded.Render(plural(len(m.sources()), "source", "sources"))
	return spread(left, right, m.w)
}

// settingsContext is the line above the hints — the counterpart to the Vault
// countdown/provenance line. It shows the last action's status, else what the
// current selection points at.
func (m Model) settingsContext() string {
	if m.setStatus != "" {
		return theme.Ok.Render(trunc("  "+m.setStatus, m.w))
	}
	if m.setCat == catTheme {
		return theme.Faded.Render(trunc("  active theme · "+m.themeName, m.w))
	}
	if m.setCat == catIcons {
		return theme.Faded.Render(trunc("  Nerd Font icons · "+onOff(nerd), m.w))
	}
	if m.setCat == catPrefs {
		return theme.Faded.Render(trunc(fmt.Sprintf("  clipboard %ds · cache stale after %s", int(m.timeout.Seconds()), humanStale(m.staleAfter)), m.w))
	}
	profs := m.sources()
	if m.setSel < len(profs) {
		p := profs[m.setSel]
		loc := p.Path
		if p.Type == config.Pleasant {
			loc = p.URL
		}
		return theme.Faded.Render(trunc("  "+p.Name+" · "+loc, m.w))
	}
	return ""
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

// sourceLines renders the Sources table (right pane). Every line carries a
// one-cell left padding so the table lines up with the Theme/Icons panes, which
// indent their content.
func (m Model) sourceLines(w int, profs []config.Profile) []string {
	i := ic()
	iw := w - 2 // reserve the leading pad columns, matching the other panes
	nameW, typeW, kfW, statW := 18, 9, 14, 18
	locW := max(8, iw-nameW-typeW-kfW-statW-6)
	out := []string{"  " + theme.Dimmed.Render(pad("NAME", nameW)+" "+pad("TYPE", typeW)+" "+pad("LOCATION", locW)+" "+pad("KEYFILE", kfW)+" STATUS")}
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
		// STATUS mirrors the unlock screen: cache freshness / credential state.
		badge, kind := sourceBadge(p, m.staleAfter)
		dot, bst := badgeDot(kind)
		badge = trunc(badge, statW-2)

		if k == m.setSel && m.focus == 1 {
			// selected row: the source's own type icon, not a ▸ arrow (as elsewhere)
			plain := pad(sicon+" "+p.Name, nameW) + " " + pad(string(p.Type), typeW) + " " + pad(trunc(loc, locW), locW) + " " + pad(trunc(kf, kfW), kfW) + " " + dot + " " + badge
			out = append(out, theme.SelRow.Width(w).Render(trunc("  "+plain, w)))
			continue
		}
		out = append(out,
			"  "+theme.Acc.Render(sicon)+" "+theme.Strong.Render(pad(trunc(p.Name, nameW-2), nameW-2))+" "+
				theme.Dimmed.Render(pad(string(p.Type), typeW))+" "+
				theme.Dimmed.Render(pad(trunc(loc, locW), locW))+" "+
				theme.Faded.Render(pad(trunc(kf, kfW), kfW))+" "+
				bst.Render(dot+" "+badge))
	}
	return out
}

// settingsHint is the footer for the Settings tab, contextual to focus/category.
func (m Model) settingsHint() string {
	switch {
	case m.focus == 0:
		return theme.Faded.Render("↑↓ pick · →/↵ open")
	case m.setCat == catTheme:
		return theme.Faded.Render("↑↓ preview · ↵ save · ← back")
	case m.setCat == catIcons:
		return theme.Faded.Render("space toggle Nerd Font · ← back")
	case m.setCat == catPrefs:
		return theme.Faded.Render("↑↓ pick · ←/→ adjust · esc back")
	default:
		return theme.Faded.Render("↑↓ select · a add · e edit · s sync · p save-pw · x clear-pw · d remove · ← back")
	}
}

// prefsLines is the Preferences pane: editable, persistent app settings.
func (m Model) prefsLines(w int) []string {
	row := func(idx int, label, val string) string {
		if m.focus == 1 && m.prefSel == idx {
			return theme.SelRow.Width(w).Render(trunc("▸ "+pad(label, 20)+val+"  −/+", w))
		}
		return "  " + theme.Strong.Render(pad(label, 20)) + theme.Hi.Render(val) + theme.Faded.Render("  −/+")
	}
	return []string{
		"",
		row(0, "Clipboard timeout", fmt.Sprintf("%ds", int(m.timeout.Seconds()))),
		row(1, "Cache stale after", humanStale(m.staleAfter)),
		"",
		"  " + theme.Faded.Render("How long a copied secret stays on the clipboard"),
		"  " + theme.Faded.Render("before it is cleared, and when a Pleasant cache"),
		"  " + theme.Faded.Render("is flagged stale in STATUS."),
	}
}

// humanStale formats the cache threshold as whole days when possible, else hours.
func humanStale(d time.Duration) string {
	if h := int(d.Hours()); h%24 == 0 {
		return fmt.Sprintf("%dd", h/24)
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// onOff renders a boolean as "on"/"off".
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// iconsLines is the Icons category pane: the current state plus a live glyph
// preview, so the user can tell whether their terminal has a Nerd Font.
func (m Model) iconsLines(_ int) []string {
	i := ic()
	state, st := "off", theme.Faded
	if nerd {
		state, st = "on", theme.Ok
	}
	preview := i.pps + "  " + i.kdbx + "  " + i.folder + "  " + i.keyfile + "  " + i.user + "  " + i.link + "  " + i.tag + "  " + i.clock
	return []string{
		"  " + theme.Dimmed.Render("Nerd Font icons  ") + st.Render(state),
		"",
		"  " + theme.Dimmed.Render("preview") + "   " + theme.Acc.Render(preview),
		"",
		"  " + theme.Faded.Render("Turn off if the icons show as boxes (□) —"),
		"  " + theme.Faded.Render("your terminal has no Nerd Font."),
		"",
		"  " + theme.Faded.Render("HARMOS_NERDFONT overrides this when set."),
	}
}

// brand is the small two-tone "harmos" wordmark.
func brand() string {
	return theme.Acc.Bold(true).Render("har") + theme.Hi.Bold(true).Render("mos")
}

// sourceIcon returns the type glyph for a source (pps server / kdbx file), as in
// the Settings sources pane. ok is false when the source's type isn't known
// (e.g. no config), so the caller can keep the folder icon.
func (m Model) sourceIcon(name string) (string, bool) {
	i := ic()
	switch m.srcType[name] {
	case config.Pleasant:
		return i.pps, true
	case config.Kdbx:
		return i.kdbx, true
	default:
		return "", false
	}
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
	if n := len(m.excluded); n > 0 && !m.showResults() {
		right += theme.Bad.Render(fmt.Sprintf("  ⚠ %d unavailable", n))
	}
	return spread(left, right, m.w)
}

func (m Model) treeLines(w, rows int) []string {
	flat := m.visible()
	i := ic()
	counts := m.matchCounts() // nil unless searching; a heat map of where hits are
	start := windowStart(m.tsel, rows, len(flat))
	end := min(start+rows, len(flat))

	var out []string
	for k := start; k < end; k++ {
		n := flat[k].node
		indent := strings.Repeat("  ", flat[k].depth)
		icon := i.folder
		if len(n.children) > 0 && n.expanded {
			icon = i.folderOpen
		}
		if flat[k].depth == 0 { // a source root — mark it by type, like Settings
			if si, ok := m.sourceIcon(n.name); ok {
				icon = si
			}
		}

		// count: while searching, the accent match count on folders that have hits;
		// otherwise the faded entry count.
		countN, countStyle := len(n.entries), theme.Faded
		if counts != nil {
			countN, countStyle = counts[n], theme.Acc
		}
		count := ""
		if countN > 0 {
			count = fmt.Sprintf(" %d", countN)
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
		if counts != nil && counts[n] == 0 { // searching: dim folders with no hits
			nameStyle, iconStyle = theme.Dimmed, theme.Faded
		}
		name := nameStyle.Render(trunc(n.name, max(1, w-dw(indent)-2-dw(count))))
		out = append(out, iconStyle.Render(indent+icon)+" "+name+countStyle.Render(count))
	}
	return out
}

func (m Model) entryLines(w, rows int) []string {
	f := m.currentFolder()
	i := ic()
	// No password column: every entry has one, so a column of dots carries no
	// information. Title + Username by default; when the pane is wide enough (e.g.
	// the tree is collapsed, or a big terminal) an extra URL column earns its keep.
	urlCol := w >= 64
	titleW := max(8, w*13/20)
	userW := max(6, w-titleW-1)
	urlW := 0
	if urlCol {
		titleW = max(8, w*2/5)
		userW = max(6, w*3/10)
		urlW = max(6, w-titleW-userW-2)
	}
	header := pad("Title", titleW) + " " + pad("Username", userW)
	if urlCol {
		header += " URL"
	}
	out := []string{theme.Dimmed.Render(header)}
	if f == nil || len(f.entries) == 0 {
		out = append(out, theme.Faded.Render("  (no entries here — open a sub-folder)"))
		return out
	}
	avail := max(1, rows-1)
	start := windowStart(m.esel, avail, len(f.entries))
	end := min(start+avail, len(f.entries))
	for k := start; k < end; k++ {
		e := f.entries[k]
		if k == m.esel && m.focus == 1 {
			plain := pad(i.entry+" "+e.Title, titleW) + " " + pad(e.Username, userW)
			if urlCol {
				plain += " " + e.URL
			}
			out = append(out, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		line := theme.Faded.Render(i.entry+" ") +
			theme.Strong.Render(pad(trunc(e.Title, titleW-2), titleW-2)) + " " +
			theme.Dimmed.Render(pad(trunc(e.Username, userW), userW))
		if urlCol {
			line += " " + theme.Dimmed.Render(trunc(e.URL, urlW))
		}
		out = append(out, line)
	}
	return out
}

// resultLines are the ranked search results shown in the right panel. Every row
// carries three things so a search is unambiguous: the entry (Title), where it
// lives (Where — source and folder, keeping the identifying tail), and what the
// query hit (Match — the field name and a highlighted excerpt). Many Pleasant
// entries have no title, so the folder tail is often the only identity.
func (m Model) resultLines(w, rows int) []string {
	i := ic()
	titleW := max(8, w*3/10)
	locW := max(10, w*7/20)
	matchW := max(6, w-titleW-locW-2)
	out := []string{theme.Dimmed.Render(pad("Title", titleW) + " " + pad("Where", locW) + " Match")}
	if len(m.results) == 0 {
		out = append(out, theme.Faded.Render("  nothing matches — esc clears"))
		return out
	}
	avail := max(1, rows-1)
	start := windowStart(m.sel, avail, len(m.results))
	end := min(start+avail, len(m.results))
	terms := search.HighlightTerms(m.input.Value())
	for k := start; k < end; k++ {
		r := m.results[k]
		e := r.Entry

		title, titleSt := e.Title, theme.Strong
		if title == "" {
			title, titleSt = "(untitled)", theme.Faded
		}
		loc := locString(e, locW)
		val := matchedFieldValue(e, r.Field) // "" for a title match or a secret field

		// The Match cell: field name + excerpt, or just the field, or "title".
		field, snip := r.Field, ""
		if field == "" {
			field = "title"
		}
		if val != "" {
			snip = snippet(val, terms, max(6, matchW-dw(field)-1))
		}

		if k == m.sel {
			match := field
			if snip != "" {
				match += " " + snip
			}
			plain := pad(i.entry+" "+title, titleW) + " " + pad(loc, locW) + " " + match
			out = append(out, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}

		matchCell := theme.Acc.Render(field)
		if snip != "" {
			matchCell += " " + highlightTerms(snip, terms, theme.Dimmed)
		}
		out = append(out,
			theme.Faded.Render(i.entry+" ")+
				highlightTerms(pad(trunc(title, titleW-2), titleW-2), terms, titleSt)+" "+
				theme.Dimmed.Render(pad(loc, locW))+" "+matchCell)
	}
	return out
}

// locString is an entry's "source · folder" location fitted to w, keeping the
// tail of the path (the leaf folder identifies an untitled entry) rather than the
// source prefix.
func locString(e vault.Entry, w int) string {
	if e.Path == "" {
		return trunc(e.Source, w)
	}
	avail := w - dw(e.Source) - 3 // " · "
	if avail < 4 {
		return trunc(e.Source+" · "+e.Path, w)
	}
	return e.Source + " · " + truncLeft(e.Path, avail)
}

// truncLeft truncates from the left, keeping the last w cells with a leading "…"
// — for paths, where the tail (the leaf) is the identifying part.
func truncLeft(s string, w int) string {
	if dw(s) <= w {
		return s
	}
	r := []rune(s)
	for j := range r {
		if dw(string(r[j:])) <= w-1 {
			return "…" + string(r[j:])
		}
	}
	return trunc(s, w)
}

// matchedFieldValue is the plain text of the field the search matched (r.Field),
// so the results can show an excerpt. It returns "" for a title match (the title
// is already shown, highlighted) and for a protected custom field (never surface
// a secret value — the field name matched, not the value).
func matchedFieldValue(e vault.Entry, field string) string {
	switch field {
	case "user":
		return e.Username
	case "url":
		return e.URL
	case "path":
		return e.Path
	case "tags":
		return strings.Join(e.Tags, ", ")
	case "notes":
		return strings.Join(strings.Fields(strings.ReplaceAll(e.Notes, "\n", " ")), " ")
	case "file":
		names := make([]string, len(e.Files))
		for i, f := range e.Files {
			names[i] = f.Name
		}
		return strings.Join(names, ", ")
	case "":
		return ""
	default:
		for _, f := range e.Custom {
			if f.Name == field {
				if f.Protected {
					return ""
				}
				return f.Value
			}
		}
	}
	return ""
}

// snippet is a short excerpt of val centered on the first query term, ellipsized,
// width cells wide — grep's match context for a result row.
func snippet(val string, terms []string, width int) string {
	if width < 6 {
		width = 6
	}
	lv := strings.ToLower(val)
	pos := -1
	for _, t := range terms {
		if t == "" {
			continue
		}
		if idx := strings.Index(lv, t); idx >= 0 && (pos < 0 || idx < pos) {
			pos = idx
		}
	}
	if pos <= 0 { // no hit (a name-only match) or hit at the start — show from the top
		return trunc(val, width)
	}
	const ctx = 12 // keep a little context before the match
	start := utf8.RuneCountInString(val[:pos])
	runes := []rune(val)
	from := max(0, start-ctx)
	if from > 0 { // snap to a word boundary so the excerpt doesn't begin mid-word
		j := from
		for j < start && runes[j] != ' ' {
			j++
		}
		if j < start {
			from = j + 1
		}
	}
	prefix := ""
	if from > 0 {
		prefix = "…"
	}
	return trunc(prefix+string(runes[from:]), width)
}

// detailView shows the selected entry inside a titled box.
// detailPane is the selected entry expanded into the right panel (a split with
// the folder tree still on the left), sized to w×h.
// detailLines builds the full (unwindowed) content of the entry-detail pane.
func (m Model) detailLines(e *vault.Entry, w int) []string {
	i := ic()
	inW := w - 2

	// one field row: icon + label on the left, the value in the middle, and a dim
	// copy/reveal key tucked to the right.
	terms := search.HighlightTerms(m.input.Value()) // active query: highlight field matches, grep-style
	rowW := inW - 2                                 // leave a 2-cell right margin, mirroring the left indent
	row := func(icon, label, value string, vst lipgloss.Style, key string) string {
		lead := "  " + theme.Acc.Render(icon) + "  " + theme.Dimmed.Render(pad(label, 9))
		right := ""
		if key != "" {
			right = theme.Faded.Render(key)
		}
		avail := max(4, rowW-dw(lead)-dw(right)-1)
		return spread(lead+highlightTerms(trunc(value, avail), terms, vst), right, rowW)
	}

	user, userKey, userSt := e.Username, "ctrl+u", theme.Strong
	if user == "" {
		user, userKey, userSt = "—", "", theme.Faded
	}

	pwVal, pwSt, pwKey := strings.Repeat("•", 12), theme.Acc, "ctrl+r"
	if m.reveal {
		pwVal, pwSt, pwKey = e.Password.Reveal(), theme.Hi, "ctrl+r"
	}

	b := []string{
		"",
		row(i.user, "user", user, userSt, userKey),
		row(i.keyfile, "password", pwVal, pwSt, pwKey),
	}
	if e.TOTP != "" {
		if k, err := otp.Parse(e.TOTP); err == nil {
			now := time.Now()
			code := k.Code(now)
			if len(code) == 6 {
				code = code[:3] + " " + code[3:] // 428 913
			}
			b = append(b, row(i.clock, "totp", code, theme.Hi, fmt.Sprintf("%ds · ctrl+t", k.Remaining(now))))
		}
	}
	if e.URL != "" {
		b = append(b, row(i.link, "url", e.URL, theme.Dimmed, "ctrl+o"))
	}
	if len(e.Tags) > 0 {
		b = append(b, row(i.tag, "tags", strings.Join(e.Tags, ", "), theme.Dimmed, ""))
	}
	if len(e.Custom) > 0 {
		b = append(b, "", "  "+theme.Acc.Render(i.entry)+"  "+theme.Dimmed.Render("fields"))
		nameW := 0
		for _, f := range e.Custom {
			nameW = max(nameW, dw(f.Name))
		}
		nameW = min(nameW, 22)
		for _, f := range e.Custom {
			val, vst := f.Value, theme.Strong
			if f.Protected {
				if m.reveal {
					vst = theme.Hi
				} else {
					val, vst = strings.Repeat("•", 8), theme.Acc
				}
			}
			name := theme.Dimmed.Render(pad(trunc(f.Name, nameW), nameW))
			b = append(b, "     "+name+"  "+highlightTerms(trunc(val, max(4, rowW-7-nameW)), terms, vst))
		}
	}
	if !e.Modified.IsZero() {
		b = append(b, row(i.clock, "modified", e.Modified.Format("2006-01-02"), theme.Dimmed, ""))
	}
	if !e.Expiry.IsZero() {
		date := e.Expiry.Format("2006-01-02")
		txt, st := "expires "+date, theme.Dimmed
		switch d := time.Until(e.Expiry); {
		case d < 0:
			txt, st = "expired "+date, theme.Bad
		case d < 14*24*time.Hour:
			txt, st = "expires "+date+" · soon", theme.Bad
		}
		b = append(b, row(i.clock, "expiry", txt, st, ""))
	}
	if len(e.Files) > 0 {
		b = append(b, "", "  "+theme.Acc.Render(i.kdbx)+"  "+theme.Dimmed.Render("attachments · s saves them"))
		for _, a := range e.Files {
			b = append(b, "     "+theme.Strong.Render(trunc(a.Name, rowW-16))+"  "+theme.Faded.Render(humanBytes(int64(a.Size()))))
		}
	}
	if e.Notes != "" {
		b = append(b, "", "  "+theme.Acc.Render(i.note)+"  "+theme.Dimmed.Render("notes"))
		notes := strings.ReplaceAll(e.Notes, "\r\n", "\n")
		for _, ln := range strings.Split(ansi.Wrap(notes, rowW-2, " -"), "\n") {
			b = append(b, "     "+highlightTerms(ln, terms, theme.Faded))
		}
	}

	return b
}

// detailPane renders the selected entry, windowed by detailScroll so long notes
// scroll instead of overflowing the panel.
func (m Model) detailPane(w, h int) string {
	e := m.selEntry()
	if e == nil {
		return box("Entry", "", nil, w, h, true)
	}
	lines := m.detailLines(e, w)
	visible := max(1, h-2)
	scroll := clampScroll(m.detailScroll, len(lines), visible)
	title := m.breadcrumb(e)
	if terms := search.HighlightTerms(m.input.Value()); len(terms) > 0 { // highlight the query in the breadcrumb too
		title = highlightTerms(title, terms, theme.Strong)
	}
	return boxV(title, "", lines[scroll:min(scroll+visible, len(lines))], w, h, true, len(lines), scroll, 0)
}

// detailViewport is the width and visible-line count of the detail pane, matching
// vaultBody's layout, so scroll bounds line up with what's rendered.
func (m Model) detailViewport() (w, visible int) {
	panelsH := max(3, m.h-3)
	leftW := m.leftPaneW()
	return m.w - leftW - 1, max(1, panelsH-2)
}

func clampScroll(scroll, total, visible int) int {
	maxOff := total - visible
	if maxOff < 0 {
		maxOff = 0
	}
	if scroll < 0 {
		return 0
	}
	if scroll > maxOff {
		return maxOff
	}
	return scroll
}

// breadcrumb is the entry's full trail — "source › folder › … › title" — shown
// in the detail panel's top border, with the source's type icon in front.
func (m Model) breadcrumb(e *vault.Entry) string {
	crumbs := []string{e.Source}
	for _, seg := range strings.Split(e.Path, "/") {
		if seg != "" {
			crumbs = append(crumbs, seg)
		}
	}
	crumbs = append(crumbs, e.Title)
	trail := strings.Join(crumbs, " › ")
	if si, ok := m.sourceIcon(e.Source); ok {
		return si + " " + trail
	}
	return trail
}

func (m Model) countdown() string {
	if m.remaining <= 0 {
		if m.flash != "" {
			return theme.Ok.Render(trunc("  "+m.flash, m.w))
		}
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
	sec := theme.Acc
	if m.remaining <= 5 {
		sec = theme.Bad
	}
	secTxt := sec.Render(fmt.Sprintf(" %2ds ", m.remaining))
	prefix := theme.Dimmed.Render("copied ") + theme.Hi.Render(m.copiedWhat) + theme.Dimmed.Render(" · ")

	// The label (seconds + "copied X · <where>") comes first in the budget; the bar
	// gets what's left, capped so it never crowds the label off the line.
	const barMax = 32
	fixed := dw(secTxt) + dw(prefix) + 3 // leading space, a gap, the ▐ cap
	barW := min(barMax, m.w-fixed-dw(m.copied))
	if barW < 6 {
		// no room for a bar — just seconds + label, truncated to fit
		provW := max(4, m.w-dw(secTxt)-dw(prefix)-1)
		return secTxt + prefix + theme.Strong.Render(trunc(m.copied, provW))
	}
	total := int(m.timeout.Seconds())
	if total < 1 {
		total = 1
	}
	filled := min(m.remaining*barW/total, barW)
	bar := theme.Acc.Render("▐"+strings.Repeat("█", filled)) + theme.Faded.Render(strings.Repeat("░", barW-filled))
	provW := max(4, m.w-dw(bar)-dw(secTxt)-dw(prefix)-3)
	return " " + bar + secTxt + " " + prefix + theme.Strong.Render(trunc(m.copied, provW))
}

func (m Model) hints() string {
	var full string
	switch {
	case m.searchMode:
		full = "type to filter · ↑↓ pick · ↵ apply · esc cancel"
	case m.showResults():
		full = "↑↓ results · ↵ copy pw · → details · c get-cmd · g folder · / edit · esc clear"
	case m.focus == 1:
		full = "↑↓ move · ↵ copy pw · → details · c get-cmd · ^b tree · / search · ?"
	default:
		full = "↑↓ move · →/⇥ into · ← collapse · ↵ open folder · ^b tree · / search · q quit"
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
