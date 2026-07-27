package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleClick routes a left-click at (x, y) to a tab switch or a list-row
// selection, matching the layout in vaultBody. A single click selects a row; a
// double-click opens it (a folder expands/collapses, an entry opens its detail).
func (m Model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	// Overlays and modal phases own the whole screen; ignore stray clicks.
	if m.locked || m.help || m.attach != attachNone {
		return m, nil
	}
	dbl := time.Since(m.clickAt) < doubleClick && m.clickX == x && m.clickY == y
	m.clickX, m.clickY, m.clickAt = x, y, time.Now()

	// The tab indicator sits on the last line, on any tab.
	if t, ok := m.tabHit(x, y); ok {
		m.tab, m.detail, m.focus = t, false, 0
		if t == 1 {
			m.setKeyring = keyringStatus(m.sources())
		}
		return m, nil
	}
	if m.tab == 1 {
		return m, nil // Settings: keyboard-driven for now
	}

	panelsH := max(3, m.h-3)
	if y < 2 || y > panelsH-1 { // outside the panel content rows
		return m, nil
	}
	leftW := min(42, max(18, m.w*2/5))
	leftSide := x <= leftW-1

	// In the entry detail, clicking the left tree leaves the entry and jumps there.
	if m.detail {
		if leftSide {
			flat := m.visible()
			if row := windowStart(m.tsel, panelsH-2, len(flat)) + (y - 2); row >= 0 && row < len(flat) {
				m.tsel, m.esel, m.focus, m.detail = row, 0, 0, false
				if m.showResults() { // came in from a search — drop it so the tree is live
					m.input.SetValue("")
					m.results, m.sel, m.searchMode = nil, 0, false
				}
			}
		}
		return m, nil
	}
	if m.searchMode {
		return m, nil // typing in the search box
	}

	// Left panel — the folder tree (only the base surface, not during search).
	if leftSide && !m.showResults() {
		flat := m.visible()
		if row := windowStart(m.tsel, panelsH-2, len(flat)) + (y - 2); row >= 0 && row < len(flat) {
			if dbl && m.tsel == row {
				flat[row].node.expanded = !flat[row].node.expanded // double-click opens the folder
			} else {
				m.tsel, m.esel, m.focus = row, 0, 0
			}
		}
		return m, nil
	}

	// Right panel — results or the folder's entry table (a pinned header on row 2).
	if x >= leftW+1 {
		if y < 3 {
			m.focus = 1 // clicking the header just focuses the pane
			return m, nil
		}
		off := y - 3
		if m.showResults() {
			if idx := windowStart(m.sel, panelsH-3, len(m.results)) + off; idx >= 0 && idx < len(m.results) {
				if dbl && idx == m.sel {
					return m.openDetail()
				}
				m.sel = idx
			}
			return m, nil
		}
		if f := m.currentFolder(); f != nil {
			if idx := windowStart(m.esel, panelsH-3, len(f.entries)) + off; idx >= 0 && idx < len(f.entries) {
				if dbl && idx == m.esel && m.focus == 1 {
					return m.openDetail()
				}
				m.esel, m.focus = idx, 1
			}
		}
	}
	return m, nil
}

// handleRightClick copies the password of the entry (or result) under the cursor
// — a quick copy from the list without opening the detail.
func (m Model) handleRightClick(x, y int) (tea.Model, tea.Cmd) {
	if m.locked || m.help || m.attach != attachNone || m.tab == 1 || m.searchMode {
		return m, nil
	}
	if m.detail { // in the detail, a right-click anywhere copies the open entry
		return m, m.copySel("password")
	}
	panelsH := max(3, m.h-3)
	leftW := min(42, max(18, m.w*2/5))
	if y < 3 || y > panelsH-1 || x < leftW+1 { // right-pane list rows only (header at 2)
		return m, nil
	}
	off := y - 3
	if m.showResults() {
		if idx := windowStart(m.sel, panelsH-3, len(m.results)) + off; idx >= 0 && idx < len(m.results) {
			m.sel = idx
			return m, m.copySel("password")
		}
		return m, nil
	}
	if f := m.currentFolder(); f != nil {
		if idx := windowStart(m.esel, panelsH-3, len(f.entries)) + off; idx >= 0 && idx < len(f.entries) {
			m.esel, m.focus = idx, 1
			return m, m.copySel("password")
		}
	}
	return m, nil
}

// tabHit maps a click on the bottom-line tab indicator to a tab index.
func (m Model) tabHit(x, y int) (int, bool) {
	if y != m.h-1 {
		return 0, false
	}
	labels := []string{"1 Vault", "2 Settings"}
	total := 0
	for i, l := range labels {
		total += dw(l)
		if i < len(labels)-1 {
			total += dw(" · ")
		}
	}
	off := m.w - total
	for i, l := range labels {
		if x >= off && x < off+dw(l) {
			return i, true
		}
		off += dw(l) + dw(" · ")
	}
	return 0, false
}
