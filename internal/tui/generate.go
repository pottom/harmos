package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/pwgen"
	"github.com/pottom/harmos/internal/theme"
)

// The Generate tab (3) is a pwgen-style password generator: options on the left,
// a single-column list of freshly generated passwords on the right (mirroring the
// vault's entry table). Read-only w.r.t. the vault — it only copies to the
// concealed clipboard. All randomness is crypto/rand (see internal/pwgen).

// genLeftW is the fixed width of the options pane; the list takes the rest.
const genLeftW = 34

// genRowLayout maps a visual row in the options pane to its field index (-1 for a
// blank spacer), so a mouse click lands on the right option.
var genRowLayout = []int{genLength, genCount, -1, genLower, genUpper, genDigit, genSymbol, -1, genAmbig, genOneEach, -1, genDo}

// Option rows in the left pane, in display order.
const (
	genLength = iota
	genCount
	genLower
	genUpper
	genDigit
	genSymbol
	genAmbig
	genOneEach
	genDo
	genRowCount
)

// regen fills genList with genCount fresh passwords, or records the error.
func (m *Model) regen() {
	ps, err := pwgen.Many(m.genOpts, m.genCount)
	if err != nil {
		m.genErr, m.genList = err.Error(), nil
		return
	}
	m.genErr, m.genList = "", ps
	if m.genSel >= len(ps) {
		m.genSel = 0
	}
}

func (m Model) updateGenerate(key string) (tea.Model, tea.Cmd) {
	if m.focus == 0 {
		return m.updateGenOptions(key)
	}
	return m.updateGenList(key)
}

// updateGenOptions handles the left (options) pane.
func (m Model) updateGenOptions(key string) (tea.Model, tea.Cmd) {
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
		if m.genRow == genDo {
			m.regen()
			m.focus = 1
			return m, nil
		}
		m.toggleGen()
		m.regen()
	case "tab":
		m.focus = 1
	case "enter", "g":
		m.regen()
		m.focus = 1
	}
	return m, nil
}

// adjustGen changes the length or count by d (clamped), on their rows only.
func (m *Model) adjustGen(d int) {
	switch m.genRow {
	case genLength:
		m.genOpts.Length = clampInt(m.genOpts.Length+d, pwgen.MinLength, pwgen.MaxLength)
		m.regen()
	case genCount:
		m.genCount = clampInt(m.genCount+d, pwgen.MinCount, pwgen.MaxCount)
		m.regen()
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
}

// updateGenList handles the right (password list) pane.
func (m Model) updateGenList(key string) (tea.Model, tea.Cmd) {
	n := len(m.genList)
	switch key {
	case "up", "ctrl+p", "k":
		if m.genSel > 0 {
			m.genSel--
		}
	case "down", "ctrl+n", "j":
		if m.genSel < n-1 {
			m.genSel++
		}
	case "pgup":
		m.genSel = max(0, m.genSel-m.genVisRows())
	case "pgdown":
		m.genSel = clampIndex(m.genSel+m.genVisRows(), n)
	case "esc", "tab", "left", "h":
		m.focus = 0
	case "r", "g":
		m.regen()
	case "enter", "ctrl+y", "c":
		if m.genSel < n {
			return m, m.copyString(m.genList[m.genSel], "password", "generated")
		}
	}
	return m, nil
}

// genVisRows is the number of password rows visible in the list pane.
func (m Model) genVisRows() int {
	return max(1, max(3, m.h-3)-2)
}

// handleGenClick routes a left-click in the Generate tab: option rows on the
// left, password rows on the right. A double-click on an already-selected option
// activates it (toggle / generate); on a selected password it copies.
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
			if dbl && already {
				if field == genDo {
					m.regen()
					m.focus = 1
				} else {
					m.toggleGen()
					m.regen()
				}
			}
		}
		return m, nil
	}
	// passwords pane
	idx := windowStart(m.genSel, m.genVisRows(), len(m.genList)) + row
	if idx >= 0 && idx < len(m.genList) {
		if dbl && idx == m.genSel && m.focus == 1 {
			return m, m.copyString(m.genList[idx], "password", "generated")
		}
		m.genSel, m.focus = idx, 1
	}
	return m, nil
}

// handleGenRightClick copies the password under the cursor — a quick copy without
// selecting first, mirroring the vault's right-click.
func (m Model) handleGenRightClick(x, y int) (tea.Model, tea.Cmd) {
	panelsH := max(3, m.h-3)
	if y < 2 || y > panelsH-1 || x <= genLeftW-1 {
		return m, nil
	}
	idx := windowStart(m.genSel, m.genVisRows(), len(m.genList)) + (y - 2)
	if idx >= 0 && idx < len(m.genList) {
		m.genSel, m.focus = idx, 1
		return m, m.copyString(m.genList[idx], "password", "generated")
	}
	return m, nil
}

func (m Model) generateView() string {
	panelsH := max(3, m.h-3)
	rightW := m.w - genLeftW - 1

	left := box("Options", "", m.genOptionLines(genLeftW-2), genLeftW, panelsH, m.focus == 0)

	vis := m.genVisRows()
	total := len(m.genList)
	sb := boolToInt(total > vis) // scrollbar steals one content column
	right := boxV("Passwords", fmt.Sprintf("%d", total),
		m.genListLines(rightW-2-sb, vis), rightW, panelsH, m.focus == 1,
		total, windowStart(m.genSel, vis, total), 0)

	ctx := m.genContext()
	if m.remaining > 0 { // a copy countdown takes over the context line, as in the vault
		ctx = m.countdown()
	}
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return m.genHeader() + "\n" + panels + "\n" + ctx + "\n" + m.footer(m.genHint())
}

// genHeader mirrors the other tabs' top line.
func (m Model) genHeader() string {
	left := brand() + theme.Faded.Render("  ·  generate")
	right := theme.Faded.Render("crypto/rand")
	return spread(left, right, m.w)
}

// genContext is the line above the hints: the strength of the current settings,
// or an error.
func (m Model) genContext() string {
	if m.genErr != "" {
		return theme.Bad.Render(trunc("  "+m.genErr, m.w))
	}
	o := m.genOpts
	txt := fmt.Sprintf("  %d chars · pool %d · ≈%.0f bits", o.Length, o.PoolSize(), o.EntropyBits())
	return theme.Faded.Render(trunc(txt, m.w))
}

// genHint is the footer, contextual to the focused pane.
func (m Model) genHint() string {
	if m.focus == 0 {
		return theme.Faded.Render("↑↓ move · space toggle · ←/→ adjust · ↵ generate · ⇥ list")
	}
	return theme.Faded.Render("↑↓ move · ↵ copy · r regenerate · esc options · click/right-click")
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
		mark := "[ ]"
		markSt := theme.Faded
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
	num(genCount, "Count", m.genCount)
	out = append(out, "")
	chk(genLower, "a–z", o.Lower, "")
	chk(genUpper, "A–Z", o.Upper, "")
	chk(genDigit, "0–9", o.Digit, "")
	chk(genSymbol, "symbols", o.Symbol, pwgen.Symbol)
	out = append(out, "")
	chk(genAmbig, "no ambiguous", o.AvoidAmbig, "0O1lI")
	chk(genOneEach, "one of each", o.OneEach, "")
	out = append(out, "")
	if m.focus == 0 && m.genRow == genDo {
		out = append(out, theme.SelRow.Width(w).Render(trunc("▸ Generate", w)))
	} else {
		out = append(out, "  "+theme.Ok.Render("[ Generate ]"))
	}
	return out
}

// genListLines renders the passwords one per row (like the vault entry table),
// windowed to the visible rows and syntax-coloured.
func (m Model) genListLines(w, rows int) []string {
	if m.genErr != "" {
		return []string{"", theme.Bad.Render("  " + m.genErr)}
	}
	if len(m.genList) == 0 {
		return []string{"", theme.Faded.Render("  press ↵ to generate")}
	}
	start := windowStart(m.genSel, rows, len(m.genList))
	end := min(start+rows, len(m.genList))
	var out []string
	for k := start; k < end; k++ {
		p := m.genList[k]
		if k == m.genSel && m.focus == 1 {
			out = append(out, theme.SelRow.Width(w).Render(trunc("  "+p, w)))
		} else {
			out = append(out, "  "+colorizePw(p))
		}
	}
	return out
}

// colorizePw renders a password with its character classes in distinct theme
// colours — letters plain, digits in accent, symbols emphasised — so a password
// reads at a glance and fits the vault's colour language. Runs of the same class
// are batched into one styled span.
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
