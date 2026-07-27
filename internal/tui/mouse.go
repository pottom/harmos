package tui

import tea "github.com/charmbracelet/bubbletea"

// handleClick routes a left-click at (x, y) to a tab switch or a list-row
// selection, matching the layout in vaultBody. Clicking the already-selected row
// opens it.
func (m Model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	// Overlays and modal phases own the whole screen; ignore stray clicks.
	if m.locked || m.help || m.attach != attachNone {
		return m, nil
	}
	// The tab indicator sits on the last line, on any tab.
	if t, ok := m.tabHit(x, y); ok {
		m.tab, m.detail, m.focus = t, false, 0
		if t == 1 {
			m.setKeyring = keyringStatus(m.sources())
		}
		return m, nil
	}
	if m.tab == 1 || m.detail || m.searchMode {
		return m, nil // Settings/detail/typing: keyboard-driven for now
	}

	panelsH := max(3, m.h-3)
	if y < 2 || y > panelsH-1 { // outside the panel content rows
		return m, nil
	}
	leftW := min(42, max(18, m.w*2/5))

	// Left panel — the folder tree (only the base surface, not during search).
	if x <= leftW-1 && !m.showResults() {
		flat := m.visible()
		if row := windowStart(m.tsel, panelsH-2, len(flat)) + (y - 2); row >= 0 && row < len(flat) {
			m.tsel, m.esel, m.focus = row, 0, 0
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
				if idx == m.sel {
					return m.openDetail()
				}
				m.sel = idx
			}
			return m, nil
		}
		if f := m.currentFolder(); f != nil {
			if idx := windowStart(m.esel, panelsH-3, len(f.entries)) + off; idx >= 0 && idx < len(f.entries) {
				if idx == m.esel && m.focus == 1 {
					return m.openDetail()
				}
				m.esel, m.focus = idx, 1
			}
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
