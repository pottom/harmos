package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/theme"
)

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
	if w < 4 || h < 2 {
		return strings.Repeat(" ", max(0, w))
	}
	bc := borderStyle(active)
	inW := w - 2

	lines := []string{boxTop(title, info, inW, active)}
	for i := 0; i < h-2; i++ {
		c := ""
		if i < len(content) {
			c = content[i]
		}
		lines = append(lines, bc.Render("│")+pad(c, inW)+bc.Render("│"))
	}
	lines = append(lines, bc.Render("╰"+strings.Repeat("─", inW)+"╯"))
	return strings.Join(lines, "\n")
}

// boxTop builds the top border line "╭─ Title ──…── info ─╮" fitting inW between
// the corners.
func boxTop(title, info string, inW int, active bool) string {
	bc := borderStyle(active)
	tstyle := theme.Dimmed
	if active {
		tstyle = theme.Strong
	}
	left := bc.Render("─ ") + tstyle.Render(title) + bc.Render(" ")
	if dw(left) > inW {
		left = bc.Render("─ ") + tstyle.Render(trunc(title, max(1, inW-4))) + bc.Render(" ")
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
