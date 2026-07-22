// Package tui is the Bubble Tea interface — the approved M4 concept made real:
// one surface with a fast heart (Palette search) and depths (Console home on an
// empty query, Split peek on tab). Brass theme, amber only for secrets. It reads
// the shared matcher and copies through the concealed clipboard.
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

type srcInfo struct {
	name  string
	count int
}

// Model is the TUI state machine (see docs/design/harmos-tui-interaction.md).
type Model struct {
	matcher *search.Matcher
	sources []srcInfo
	input   textinput.Model
	results []search.Result
	sel     int
	w, h    int
	peek    bool
	reveal  bool
	help    bool

	timeout    time.Duration
	copied     string
	copiedWhat string
	remaining  int
}

// New builds the model over the given entries.
func New(entries []vault.Entry, timeout time.Duration) Model {
	seen := map[string]int{}
	var order []string
	for _, e := range entries {
		if _, ok := seen[e.Source]; !ok {
			order = append(order, e.Source)
		}
		seen[e.Source]++
	}
	srcs := make([]srcInfo, len(order))
	for i, n := range order {
		srcs[i] = srcInfo{n, seen[n]}
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "search everything…"
	ti.Focus()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return Model{matcher: search.New(entries), sources: srcs, input: ti, timeout: timeout}
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

func (m *Model) refilter() {
	m.results = m.matcher.Match(m.input.Value())
	if m.sel >= len(m.results) {
		m.sel = max(0, len(m.results)-1)
	}
}

func (m *Model) copyField(what string) tea.Cmd {
	if len(m.results) == 0 {
		return nil
	}
	e := m.results[m.sel].Entry
	var val string
	switch what {
	case "username":
		val = e.Username
	case "URL":
		val = e.URL
	default:
		val, what = e.Password.Reveal(), "password"
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
		if key == "?" {
			m.help = !m.help
			return m, nil
		}
		if m.help {
			m.help = false
			return m, nil
		}
		switch key {
		case "up", "ctrl+p":
			if m.sel > 0 {
				m.sel--
				m.reveal = false
			}
			return m, nil
		case "down", "ctrl+n":
			if m.sel < len(m.results)-1 {
				m.sel++
				m.reveal = false
			}
			return m, nil
		case "enter":
			return m, m.copyField("password")
		case "ctrl+u":
			return m, m.copyField("username")
		case "ctrl+o":
			return m, m.copyField("URL")
		case "ctrl+r":
			if m.peek {
				m.reveal = !m.reveal
			}
			return m, nil
		case "tab", "right":
			if len(m.results) > 0 {
				m.peek = !m.peek
				m.reveal = false
			}
			return m, nil
		case "esc":
			if m.peek {
				m.peek = false
				m.reveal = false
			} else if m.input.Value() != "" {
				m.input.SetValue("")
				m.refilter()
			}
			return m, nil
		}
		// while peeking, letters are commands, not search input
		if m.peek {
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.reveal = false
	m.refilter()
	return m, cmd
}
