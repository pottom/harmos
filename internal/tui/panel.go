package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/theme"
)

// button renders a modal action button: an outlined "[ Label ]" normally, a
// filled (reverse-video) colored button when focused. danger → red (Remove),
// else green (Save/Confirm).
func button(label string, danger, focused bool) string {
	col := theme.OK
	if danger {
		col = theme.Warn
	}
	st := lipgloss.NewStyle().Foreground(col).Bold(true)
	if focused {
		return st.Reverse(true).Render(" " + label + " ")
	}
	return st.Render("[ " + label + " ]")
}

func borderStyle(active bool) lipgloss.Style {
	if active {
		return lipgloss.NewStyle().Foreground(theme.Accent)
	}
	return lipgloss.NewStyle().Foreground(theme.Faint)
}

// box renders content inside a rounded, titled border of exactly w×h cells. The
// title sits in the top border; info (e.g. "3/42") is tucked to its right. A
// focused panel gets the accent border, else a faint one.
func box(title, info string, content []string, w, h int, active bool) string {
	return boxV(title, info, content, w, h, active, len(content), 0, 0)
}

// boxV is box with a vertical scrollbar in the right inner column when the list
// overflows. total is the full item count, offset the first visible item, and
// headerRows the number of pinned top rows (a table header) the bar skips.
func boxV(title, info string, content []string, w, h int, active bool, total, offset, headerRows int) string {
	if w < 4 || h < 2 {
		return strings.Repeat(" ", max(0, w))
	}
	bc := borderStyle(active)
	inW := w - 2
	inner := h - 2

	listRows := inner - headerRows
	bar := listRows > 0 && total > listRows
	cw := inW
	tStart, tSize := 0, 0
	if bar {
		cw = inW - 1 // reserve the last column for the scrollbar
		tStart, tSize = scrollRange(listRows, total, offset)
	}

	lines := []string{boxTop(title, info, inW, active)}
	for i := 0; i < inner; i++ {
		c := ""
		if i < len(content) {
			c = content[i]
		}
		row := pad(c, cw)
		if bar {
			g := " "
			if li := i - headerRows; li >= 0 {
				if li >= tStart && li < tStart+tSize {
					g = theme.Acc.Render("┃")
				} else {
					g = theme.Faded.Render("│")
				}
			}
			row += g
		}
		lines = append(lines, bc.Render("│")+row+bc.Render("│"))
	}
	lines = append(lines, bc.Render("╰"+strings.Repeat("─", inW)+"╯"))
	return strings.Join(lines, "\n")
}

// scrollRange sizes and positions the scrollbar thumb: size ∝ the visible
// fraction, position ∝ the scroll offset.
func scrollRange(rows, total, offset int) (start, size int) {
	size = rows * rows / total
	if size < 1 {
		size = 1
	}
	if size > rows {
		size = rows
	}
	if maxOff := total - rows; maxOff > 0 {
		start = offset * (rows - size) / maxOff
	}
	if start < 0 {
		start = 0
	}
	if start+size > rows {
		start = rows - size
	}
	return start, size
}

// boxTop builds the top border line "╭─ Title ──…── info ─╮" fitting inW between
// the corners.
func boxTop(title, info string, inW int, active bool) string {
	bc := borderStyle(active)
	tstyle := theme.Dimmed
	if active {
		tstyle = theme.Strong
	}
	// A title that already carries styling (e.g. a search-highlighted breadcrumb)
	// is rendered as-is; a plain one gets the title style.
	titled := func(s string) string {
		if strings.Contains(s, "\x1b") {
			return s
		}
		return tstyle.Render(s)
	}
	left := bc.Render("─ ") + titled(title) + bc.Render(" ")
	if dw(left) > inW {
		left = bc.Render("─ ") + titled(trunc(title, max(1, inW-4))) + bc.Render(" ")
	}
	right := ""
	if info != "" {
		right = bc.Render(" ") + theme.Faded.Render(info) + bc.Render(" ─")
	}
	fill := inW - dw(left) - dw(right)
	if fill < 0 {
		fill = 0
	}
	return bc.Render("╭") + left + bc.Render(strings.Repeat("─", fill)) + right + bc.Render("╮")
}
