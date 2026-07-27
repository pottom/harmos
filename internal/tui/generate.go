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
// a grid of freshly generated passwords on the right. Read-only w.r.t. the vault
// — it only copies to the concealed clipboard. All randomness is crypto/rand
// (see internal/pwgen).

// genLeftW is the fixed width of the options pane; the grid takes the rest.
const genLeftW = 34

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

// updateGenList handles the right (grid) pane.
func (m Model) updateGenList(key string) (tea.Model, tea.Cmd) {
	cols := m.genGridCols()
	n := len(m.genList)
	switch key {
	case "left", "h":
		if m.genSel > 0 {
			m.genSel--
		}
	case "right", "l":
		if m.genSel < n-1 {
			m.genSel++
		}
	case "up", "ctrl+p", "k":
		if m.genSel-cols >= 0 {
			m.genSel -= cols
		}
	case "down", "ctrl+n", "j":
		if m.genSel+cols < n {
			m.genSel += cols
		}
	case "pgup":
		m.genSel = max(0, m.genSel-cols*m.genVisRows())
	case "pgdown":
		m.genSel = clampIndex(m.genSel+cols*m.genVisRows(), n)
	case "esc", "tab":
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

// genVisRows is the number of grid rows visible in the passwords pane.
func (m Model) genVisRows() int {
	return max(1, max(3, m.h-3)-2)
}

// genGridCols is how many password cells fit across the passwords pane — shared by
// the renderer and the grid navigation so ←/→/↑/↓ match what is drawn.
func (m Model) genGridCols() int {
	rightInner := (m.w - genLeftW - 1) - 2 // box inner width
	return gridCols(rightInner-2, m.genOpts.Length)
}

// gridCols fits fixed-width cells (plus a 2-cell gap) into w.
func gridCols(w, cellW int) int {
	if cellW < 1 {
		cellW = 1
	}
	c := (w + 2) / (cellW + 2)
	if c < 1 {
		c = 1
	}
	return c
}

func (m Model) generateView() string {
	panelsH := max(3, m.h-3)
	rightW := m.w - genLeftW - 1

	left := box("Options", "", m.genOptionLines(genLeftW-2), genLeftW, panelsH, m.focus == 0)
	right := box("Passwords", fmt.Sprintf("%d", len(m.genList)), m.genGridLines(m.genVisRows()), rightW, panelsH, m.focus == 1)

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
	return theme.Faded.Render("↑↓←→ move · ↵ copy · r regenerate · esc options")
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

// genGridLines renders the passwords as a grid of fixed-width cells; rows is how
// many grid rows are visible.
func (m Model) genGridLines(rows int) []string {
	if m.genErr != "" {
		return []string{"", theme.Bad.Render("  " + m.genErr)}
	}
	if len(m.genList) == 0 {
		return []string{"", theme.Faded.Render("  press ↵ to generate")}
	}
	cols := m.genGridCols()
	total := len(m.genList)
	totalRows := (total + cols - 1) / cols
	start := windowStart(m.genSel/cols, rows, totalRows)

	var out []string
	for r := start; r < min(start+rows, totalRows); r++ {
		var b strings.Builder
		b.WriteString("  ")
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			if idx >= total {
				break
			}
			cell := m.genList[idx]
			if idx == m.genSel && m.focus == 1 {
				b.WriteString(theme.SelRow.Render(cell))
			} else {
				b.WriteString(theme.Strong.Render(cell))
			}
			if c < cols-1 {
				b.WriteString("  ")
			}
		}
		out = append(out, b.String())
	}
	return out
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
