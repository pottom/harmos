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
	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/otp"
	"github.com/pottom/harmos/internal/pwgen"
	"github.com/pottom/harmos/internal/search"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/session"
	"github.com/pottom/harmos/internal/vault"
)

type tickMsg time.Time
type clearedMsg struct{}

// totpTickMsg drives the once-a-second refresh of a live TOTP code in the detail
// view; the int is a generation token so only the latest open keeps ticking.
type totpTickMsg int

// Model is the TUI state machine (see docs/design/harmos-tui-interaction.md).
type Model struct {
	matcher *search.Matcher
	roots   []*node // folder tree, one root per source
	nSrc    int
	input   textinput.Model
	results []search.Result

	tab        int  // 0 = Vault, 1 = Settings
	tsel       int  // selected folder (index into the flattened visible tree)
	focus      int  // 0 = tree (left), 1 = entry table (right)
	esel       int  // selected entry within the folder's table
	sel        int  // selected search result (while searching)
	searchMode bool // the "/" search box is capturing keystrokes
	detail     bool // entry-details screen
	reveal     bool
	help       bool
	helpScroll int // top line offset in the (scrollable) help overlay
	w, h       int

	configPath string                 // for the Settings tab
	srcType    map[string]config.Type // source name → type, for tree/breadcrumb icons
	setSel     int                    // selected row in the Settings sources table
	setKeyring map[string]bool        // profile name → has a saved keyring password
	setMode    int                    // setList / setRemove / …
	setCat     int                    // selected Settings category (left pane): catSources / catTheme
	setStatus  string                 // last action result, shown in the Settings footer
	rmToggle   int                    // remove overlay: 0 delete-file, 1 forget-pw, 2 confirm
	rmFile     bool                   // remove overlay: also delete the local file
	rmPw       bool                   // remove overlay: also forget the keyring password

	form        []formField // add/edit form fields
	formFocus   int         // focused form row (len(form) = the Save button)
	formEditing bool        // editing an existing source vs adding
	formOrig    string      // the profile name being edited (for rename)
	formPps     bool        // form type: Pleasant vs kdbx

	promptInput textinput.Model // save-password prompt
	promptQueue []promptStep    // remaining password prompts
	promptName  string          // profile the prompt(s) are for

	syncCh    chan syncProgressMsg // live sync progress
	syncName  string               // source being synced
	syncPhase string               // current phase
	syncDone  int64                // bytes downloaded
	syncTotal int64                // total bytes (-1 unknown)

	themeName string // active theme name
	themeSel  int    // theme picker: selected index into theme.Names()
	themeOrig string // theme picker: name to revert to on cancel

	// Unlock phase (locked == true until every source is opened).
	locked   bool
	cfg      *config.Config  // the config being unlocked (nil once browsing)
	ulInput  textinput.Model // masked credential input
	ulSteps  []ulStep        // remaining credentials to collect
	ulIdx    int             // current step
	ulMaster secret.Secret   // the shared master, once resolved
	ulHasM   bool            // master resolved (env/keyring/typed)
	ulStats  []srcStat       // per-source status rows for the unlock table
	ulErr    string          // inline error (wrong password)
	ulBusy   bool            // an open attempt (Argon2) is running
	ulSkip   map[string]bool // sources the user chose to skip (esc)

	excluded []session.Excluded // sources that could not be opened, shown while browsing

	timeout    time.Duration
	copied     string
	copiedWhat string
	remaining  int

	totpGen      int    // generation token for the live-TOTP refresh loop in detail view
	detailScroll int    // top line offset when the detail pane overflows
	flash        string // transient message (e.g. "saved attachment"), shown in the bottom line

	attach      int             // attachment-save modal: attachNone / attachPick / attachDest
	attachSel   int             // cursor position in the picker
	attachCheck []bool          // per-attachment selection (space toggles)
	attachInput textinput.Model // destination-directory input

	clickX, clickY int       // last left-click cell, for double-click detection
	clickAt        time.Time //

	// Generate tab (2): a pwgen-style password generator.
	genOpts pwgen.Options // character classes + length
	genList []string      // this session's recent rolls (newest last)
	genSel  int           // the hero password (index into genList)
	genRow  int           // selected option row (left pane)
	genErr  string        // last generation error (e.g. no classes enabled)
}

// doubleClick is the window within which a second click on the same cell counts
// as a double-click.
const doubleClick = 450 * time.Millisecond

// New builds the model over the given entries. configPath is the config file the
// Settings tab reads and edits (may be "" when there is no Settings work).
func New(entries []vault.Entry, configPath string, timeout time.Duration) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "press / to search every source…"
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	roots := buildTree(entries)
	themeName := "charm"
	srcType := map[string]config.Type{}
	var loaded *config.Config
	if configPath != "" {
		if cfg, err := config.Load(configPath); err == nil {
			loaded = cfg
			if cfg.Theme != "" {
				themeName = cfg.Theme
			}
			for _, p := range cfg.Profiles {
				srcType[p.Name] = p.Type
			}
		}
	}
	nerd = resolveNerd(loaded) // env > config > default
	return Model{
		matcher:    search.New(entries),
		roots:      roots,
		nSrc:       len(roots),
		tsel:       firstFolderWithEntries(roots),
		input:      ti,
		configPath: configPath,
		srcType:    srcType,
		themeName:  themeName,
		timeout:    timeout,
		genOpts:    pwgen.Default(),
	}
}

// firstFolderWithEntries is the visible-tree index of the first folder that
// actually holds entries, so launch doesn't land on an empty source root.
func firstFolderWithEntries(roots []*node) int {
	for i, tl := range visibleTree(roots) {
		if len(tl.node.entries) > 0 {
			return i
		}
	}
	return 0
}

// Run launches the TUI in the alt screen.
func Run(entries []vault.Entry, configPath string, timeout time.Duration) error {
	_, err := tea.NewProgram(New(entries, configPath, timeout), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	// A locked model with nothing left to ask (every credential came from env or
	// the keyring) opens straight away — no unlock screen flash for saved users.
	if m.locked && len(m.ulSteps) == 0 {
		return tea.Batch(textinput.Blink, m.openCmd())
	}
	return textinput.Blink
}

// intoBrowsing rebuilds the browsing surface from freshly opened entries and
// leaves the unlock phase.
func (m Model) intoBrowsing(entries []vault.Entry) Model {
	m.matcher = search.New(entries)
	m.roots = buildTree(entries)
	m.nSrc = len(m.roots)
	m.tsel = firstFolderWithEntries(m.roots)
	m.locked = false
	m.cfg = nil
	m.ulSteps = nil
	m.ulBusy = false
	return m
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func totpTick(gen int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return totpTickMsg(gen) })
}

// openDetail enters the entry-detail split. If the entry carries a TOTP it starts
// a fresh once-a-second refresh loop so the code and its countdown stay live.
func (m Model) openDetail() (tea.Model, tea.Cmd) {
	m.detail, m.reveal, m.detailScroll = true, false, 0
	if e := m.selEntry(); e != nil && e.TOTP != "" {
		m.totpGen++
		return m, totpTick(m.totpGen)
	}
	return m, nil
}

// clearClip clears the concealed clipboard unless the user copied something else
// since — safe on the countdown expiry and on quit (spec §9).
func clearClip() tea.Msg {
	_ = clip.ClearIfUnchanged()
	return clearedMsg{}
}

// sources reads the configured profiles fresh from disk (so the Settings tab
// reflects edits); an unreadable or empty config yields no rows.
func (m Model) sources() []config.Profile {
	if m.configPath == "" {
		return nil
	}
	cfg, err := config.Load(m.configPath)
	if err != nil {
		return nil
	}
	return cfg.Profiles
}

// keyringStatus probes the OS keyring for each profile's saved password (the
// server password for Pleasant, the per-file password for kdbx). Computed once
// on entering Settings, not per render, to avoid repeated keychain access.
func keyringStatus(profs []config.Profile) map[string]bool {
	st := make(map[string]bool, len(profs))
	for _, p := range profs {
		var ok bool
		if p.Type == config.Pleasant {
			_, ok, _ = keyring.FetchServer(p.Name)
		} else {
			_, ok, _ = keyring.Fetch(p.Name)
		}
		st[p.Name] = ok
	}
	return st
}

func (m Model) searching() bool { return m.input.Value() != "" }

// showResults is true whenever the right pane is the ranked results list — only
// when there's an actual query. An empty search box (just opened, or deleted
// back to empty) shows the tree, not every entry.
func (m Model) showResults() bool { return m.input.Value() != "" }

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
	m.sel = 0
	if m.input.Value() == "" { // no query → no results list (the tree shows instead)
		m.results = nil
		return
	}
	m.results = m.matcher.Match(m.input.Value())
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
	case "totp":
		k, err := otp.Parse(e.TOTP)
		if err != nil {
			return nil
		}
		val, what = k.Code(time.Now()), "TOTP"
	default:
		val, what = e.Password.Reveal(), "password"
	}
	return m.copyString(val, what, e.Source+" · "+e.Path)
}

// copyString copies val to the concealed clipboard and starts (or refreshes) the
// clear countdown. what labels it in the countdown line, provenance says where it
// came from. Shared by the vault copy and the generator.
func (m *Model) copyString(val, what, provenance string) tea.Cmd {
	if val == "" {
		return nil
	}
	if err := clip.Copy([]byte(val)); err != nil {
		return nil
	}
	m.copied = provenance
	m.copiedWhat = what
	// A countdown already running means a tick loop is live: just refresh the
	// timer and let that single loop carry it. Starting another tick() here would
	// stack overlapping loops, and the countdown would run 2×, 3×… too fast.
	running := m.remaining > 0
	if m.remaining = int(m.timeout.Seconds()); m.remaining < 1 {
		m.remaining = 1
	}
	if running {
		return nil
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

	case totpTickMsg:
		// keep re-rendering while the latest detail view shows a TOTP entry
		if int(msg) == m.totpGen && m.detail {
			if e := m.selEntry(); e != nil && e.TOTP != "" {
				return m, totpTick(m.totpGen)
			}
		}
		return m, nil

	case unlockDoneMsg:
		return m.onUnlockDone(msg)

	case syncProgressMsg:
		m.syncPhase = msg.phase
		if msg.done > 0 || msg.total > 0 {
			m.syncDone, m.syncTotal = msg.done, msg.total
		}
		return m, listenSync(m.syncCh)

	case syncDoneMsg:
		if msg.err != nil {
			m.setStatus = "sync failed: " + msg.err.Error()
		} else {
			m.setStatus = msg.summary
		}
		m.setMode = setList
		m.syncCh = nil
		m.setKeyring = keyringStatus(m.sources())
		return m, nil

	case tea.MouseMsg:
		// The wheel scrolls the focused surface — the same as pressing up/down a
		// few times, so it moves the cursor in lists and scrolls the detail pane.
		switch msg.Button {
		case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
			k := tea.KeyMsg{Type: tea.KeyDown}
			if msg.Button == tea.MouseButtonWheelUp {
				k = tea.KeyMsg{Type: tea.KeyUp}
			}
			var cmd tea.Cmd
			for range 3 {
				var nm tea.Model
				nm, cmd = m.Update(k)
				m = nm.(Model)
			}
			return m, cmd
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				return m.handleClick(msg.X, msg.Y)
			}
		case tea.MouseButtonRight:
			if msg.Action == tea.MouseActionPress {
				return m.handleRightClick(msg.X, msg.Y)
			}
		}
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Sequence(clearClip, tea.Quit)
		}
		// The unlock phase owns every key until every source is open.
		if m.locked {
			return m.updateUnlock(key, msg)
		}
		// The attachment-save modal owns every key while it is open.
		if m.attach != attachNone {
			return m.updateAttach(key, msg)
		}
		if key == "?" && !m.searchMode {
			m.help = !m.help
			m.helpScroll = 0
			return m, nil
		}
		if m.help {
			return m.updateHelp(key), nil
		}
		// q quits everywhere except while typing a search (where it's a character).
		if key == "q" && !m.searchMode {
			return m, tea.Sequence(clearClip, tea.Quit)
		}

		// Tab switching (1 = Vault, 2 = Settings, 3 = Generate) — not while typing a
		// search, and not while a Settings overlay (form/remove) is capturing keys.
		inOverlay := m.searchMode || (m.tab == 1 && m.setMode != setList)
		if !inOverlay {
			switch key {
			case "1":
				m.tab, m.detail, m.focus = 0, false, 0
				return m, nil
			case "2": // 2 = Generate (displayed second; internal tab index 2)
				m.tab, m.detail, m.focus = 2, false, 0
				if len(m.genList) == 0 {
					m.resetGen() // first visit: roll a password so the pane isn't empty
				}
				return m, nil
			case "3": // 3 = Settings (displayed last; internal tab index 1)
				m.tab, m.detail, m.focus = 1, false, 0
				m.setKeyring = keyringStatus(m.sources())
				return m, nil
			}
		}

		// The Settings tab handles its own keys.
		if m.tab == 1 {
			return m.updateSettings(key, msg)
		}
		// The Generate tab handles its own keys.
		if m.tab == 2 {
			return m.updateGenerate(key)
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
			case "pgup":
				_, step := m.panelRows()
				m.sel = clampIndex(m.sel-max(1, step), len(m.results))
				return m, nil
			case "pgdown":
				_, step := m.panelRows()
				m.sel = clampIndex(m.sel+max(1, step), len(m.results))
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.refilter()
			return m, cmd
		}

		// ENTRY DETAILS — keys are commands.
		if m.detail {
			m.flash = "" // any key dismisses a transient message
			switch key {
			case "esc", "left":
				m.detail, m.reveal = false, false
			case "up", "ctrl+p":
				if m.detailScroll > 0 {
					m.detailScroll--
				}
			case "down", "ctrl+n":
				if e := m.selEntry(); e != nil {
					w, vis := m.detailViewport()
					if len(m.detailLines(e, w))-vis > m.detailScroll {
						m.detailScroll++
					}
				}
			case "pgup":
				_, vis := m.detailViewport()
				m.detailScroll = max(0, m.detailScroll-(vis-1))
			case "pgdown":
				if e := m.selEntry(); e != nil {
					w, vis := m.detailViewport()
					m.detailScroll = clampScroll(m.detailScroll+(vis-1), len(m.detailLines(e, w)), vis)
				}
			case "ctrl+r":
				m.reveal = !m.reveal
			case "s":
				if e := m.selEntry(); e != nil && len(e.Files) > 0 {
					return m.openAttachments(), nil
				}
			case "ctrl+u":
				return m, m.copySel("username")
			case "ctrl+o":
				return m, m.copySel("URL")
			case "ctrl+t":
				return m, m.copySel("totp")
			case "c":
				return m.copyGetCommand()
			case "enter", "ctrl+y":
				return m, m.copySel("password")
			}
			return m, nil
		}

		// RESULTS BROWSE — a filter is applied; arrows walk the ranked list.
		if m.showResults() {
			m.flash = "" // any key dismisses a transient message
			switch key {
			case "/":
				m.searchMode = true
				return m, m.input.Focus()
			case "c":
				return m.copyGetCommand()
			case "up", "ctrl+p":
				if m.sel > 0 {
					m.sel--
				}
			case "down", "ctrl+n":
				if m.sel < len(m.results)-1 {
					m.sel++
				}
			case "pgup":
				_, step := m.panelRows()
				m.sel = clampIndex(m.sel-max(1, step), len(m.results))
			case "pgdown":
				_, step := m.panelRows()
				m.sel = clampIndex(m.sel+max(1, step), len(m.results))
			case "enter":
				if m.sel < len(m.results) {
					return m, m.copySel("password") // ↵ copies straight from the results
				}
			case "right", "tab":
				if m.sel < len(m.results) {
					return m.openDetail() // → opens the entry details
				}
			case "g":
				return m.gotoResultFolder(), nil
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
			case "ctrl+t":
				return m, m.copySel("totp")
			}
			return m, nil
		}

		// TREE BROWSE — the base surface. Letters are free for future hotkeys.
		m.flash = "" // any key dismisses a transient message
		folder := m.currentFolder()
		switch key {
		case "/":
			m.sel = 0
			m.searchMode = true
			m.refilter()
			return m, m.input.Focus()
		case "c":
			if m.focus == 1 { // an entry is selected in the table
				return m.copyGetCommand()
			}
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
		case "pgup":
			tree, list := m.panelRows()
			if m.focus == 0 {
				m.tsel, m.esel = clampIndex(m.tsel-max(1, tree), len(m.visible())), 0
			} else if folder != nil {
				m.esel = clampIndex(m.esel-max(1, list), len(folder.entries))
			}
		case "pgdown":
			tree, list := m.panelRows()
			if m.focus == 0 {
				m.tsel, m.esel = clampIndex(m.tsel+max(1, tree), len(m.visible())), 0
			} else if folder != nil {
				m.esel = clampIndex(m.esel+max(1, list), len(folder.entries))
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
			} else if m.focus == 1 && folder != nil && m.esel < len(folder.entries) {
				return m.openDetail() // → opens the entry details
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
				return m, m.copySel("password") // ↵ copies straight from the list
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
		case "ctrl+t":
			return m, m.copySel("totp")
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.locked {
		m.ulInput, cmd = m.ulInput.Update(msg)
		return m, cmd
	}
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
