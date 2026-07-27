package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/otp"
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
	return m.vaultBody() // browse, search, and entry detail share this frame
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

	leftW := min(42, max(18, m.w*2/5))
	rightW := m.w - leftW - 1 // 1-column gap between panels

	flat := m.visible()
	treeRows := panelsH - 2
	treeSB := boolToInt(len(flat) > treeRows) // scrollbar steals one content column
	folders := boxV("Folders", cursorInfo(m.tsel, len(flat)),
		m.treeLines(leftW-2-treeSB, treeRows), leftW, panelsH, m.focus == 0 && !m.showResults() && !m.detail,
		len(flat), windowStart(m.tsel, treeRows, len(flat)), 0)

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
	switch {
	case m.focus == 0:
		return theme.Faded.Render("↑↓ pick · →/↵ open · 1 Vault")
	case m.setCat == catTheme:
		return theme.Faded.Render("↑↓ preview · ↵ save · ← back")
	case m.setCat == catIcons:
		return theme.Faded.Render("space toggle Nerd Font · ← back")
	default:
		return theme.Faded.Render("↑↓ select · a add · e edit · s sync · p save-pw · x clear-pw · d remove · ← back")
	}
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
	titleW := max(8, w*4/10)
	userW := max(6, w*3/10)
	out := []string{theme.Dimmed.Render(pad("Title", titleW) + " " + pad("Username", userW) + " Password")}
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
	start := windowStart(m.sel, avail, len(m.results))
	end := min(start+avail, len(m.results))
	q := m.input.Value()
	locW := max(4, w-titleW-3)
	for k := start; k < end; k++ {
		r := m.results[k]
		e := r.Entry
		loc := e.Source
		if e.Path != "" {
			loc += " · " + e.Path
		}
		if k == m.sel {
			tail := loc
			if r.Field != "" { // badge: which field matched, when it's not the title
				tail += " · " + r.Field
			}
			plain := pad(i.entry+" "+e.Title, titleW) + " " + tail
			out = append(out, theme.SelRow.Width(w).Render(trunc(plain, w)))
			continue
		}
		locCell := theme.Dimmed.Render(trunc(loc, locW))
		if r.Field != "" {
			bw := dw(r.Field) + 3
			locCell = theme.Dimmed.Render(trunc(loc, max(4, locW-bw))) + theme.Faded.Render(" · ") + theme.Acc.Render(r.Field)
		}
		out = append(out, theme.Faded.Render(i.entry+" ")+highlight(pad(trunc(e.Title, titleW-2), titleW-2), q, theme.Strong)+" "+locCell)
	}
	return out
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
	q := m.input.Value() // active query: highlight matches in the fields, grep-style
	rowW := inW - 2      // leave a 2-cell right margin, mirroring the left indent
	row := func(icon, label, value string, vst lipgloss.Style, key string) string {
		lead := "  " + theme.Acc.Render(icon) + "  " + theme.Dimmed.Render(pad(label, 9))
		right := ""
		if key != "" {
			right = theme.Faded.Render(key)
		}
		avail := max(4, rowW-dw(lead)-dw(right)-1)
		return spread(lead+highlight(trunc(value, avail), q, vst), right, rowW)
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
			b = append(b, "     "+name+"  "+highlight(trunc(val, max(4, rowW-7-nameW)), q, vst))
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
			b = append(b, "     "+highlight(ln, q, theme.Faded))
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
	return boxV(m.breadcrumb(e), "", lines[scroll:min(scroll+visible, len(lines))], w, h, true, len(lines), scroll, 0)
}

// detailViewport is the width and visible-line count of the detail pane, matching
// vaultBody's layout, so scroll bounds line up with what's rendered.
func (m Model) detailViewport() (w, visible int) {
	panelsH := max(3, m.h-3)
	leftW := min(42, max(18, m.w*2/5))
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
		full = "↑↓ move · ↵ copy pw · → details · c get-cmd · ← back · / search · ?"
	default:
		full = "↑↓ move · →/⇥ into · ← collapse · ↵ open folder · / search · q quit · ?"
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
				{"i", "Toggle Nerd Font icons"},
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
	if len(m.excluded) > 0 {
		fmt.Fprint(&b, "\n\n"+theme.Bad.Render("Unavailable sources"))
		for _, ex := range m.excluded {
			fmt.Fprint(&b, "\n"+theme.Strong.Render(padLeft(ex.Source, keyW))+"    "+theme.Faded.Render(trunc(ex.Reason, max(10, m.w/2))))
		}
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 3).
		Render(b.String())
	return lipgloss.Place(max(1, m.w), max(1, m.h), lipgloss.Center, lipgloss.Center, panel)
}
