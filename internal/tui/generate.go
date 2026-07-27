package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/pwgen"
	"github.com/pottom/harmos/internal/theme"
)

// genOptsFromConfig builds the generator options from saved preferences, falling
// back to the built-in defaults for anything unset.
func genOptsFromConfig(cfg *config.Config) pwgen.Options {
	o := pwgen.Default()
	if cfg == nil {
		return o
	}
	if cfg.GenLength >= pwgen.MinLength && cfg.GenLength <= pwgen.MaxLength {
		o.Length = cfg.GenLength
	}
	setBool(&o.Lower, cfg.GenLower)
	setBool(&o.Upper, cfg.GenUpper)
	setBool(&o.Digit, cfg.GenDigit)
	setBool(&o.Symbol, cfg.GenSymbol)
	setBool(&o.AvoidAmbig, cfg.GenNoAmbig)
	setBool(&o.OneEach, cfg.GenOneEach)
	o.Exclude = cfg.GenExclude
	return o
}

func setBool(dst, src *bool) {
	if src != nil {
		*dst = *src
	}
}

// saveGenOpts persists the current generator options (best-effort — a failed
// write just means they aren't remembered next launch). It only writes when a
// config file already exists, so toggling an option never creates one out of thin
// air.
func (m *Model) saveGenOpts() {
	if m.configPath == "" {
		return
	}
	if _, err := os.Stat(m.configPath); err != nil {
		return
	}
	o := m.genOpts
	_ = config.SetGenerator(m.configPath, o.Length, o.Lower, o.Upper, o.Digit, o.Symbol, o.AvoidAmbig, o.OneEach, o.Exclude)
}

// The Generate tab (2) is a focused password generator: options on the left, and
// on the right one prominent password with a strength meter, centred in the pane,
// plus a short "recent" list of this session's earlier rolls. Read-only w.r.t.
// the vault — it only copies to the concealed clipboard. All randomness is
// crypto/rand (see internal/pwgen). There is deliberately no "count": every
// password from the same settings is equivalent, so one at a time + reroll is the
// honest model (what 1Password / Bitwarden do).

const (
	genLeftW   = 34 // width of the options pane; the password pane takes the rest
	genHistMax = 10 // how many recent rolls to keep
)

// genRowLayout maps a visual row in the options pane to its field index (-1 for a
// blank spacer), so a mouse click lands on the right option.
var genRowLayout = []int{genLength, -1, genLower, genUpper, genDigit, genSymbol, -1, genAmbig, genOneEach, genExclude}

// Option rows in the left pane, in display order. There is no "Generate" button:
// changing any option regenerates automatically, and reroll lives on the password
// pane (r), so a button would be redundant.
const (
	genLength = iota
	genLower
	genUpper
	genDigit
	genSymbol
	genAmbig
	genOneEach
	genExclude
	genRowCount
)

// reroll generates one password with the current options and makes it the hero,
// keeping a bounded recent history.
func (m *Model) reroll() {
	p, err := pwgen.Generate(m.genOpts)
	if err != nil {
		m.genErr = err.Error()
		return
	}
	m.genErr = ""
	m.genList = append(m.genList, p)
	if len(m.genList) > genHistMax {
		m.genList = m.genList[len(m.genList)-genHistMax:]
	}
	m.genSel = len(m.genList) - 1
}

// resetGen clears the history and rolls a fresh hero — used on entry and whenever
// the options change, so the recent list always reflects the current settings.
func (m *Model) resetGen() {
	m.genList, m.genSel = nil, 0
	m.reroll()
}

func (m Model) updateGenerate(key string) (tea.Model, tea.Cmd) {
	if m.focus == 0 {
		return m.updateGenOptions(key)
	}
	return m.updateGenList(key)
}

// updateGenOptions handles the left (options) pane.
func (m Model) updateGenOptions(key string) (tea.Model, tea.Cmd) {
	// On the exclude row, printable keys type into the excluded-character set
	// (backspace deletes); arrows/tab/enter still navigate.
	if m.genRow == genExclude {
		if key == "backspace" {
			if r := []rune(m.genOpts.Exclude); len(r) > 0 {
				m.genOpts.Exclude = string(r[:len(r)-1])
				m.resetGen()
				m.saveGenOpts()
			}
			return m, nil
		}
		if len(key) == 1 && key[0] > ' ' && key[0] < 0x7f { // a printable ASCII char
			if !strings.ContainsRune(m.genOpts.Exclude, rune(key[0])) {
				m.genOpts.Exclude += key
				m.resetGen()
				m.saveGenOpts()
			}
			return m, nil
		}
	}
	switch key {
	case "up", "ctrl+p", "k":
		if m.genRow > 0 {
			m.genRow--
		}
	case "down", "ctrl+n", "j":
		if m.genRow < genRowCount-1 {
			m.genRow++
		}
	case "left", "h", "-":
		m.adjustGen(-1)
	case "right", "l", "+", "=":
		m.adjustGen(+1)
	case " ":
		m.toggleGen()
		m.resetGen()
	case "tab", "enter", "g": // options already regenerate live — just show the password
		m.focus = 1
	}
	return m, nil
}

// adjustGen changes the length by d (clamped) on the Length row.
func (m *Model) adjustGen(d int) {
	if m.genRow == genLength {
		m.genOpts.Length = clampInt(m.genOpts.Length+d, pwgen.MinLength, pwgen.MaxLength)
		m.resetGen()
		m.saveGenOpts()
	}
}

// toggleGen flips the boolean option on the current row.
func (m *Model) toggleGen() {
	switch m.genRow {
	case genLower:
		m.genOpts.Lower = !m.genOpts.Lower
	case genUpper:
		m.genOpts.Upper = !m.genOpts.Upper
	case genDigit:
		m.genOpts.Digit = !m.genOpts.Digit
	case genSymbol:
		m.genOpts.Symbol = !m.genOpts.Symbol
	case genAmbig:
		m.genOpts.AvoidAmbig = !m.genOpts.AvoidAmbig
	case genOneEach:
		m.genOpts.OneEach = !m.genOpts.OneEach
	}
	m.saveGenOpts()
}

// updateGenList handles the right (password) pane.
func (m Model) updateGenList(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "ctrl+p", "k": // up the list = towards the newer rolls
		if m.genSel < len(m.genList)-1 {
			m.genSel++
		}
	case "down", "ctrl+n", "j": // down the list = towards the older rolls
		if m.genSel > 0 {
			m.genSel--
		}
	case "esc", "tab", "left", "h":
		m.focus = 0
	case "r", "g", " ":
		m.reroll()
	case "enter", "ctrl+y", "c":
		if m.genSel < len(m.genList) {
			return m, m.copyString(m.genList[m.genSel], "password", "generated")
		}
	}
	return m, nil
}

// genVisRows is the number of content rows in the password pane.
func (m Model) genVisRows() int {
	return max(1, max(3, m.h-3)-2)
}

// genOrder returns the whole history newest-first (empty when there is nothing to
// pick from — a single roll).
func (m Model) genOrder() []int {
	if len(m.genList) <= 1 {
		return nil
	}
	out := make([]int, len(m.genList))
	for i := range out {
		out[i] = len(m.genList) - 1 - i
	}
	return out
}

// genLayout returns the vertical top-pad and the content row where the recent
// list begins, so the renderer and the mouse hit-test agree. The top-pad is fixed
// for the *maximum* history, so the hero never drifts as rolls accumulate — the
// list simply grows downward into the reserved space.
func (m Model) genLayout(rows int) (top, listStart int, order []int) {
	order = m.genOrder()
	top = max(0, (rows-genInnerMax)/2)
	listStart = top + genHeroInner + 2 // past the hero block + blank + header
	return top, listStart, order
}

// genHeroInner is the number of lines in the hero block (password, blank, bar,
// blank, affordance); genInnerMax reserves room for the fullest recent list so the
// hero's position is stable.
const (
	genHeroInner = 10 // framed password (3) + blank + bar + bits + label + breakdown + blank + affordance
	genInnerMax  = genHeroInner + 2 + (genHistMax - 1)
)

// handleGenClick routes a left-click in the Generate tab.
func (m Model) handleGenClick(x, y int, dbl bool) (tea.Model, tea.Cmd) {
	panelsH := max(3, m.h-3)
	if y < 2 || y > panelsH-1 {
		return m, nil
	}
	row := y - 2
	if x <= genLeftW-1 { // options pane
		if row < len(genRowLayout) && genRowLayout[row] >= 0 {
			field := genRowLayout[row]
			already := m.focus == 0 && m.genRow == field
			m.genRow, m.focus = field, 0
			if dbl && already { // double-click toggles the option under the cursor
				m.toggleGen()
				m.resetGen()
			}
		}
		return m, nil
	}
	// password pane: the hero block, then the recent list.
	m.focus = 1
	_, recentStart, recent := m.genLayout(m.genVisRows())
	if ri := row - recentStart; ri >= 0 && ri < len(recent) {
		m.genSel = recent[ri] // pick a recent one
		if dbl {
			return m, m.copyString(m.genList[m.genSel], "password", "generated")
		}
		return m, nil
	}
	if dbl && m.genSel < len(m.genList) { // double-click the hero copies it
		return m, m.copyString(m.genList[m.genSel], "password", "generated")
	}
	return m, nil
}

// handleGenRightClick copies the password under the cursor (the hero, or a recent
// entry) — a quick copy, mirroring the vault's right-click.
func (m Model) handleGenRightClick(x, y int) (tea.Model, tea.Cmd) {
	panelsH := max(3, m.h-3)
	if y < 2 || y > panelsH-1 || x <= genLeftW-1 || len(m.genList) == 0 {
		return m, nil
	}
	m.focus = 1
	_, recentStart, recent := m.genLayout(m.genVisRows())
	if ri := (y - 2) - recentStart; ri >= 0 && ri < len(recent) {
		m.genSel = recent[ri]
	}
	return m, m.copyString(m.genList[m.genSel], "password", "generated")
}

func (m Model) generateView() string {
	panelsH := max(3, m.h-3)
	rightW := m.w - genLeftW - 1

	left := box("Options", "", m.genOptionLines(genLeftW-2), genLeftW, panelsH, m.focus == 0)
	right := box("Password", cursorInfo(m.genSel, len(m.genList)),
		m.genPasswordLines(rightW-2, m.genVisRows()), rightW, panelsH, m.focus == 1)

	ctx := m.genContext()
	if m.remaining > 0 { // a copy countdown takes over the context line, as in the vault
		ctx = m.countdown()
	}
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return m.genHeader() + "\n" + panels + "\n" + ctx + "\n" + m.footer(m.genHint())
}

// genHeader mirrors the other tabs' top line.
func (m Model) genHeader() string {
	left := m.brandVersion() + theme.Faded.Render("  ·  generate")
	right := theme.Faded.Render("crypto/rand")
	return spread(left, right, m.w)
}

// genContext is the line above the hints: the current settings, or an error.
func (m Model) genContext() string {
	if m.genErr != "" {
		return theme.Bad.Render(trunc("  "+m.genErr, m.w))
	}
	o := m.genOpts
	return theme.Faded.Render(trunc(fmt.Sprintf("  %d chars · pool %d", o.Length, o.PoolSize()), m.w))
}

// genHint is the footer, contextual to the focused pane.
func (m Model) genHint() string {
	if m.focus == 0 {
		return theme.Faded.Render("↑↓ move · space toggle · ←/→ length · ↵/⇥ password")
	}
	return theme.Faded.Render("↑↓ recent · ↵ copy · r reroll · esc options · click/right-click")
}

// genOptionLines renders the left options pane.
func (m Model) genOptionLines(w int) []string {
	o := m.genOpts
	var out []string
	row := func(idx int, plain, styled string) {
		if m.focus == 0 && m.genRow == idx {
			out = append(out, theme.SelRow.Width(w).Render(trunc("▸ "+plain, w)))
		} else {
			out = append(out, "  "+styled)
		}
	}
	num := func(idx int, label string, val int) {
		v := fmt.Sprintf("%-4d", val)
		row(idx, pad(label, 10)+v+"−/+",
			theme.Strong.Render(pad(label, 10))+theme.Hi.Render(v)+theme.Faded.Render("−/+"))
	}
	chk := func(idx int, label string, on bool, extra string) {
		mark, markSt := "[ ]", theme.Faded
		if on {
			mark, markSt = "[x]", theme.Ok
		}
		plain := mark + " " + label
		styled := markSt.Render(mark) + " " + theme.Strong.Render(label)
		if extra != "" {
			plain += "  " + extra
			styled += "  " + theme.Dimmed.Render(extra)
		}
		row(idx, plain, styled)
	}

	num(genLength, "Length", o.Length)
	out = append(out, "")
	chk(genLower, "a–z", o.Lower, "")
	chk(genUpper, "A–Z", o.Upper, "")
	chk(genDigit, "0–9", o.Digit, "")
	chk(genSymbol, "symbols", o.Symbol, pwgen.Symbol)
	out = append(out, "")
	chk(genAmbig, "no ambiguous", o.AvoidAmbig, "0O1lI")
	chk(genOneEach, "one of each", o.OneEach, "")
	out = append(out, "")
	exVal, exSt := o.Exclude, theme.Hi
	if exVal == "" {
		exVal, exSt = "(type to add)", theme.Faded
	}
	row(genExclude, pad("exclude", 10)+exVal, theme.Strong.Render(pad("exclude", 10))+exSt.Render(exVal))
	return out
}

// genPasswordLines renders the hero password (grouped, syntax-coloured, with a
// strength bar) and the recent list, centred in the w×rows pane.
func (m Model) genPasswordLines(w, rows int) []string {
	center := func(s string) string {
		if d := dw(s); d < w {
			return strings.Repeat(" ", (w-d)/2) + s
		}
		return s
	}
	if m.genErr != "" {
		return []string{"", center(theme.Bad.Render(m.genErr))}
	}
	if len(m.genList) == 0 {
		return []string{"", center(theme.Faded.Render("press ↵ to generate"))}
	}
	o := m.genOpts
	bits := o.EntropyBits()
	label, lst := strengthLabel(bits)

	// The hero sits in an accent-bordered box so the eye lands on it immediately.
	frame := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(0, 3).
		Render(heroPassword(m.genList[m.genSel]))
	var inner []string
	for _, ln := range strings.Split(frame, "\n") {
		inner = append(inner, center(ln))
	}
	inner = append(inner,
		center(theme.Faded.Render(classBreakdown(m.genList[m.genSel]))), // directly under the box
		"",
		center(strengthBar(bits, 24)),
		center(theme.Strong.Render(fmt.Sprintf("%.0f bits", bits))),
		center(lst.Render(label)),
		"",
		center(theme.Faded.Render("↵ copy    r reroll")),
	)
	// The recent list shows the whole history, newest first, with a ▸ marking the
	// current pick so ↑↓ navigation is visible.
	if order := m.genOrder(); len(order) > 0 {
		inner = append(inner, "", center(theme.Dimmed.Render("recent")))
		for _, idx := range order {
			p := m.genList[idx]
			if idx == m.genSel {
				inner = append(inner, center(theme.Acc.Render("▸ ")+theme.Hi.Render(p)))
			} else {
				inner = append(inner, center("  "+theme.Faded.Render(p)))
			}
		}
	}

	top := max(0, (rows-genInnerMax)/2) // fixed — the hero never drifts
	out := make([]string, 0, top+len(inner))
	for range top {
		out = append(out, "")
	}
	return append(out, inner...)
}

// classBreakdown counts how many of each character class the password contains,
// e.g. "12 lower · 5 upper · 2 num · 1 sym" — so you can see its make-up at a
// glance (and confirm "one of each").
func classBreakdown(p string) string {
	var lo, up, num, sym int
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z':
			lo++
		case r >= 'A' && r <= 'Z':
			up++
		case r >= '0' && r <= '9':
			num++
		default:
			sym++
		}
	}
	var parts []string
	add := func(n int, label string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(lo, "lower")
	add(up, "upper")
	add(num, "num")
	add(sym, "sym")
	return strings.Join(parts, " · ")
}

// heroPassword renders the password contiguously and syntax-coloured. It is shown
// exactly as generated — no separators — so there is never any doubt about which
// characters the password contains (it never contains spaces itself).
func heroPassword(p string) string {
	return colorizePw(p)
}

// strengthBar is a filled/empty block bar for the entropy, capped at 128 bits.
func strengthBar(bits float64, w int) string {
	frac := bits / 128
	if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(w) + 0.5)
	_, st := strengthLabel(bits)
	return st.Render(strings.Repeat("█", filled)) + theme.Faded.Render(strings.Repeat("░", w-filled))
}

// strengthLabel maps entropy bits to a word and a colour.
func strengthLabel(bits float64) (string, lipgloss.Style) {
	switch {
	case bits < 60:
		return "weak", theme.Bad
	case bits < 90:
		return "fair", theme.Acc
	case bits < 120:
		return "strong", theme.Ok
	default:
		return "very strong", theme.Hi
	}
}

// colorizePw renders a password with its character classes in distinct theme
// colours — letters plain, digits in accent, symbols emphasised — so a password
// reads at a glance. Runs of the same class are batched into one styled span.
func colorizePw(s string) string {
	class := func(r rune) int {
		switch {
		case r >= '0' && r <= '9':
			return 1
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			return 0
		default:
			return 2
		}
	}
	styles := []lipgloss.Style{theme.Strong, theme.Acc, theme.Hi}
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); {
		c := class(runes[i])
		j := i
		for j < len(runes) && class(runes[j]) == c {
			j++
		}
		b.WriteString(styles[c].Render(string(runes[i:j])))
		i = j
	}
	return b.String()
}

// clampInt keeps v within [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
