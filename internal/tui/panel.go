package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/theme"
)

// button renders a modal action button. The focused one is bracketed and filled
// (reverse video); the others are plain. danger → red (Remove), else green
// (Save/Confirm).
func button(label string, danger, focused bool) string {
	col := theme.OK
	if danger {
		col = theme.Warn
	}
	st := lipgloss.NewStyle().Foreground(col).Bold(true)
	if focused {
		// Brackets on the focused one, not the other way round: a reader who
		// does not know the convention reads brackets as "this is the one", and
		// the reverse video that used to be the only marker is invisible in a
		// screenshot, a log, or a terminal that does not do it.
		return st.Reverse(true).Render("[ " + label + " ]")
	}
	return st.Render("  " + label + "  ")
}

// panelState is how a panel's border should read.
//
// Editing is its own state rather than a flavour of active, because a panel
// being edited is a mode the user is in — like vim's insert mode — and losing
// track of which mode you are in is how you type a password into a search box.
// The amber border says so at a glance, and the footer says so in words.
type panelState int

const (
	panelInactive panelState = iota
	panelActive
	panelEditing
)

func borderStyle(st panelState) lipgloss.Style {
	switch st {
	case panelEditing:
		// The same token as the tree icons: one colour means "writable
		// territory" everywhere, so a theme has a single knob for it.
		return lipgloss.NewStyle().Foreground(theme.Writable)
	case panelActive:
		return lipgloss.NewStyle().Foreground(theme.Accent)
	}
	return lipgloss.NewStyle().Foreground(theme.Faint)
}

// panelStateOf is the common case: a panel that is either focused or not.
func panelStateOf(active bool) panelState {
	if active {
		return panelActive
	}
	return panelInactive
}

// box renders content inside a rounded, titled border of exactly w×h cells. The
// title sits in the top border; info (e.g. "3/42") is tucked to its right. A
// focused panel gets the accent border, else a faint one.
func box(title, info string, content []string, w, h int, active bool) string {
	return boxV(title, info, content, w, h, active, len(content), 0, 0)
}

// boxState is box with the border state named outright, for the editor's amber.
func boxState(title, info string, content []string, w, h int, st panelState) string {
	return boxVState(title, info, content, w, h, st, len(content), 0, 0)
}

// boxV is box with a vertical scrollbar in the right inner column when the list
// overflows. total is the full item count, offset the first visible item, and
// headerRows the number of pinned top rows (a table header) the bar skips.
func boxV(title, info string, content []string, w, h int, active bool, total, offset, headerRows int) string {
	return boxVState(title, info, content, w, h, panelStateOf(active), total, offset, headerRows)
}

func boxVState(title, info string, content []string, w, h int, st panelState, total, offset, headerRows int) string {
	if w < 4 || h < 2 {
		return strings.Repeat(" ", max(0, w))
	}
	bc := borderStyle(st)
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

	lines := []string{boxTopState(title, info, inW, st)}
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

// boxTopState builds the top border line "╭─ Title ──…── info ─╮" fitting inW
// between the corners.
func boxTopState(title, info string, inW int, st panelState) string {
	bc := borderStyle(st)
	tstyle := theme.Dimmed
	if st != panelInactive {
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
	// The same for the marker on the right: the Changes tab's tally arrives
	// already coloured, and wrapping it in another style would cut every colour
	// short at the first reset.
	infoed := func(s string) string {
		if strings.Contains(s, "\x1b") {
			return s
		}
		return theme.Faded.Render(s)
	}
	// The row between the corners is exactly inW columns — no more, whatever the
	// title and the marker are. It used to clamp the fill at zero and let the
	// title push past, which made a narrow panel one column wider at the top than
	// at the bottom: the collapsed tree rail overflowed the terminal by a column.
	//
	// Both decorations degrade rather than overflow: the title is truncated and
	// then dropped, and the marker is dropped when what is left cannot hold it.
	left, right := "", ""
	leftW, rightW := 0, 0
	if t := trunc(title, inW-3); t != "" { // "─ " + title + " "
		left, leftW = bc.Render("─ ")+titled(t)+bc.Render(" "), 3+dw(t)
	}
	if info != "" && inW-leftW >= dw(info)+3 { // " " + info + " ─"
		right, rightW = bc.Render(" ")+infoed(info)+bc.Render(" ─"), dw(info)+3
	}
	fill := max(0, inW-leftW-rightW)
	return bc.Render("╭") + left + bc.Render(strings.Repeat("─", fill)) + right + bc.Render("╮")
}
