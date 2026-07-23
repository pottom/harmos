// Package tui is the Bubble Tea interface. One surface, no modes: the left pane
// is the source→folder tree (the base, mirroring the Pleasant Password Server
// web UI), the right pane is the selected folder's entry table. Typing anything
// quick-searches every source and the right pane becomes ranked results; esc
// clears back to the tree. Enter opens an entry's details. Brass/charm themes,
// amber only for secrets. It reads the shared matcher and copies through the
// concealed clipboard (spec §9).
package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/clip"
	"github.com/pottom/harmos/internal/search"
	"github.com/pottom/harmos/internal/vault"
)

type tickMsg time.Time
type clearedMsg struct{}

// Model is the TUI state machine (see docs/design/harmos-tui-interaction.md).
type Model struct {
	matcher *search.Matcher
	roots   []*node // folder tree, one root per source
	nSrc    int
	input   textinput.Model
	results []search.Result

	tsel       int  // selected folder (index into the flattened visible tree)
	focus      int  // 0 = tree (left), 1 = entry table (right)
	esel       int  // selected entry within the folder's table
	sel        int  // selected search result (while searching)
	searchMode bool // the "/" search box is capturing keystrokes
	detail     bool // entry-details screen
	reveal     bool
	help       bool
	w, h       int

	timeout    time.Duration
	copied     string
	copiedWhat string
	remaining  int
}

// New builds the model over the given entries.
func New(entries []vault.Entry, timeout time.Duration) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "press / to search every source…"
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	roots := buildTree(entries)
	return Model{
		matcher: search.New(entries),
		roots:   roots,
		nSrc:    len(roots),
		input:   ti,
		timeout: timeout,
	}
}

// Run launches the TUI in the alt screen.
func Run(entries []vault.Entry, timeout time.Duration) error {
	_, err := tea.NewProgram(New(entries, timeout), tea.WithAltScreen()).Run()
	return err
}

func (m Model) Init() tea.Cmd { return textinput.Blink }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// clearClip clears the concealed clipboard unless the user copied something else
// since — safe on the countdown expiry and on quit (spec §9).
func clearClip() tea.Msg {
	_ = clip.ClearIfUnchanged()
	return clearedMsg{}
}

func (m Model) searching() bool { return m.input.Value() != "" }

// showResults is true whenever the right pane is the ranked results list: while
// actively typing in the search box, or afterwards while a filter is applied.
func (m Model) showResults() bool { return m.searchMode || m.input.Value() != "" }

func (m Model) visible() []treeLine { return visibleTree(m.roots) }

func (m Model) currentFolder() *node {
	flat := m.visible()
	if m.tsel >= 0 && m.tsel < len(flat) {
		return flat[m.tsel].node
	}
	return nil
}

// selEntry is the entry in focus: a search result while searching, otherwise the
// selected row of the open folder's table.
func (m Model) selEntry() *vault.Entry {
	if m.showResults() {
		if m.sel >= 0 && m.sel < len(m.results) {
			return &m.results[m.sel].Entry
		}
		return nil
	}
	f := m.currentFolder()
	if f != nil && m.esel >= 0 && m.esel < len(f.entries) {
		return &f.entries[m.esel]
	}
	return nil
}

func (m *Model) refilter() {
	m.results = m.matcher.Match(m.input.Value())
	m.sel = 0
}

func (m *Model) copySel(what string) tea.Cmd {
	e := m.selEntry()
	if e == nil {
		return nil
	}
	var val string
	switch what {
	case "username":
		val = e.Username
	case "URL":
		val = e.URL
	default:
		val, what = e.Password.Reveal(), "password"
	}
	if val == "" {
		return nil
	}
	if err := clip.Copy([]byte(val)); err != nil {
		return nil
	}
	m.copied = e.Source + " · " + e.Path
	m.copiedWhat = what
	if m.remaining = int(m.timeout.Seconds()); m.remaining < 1 {
		m.remaining = 1
	}
	return tick()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if m.remaining > 0 {
			m.remaining--
			if m.remaining > 0 {
				return m, tick()
			}
			m.copied = ""
			return m, clearClip
		}
		return m, nil

	case clearedMsg:
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Sequence(clearClip, tea.Quit)
		}
		if key == "?" && !m.searchMode {
			m.help = !m.help
			return m, nil
		}
		if m.help {
			m.help = false
			return m, nil
		}

		// SEARCH MODE — the "/" box is capturing keystrokes.
		if m.searchMode {
			switch key {
			case "enter": // apply the filter, leave the box (results stay)
				m.searchMode = false
				m.input.Blur()
				return m, nil
			case "esc": // leave the box and clear the filter
				m.searchMode = false
				m.input.Blur()
				m.input.SetValue("")
				m.results = nil
				m.sel = 0
				return m, nil
			case "up", "ctrl+p":
				if m.sel > 0 {
					m.sel--
				}
				return m, nil
			case "down", "ctrl+n":
				if m.sel < len(m.results)-1 {
					m.sel++
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.refilter()
			return m, cmd
		}

		// ENTRY DETAILS — keys are commands.
		if m.detail {
			switch key {
			case "esc", "left":
				m.detail, m.reveal = false, false
			case "ctrl+r":
				m.reveal = !m.reveal
			case "ctrl+u":
				return m, m.copySel("username")
			case "ctrl+o":
				return m, m.copySel("URL")
			case "enter", "ctrl+y":
				return m, m.copySel("password")
			}
			return m, nil
		}

		// RESULTS BROWSE — a filter is applied; arrows walk the ranked list.
		if m.showResults() {
			switch key {
			case "/":
				m.searchMode = true
				return m, m.input.Focus()
			case "up", "ctrl+p":
				if m.sel > 0 {
					m.sel--
				}
			case "down", "ctrl+n":
				if m.sel < len(m.results)-1 {
					m.sel++
				}
			case "enter":
				if m.sel < len(m.results) {
					m.detail, m.reveal = true, false
				}
			case "esc":
				m.input.SetValue("")
				m.results = nil
				m.sel = 0
			case "ctrl+y":
				return m, m.copySel("password")
			case "ctrl+u":
				return m, m.copySel("username")
			case "ctrl+o":
				return m, m.copySel("URL")
			}
			return m, nil
		}

		// TREE BROWSE — the base surface. Letters are free for future hotkeys.
		folder := m.currentFolder()
		switch key {
		case "/":
			m.sel = 0
			m.searchMode = true
			m.refilter()
			return m, m.input.Focus()
		case "up", "ctrl+p":
			if m.focus == 0 {
				if m.tsel > 0 {
					m.tsel, m.esel = m.tsel-1, 0
				}
			} else if m.esel > 0 {
				m.esel--
			}
		case "down", "ctrl+n":
			if m.focus == 0 {
				if m.tsel < len(m.visible())-1 {
					m.tsel, m.esel = m.tsel+1, 0
				}
			} else if folder != nil && m.esel < len(folder.entries)-1 {
				m.esel++
			}
		case "tab":
			if m.focus == 0 && folder != nil && len(folder.entries) > 0 {
				m.focus, m.esel = 1, 0
			} else {
				m.focus = 0
			}
		case "right":
			if m.focus == 0 && folder != nil {
				if len(folder.children) > 0 && !folder.expanded {
					folder.expanded = true
				} else if len(folder.entries) > 0 {
					m.focus, m.esel = 1, 0
				}
			}
		case "left":
			if m.focus == 1 {
				m.focus = 0
			} else if folder != nil && folder.expanded {
				folder.expanded = false
			}
		case "enter":
			if m.focus == 0 {
				if folder != nil {
					folder.expanded = !folder.expanded
				}
			} else if folder != nil && m.esel < len(folder.entries) {
				m.detail, m.reveal = true, false
			}
		case "esc":
			if m.focus == 1 {
				m.focus = 0
			}
		case "ctrl+y":
			return m, m.copySel("password")
		case "ctrl+u":
			return m, m.copySel("username")
		case "ctrl+o":
			return m, m.copySel("URL")
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
