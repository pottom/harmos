package tui

import (
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/session"
	"github.com/pottom/harmos/internal/theme"
)

// ulStep is one credential the unlock screen still needs to collect: the shared
// master (name == ""), or a single kdbx source's own password.
type ulStep struct {
	name  string // profile name, "" for the shared master
	label string // prompt shown while this step is active
	got   secret.Secret
	done  bool
}

// srcStat is a source's row on the unlock screen: identity plus a credential /
// freshness badge.
type srcStat struct {
	name, typ, loc string
	badge          string
	kind           badgeKind
}

type badgeKind int

const (
	badgeFresh badgeKind = iota // green — cache fresh / keyring / env
	badgeStale                  // rust — cache old / missing / needs attention
	badgeAsk                    // faint — a password will be asked
)

// staleAfter marks a Pleasant cache "stale" once it is older than this.
const staleAfter = 24 * time.Hour

// unlockDoneMsg carries the outcome of one open attempt.
type unlockDoneMsg struct{ res *session.Result }

// NewLocked builds a model that starts in the unlock phase: it lists the sources,
// resolves whatever credentials it can from HARMOS_MASTER and the keyring, and
// asks (masked) only for what is left before opening every source.
func NewLocked(cfg *config.Config, configPath string, timeout time.Duration) Model {
	m := New(nil, configPath, timeout)
	m.locked = true
	m.cfg = cfg
	m.ulSkip = map[string]bool{}

	pi := textinput.New()
	pi.Prompt = ""
	pi.EchoMode = textinput.EchoPassword
	pi.EchoCharacter = '•'
	pi.Focus()
	m.ulInput = pi

	m.ulStats = sourceStats(cfg)

	// The master unlocks every Pleasant cache; resolve it once from env/keyring.
	if hasPleasant(cfg) {
		if pw, ok := resolveMasterForTUI(); ok {
			m.ulMaster, m.ulHasM = pw, true
		} else {
			m.ulSteps = append(m.ulSteps, ulStep{label: "master password"})
		}
	}
	// Each kdbx source needs its own password unless the keyring holds it.
	for _, p := range cfg.Profiles {
		if p.Type != config.Kdbx {
			continue
		}
		if _, ok, _ := keyring.Fetch(p.Name); ok {
			continue
		}
		m.ulSteps = append(m.ulSteps, ulStep{name: p.Name, label: "password for " + p.Name})
	}
	return m
}

// RunLocked launches the TUI in the unlock phase.
func RunLocked(cfg *config.Config, configPath string, timeout time.Duration) error {
	_, err := tea.NewProgram(NewLocked(cfg, configPath, timeout), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func hasPleasant(cfg *config.Config) bool {
	for _, p := range cfg.Profiles {
		if p.Type == config.Pleasant {
			return true
		}
	}
	return false
}

// sourceStats builds the per-source status rows: Pleasant sources show cache
// freshness, kdbx sources show whether a saved password covers them.
func sourceStats(cfg *config.Config) []srcStat {
	i := ic()
	stats := make([]srcStat, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		s := srcStat{name: p.Name}
		if p.Type == config.Pleasant {
			s.typ, s.loc = "pleasant", p.URL
			if age, ok := fileAge(p.Cache); !ok {
				s.badge, s.kind = "no cache", badgeStale
			} else if age > staleAfter {
				s.badge, s.kind = "cache "+humanAge(age)+" — stale", badgeStale
			} else {
				s.badge, s.kind = "cache "+humanAge(age), badgeFresh
			}
		} else {
			s.typ, s.loc = "kdbx", p.Path
			if _, ok, _ := keyring.Fetch(p.Name); ok {
				s.badge, s.kind = "keyring", badgeFresh
			} else if p.Keyfile != "" {
				s.badge, s.kind = "keyfile "+i.none, badgeAsk
			} else {
				s.badge, s.kind = "own password", badgeAsk
			}
		}
		stats = append(stats, s)
	}
	return stats
}

func fileAge(path string) (time.Duration, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return time.Since(fi.ModTime()), true
}

// humanAge is a compact "just now" / "12m" / "2h" / "4d" age.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

// updateUnlock drives the unlock screen: type/edit the current credential, enter
// advances (and opens after the last), esc skips an optional kdbx source.
func (m Model) updateUnlock(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.ulBusy {
		return m, nil // keys ignored while Argon2 runs
	}
	switch key {
	case "enter":
		return m.submitStep(false)
	case "esc":
		// Skip an optional source (not the master): open without it.
		if m.curStepName() != "" {
			return m.submitStep(true)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.ulInput, cmd = m.ulInput.Update(msg)
	return m, cmd
}

func (m Model) curStepName() string {
	if m.ulIdx < len(m.ulSteps) {
		return m.ulSteps[m.ulIdx].name
	}
	return ""
}

// submitStep records the typed credential (or skips it) and advances; once every
// step is answered it fires the open. skip drops the current optional source.
func (m Model) submitStep(skip bool) (tea.Model, tea.Cmd) {
	m.ulErr = ""
	if m.ulIdx < len(m.ulSteps) {
		st := &m.ulSteps[m.ulIdx]
		if !skip {
			pw := secret.New(m.ulInput.Value())
			st.got, st.done = pw, true
			if st.name == "" {
				m.ulMaster, m.ulHasM = pw, true
			}
		} else {
			st.done = false // left unanswered → source will be excluded
			if st.name != "" {
				m.ulSkip[st.name] = true
			}
		}
		m.ulIdx++
		m.ulInput.SetValue("")
	}
	if m.ulIdx < len(m.ulSteps) {
		return m, nil
	}
	// All collected — open in the background.
	m.ulBusy = true
	return m, m.openCmd()
}

// openCmd opens every source with the collected credentials, off the update loop
// (KDF is slow), and reports the result.
func (m Model) openCmd() tea.Cmd {
	cfg := m.cfg
	master := m.ulMaster
	typed := map[string]secret.Secret{}
	answered := map[string]bool{}
	for _, s := range m.ulSteps {
		if s.name != "" && s.done {
			typed[s.name] = s.got
			answered[s.name] = true
		}
	}
	ask := func(p config.Profile, _ bool) (secret.Secret, bool, error) {
		if p.Type == config.Pleasant {
			return master, false, nil // interactive=false: retry is handled here, not in session
		}
		if pw, ok := typed[p.Name]; ok {
			return pw, false, nil
		}
		if pw, ok, _ := keyring.Fetch(p.Name); ok {
			return pw, false, nil
		}
		return secret.Secret{}, false, nil // keyfile-only or skipped
	}
	return func() tea.Msg {
		return unlockDoneMsg{res: session.Open(cfg, ask)}
	}
}

// onUnlockDone transitions to browsing, or re-queues the credentials that were
// rejected so the user can try again in place.
func (m Model) onUnlockDone(msg unlockDoneMsg) (tea.Model, tea.Cmd) {
	m.ulBusy = false
	res := msg.res

	var retry []ulStep
	var masterBad bool
	var other []session.Excluded
	for _, ex := range res.Excluded {
		// A source the user deliberately skipped stays out — never re-prompted.
		if m.ulSkip[ex.Source] {
			other = append(other, session.Excluded{Source: ex.Source, Reason: "skipped"})
			continue
		}
		if !ex.BadCredential {
			other = append(other, ex)
			continue
		}
		if p, ok := profileByName(m.cfg, ex.Source); ok && p.Type == config.Pleasant {
			masterBad = true
			continue
		}
		retry = append(retry, ulStep{name: ex.Source, label: "password for " + ex.Source})
	}

	if masterBad || len(retry) > 0 {
		steps := retry
		if masterBad {
			m.ulHasM = false
			steps = append([]ulStep{{label: "master password"}}, steps...)
		}
		m.ulSteps, m.ulIdx = steps, 0
		m.ulInput.SetValue("")
		m.ulErr = unlockErrText(masterBad, retry)
		m.ulStats = sourceStats(m.cfg) // badges may have shifted
		return m, nil
	}

	if len(res.Entries) == 0 {
		m.ulErr = "no entries unlocked — check the sources, or run `harmos sync`"
		m.ulStats = sourceStats(m.cfg)
		return m, nil
	}

	m = m.intoBrowsing(res.Entries)
	m.excluded = other // surfaced in the search line + help while browsing
	return m, nil
}

func unlockErrText(masterBad bool, retry []ulStep) string {
	switch {
	case masterBad && len(retry) > 0:
		return "wrong master password, and some sources too — try again"
	case masterBad:
		return "wrong master password — try again"
	case len(retry) == 1:
		return "wrong password for " + retry[0].name + " — try again"
	default:
		return "wrong password for some sources — try again"
	}
}

func profileByName(cfg *config.Config, name string) (config.Profile, bool) {
	if cfg == nil {
		return config.Profile{}, false
	}
	for _, p := range cfg.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return config.Profile{}, false
}

// unlockView is the unlock screen: a centered, titled panel listing the sources
// with credential/freshness badges, then the one masked prompt.
func (m Model) unlockView() string {
	boxW := min(m.w-4, 68)
	if boxW < 40 {
		boxW = min(m.w-2, 40)
	}
	inW := boxW - 2

	i := ic()
	nameW, typW := 12, 9
	badgeW := 18 // "● cache Nd — stale"
	locW := max(8, inW-4-nameW-typW-badgeW-3)

	lines := []string{"  " + theme.Faded.Render("one master password opens every cache"), ""}
	for _, s := range m.ulStats {
		dot, st := "●", theme.Ok
		switch s.kind {
		case badgeStale:
			st = theme.Bad
		case badgeAsk:
			dot, st = "○", theme.Faded
		}
		icon := i.kdbx
		if s.typ == "pleasant" {
			icon = i.pps
		}
		row := "  " + theme.Acc.Render(icon) + " " + theme.Strong.Render(pad(trunc(s.name, nameW), nameW)) + " " +
			theme.Dimmed.Render(pad(s.typ, typW)) + " " +
			theme.Dimmed.Render(pad(trunc(s.loc, locW), locW)) + " " +
			st.Render(dot+" "+s.badge)
		lines = append(lines, row)
	}
	lines = append(lines, "")

	if m.ulBusy {
		lines = append(lines, "  "+theme.Acc.Render("unlocking…"))
	} else if m.ulIdx < len(m.ulSteps) {
		label := theme.Strong.Render(m.ulSteps[m.ulIdx].label)
		lines = append(lines, "  "+label+"  "+m.ulInput.View())
	}
	if m.ulErr != "" {
		lines = append(lines, "", "  "+theme.Bad.Render(m.ulErr))
	}

	boxH := len(lines) + 2
	panel := box("harmos", "unlock", lines, boxW, boxH, true)

	hint := theme.Faded.Render("↵ unlock · ctrl-c quit")
	if !m.ulBusy && m.curStepName() != "" {
		hint = theme.Faded.Render("↵ unlock · esc skip this source · ctrl-c quit")
	}
	block := lipgloss.JoinVertical(lipgloss.Center, panel, "", hint)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, block)
}
