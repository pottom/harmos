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
	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/otp"
	"github.com/pottom/harmos/internal/pwgen"
	"github.com/pottom/harmos/internal/search"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/session"
	"github.com/pottom/harmos/internal/updater"
	"github.com/pottom/harmos/internal/vault"
	"github.com/pottom/harmos/internal/version"
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

	tab           int  // 0 = Vault, 1 = Settings
	tsel          int  // selected folder (index into the flattened visible tree)
	focus         int  // 0 = tree (left), 1 = entry table (right)
	esel          int  // selected entry within the folder's table
	sel           int  // selected search result (while searching)
	searchMode    bool // the "/" search box is capturing keystrokes
	detail        bool // entry-details screen
	reveal        bool
	help          bool
	helpScroll    int    // top line offset in the (scrollable) help overlay
	treeCollapsed bool   // the folder tree is collapsed to a thin rail (ctrl+b)
	latest        string // a newer release tag found by the background check ("" = none)
	w, h          int

	configPath string                 // for the Settings tab
	srcType    map[string]config.Type // source name → type, for tree/breadcrumb icons
	setSel     int                    // selected row in the Settings sources table
	setKeyring map[string]bool        // source name → has a saved keyring password
	setMode    int                    // setList / setRemove / …
	setCat     int                    // selected Settings category (left pane): catSources / catTheme
	setStatus  string                 // last action result, shown in the Settings footer
	onboarding bool                   // launched with no config — first-run add-a-source flow
	prefSel    int                    // selected row in the Preferences pane
	staleAfter time.Duration          // Pleasant cache stale threshold (config-backed)
	rmToggle   int                    // remove overlay: 0 delete-file, 1 forget-pw, 2 confirm
	rmFile     bool                   // remove overlay: also delete the local file
	rmPw       bool                   // remove overlay: also forget the keyring password

	form        form   // the add/edit form (see formkit.go)
	formEditing bool   // editing an existing source vs adding
	formOrig    string // the source name being edited (for rename)
	formPps     bool   // form type: Pleasant vs kdbx

	promptInput textinput.Model // save-password prompt
	promptQueue []promptStep    // remaining password prompts
	promptName  string          // source the prompt(s) are for

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

	// Writing. Every source starts locked on every run: writeOK is empty at
	// launch and is never persisted, so a vault is only ever editable because
	// the user said so during this session. Deliberately not called "locked" —
	// that already means the unlock phase.
	handles map[string]*vault.Handle // sources harmos could write, from the session
	writeOK map[string]bool          // sources the user has unlocked, this run only
	chg     edit.Set                 // staged changes, not yet written

	confirmUnlock string // source awaiting an unlock confirmation ("" = none)
	confirmSel    int    // which button is focused in a confirmation

	// The editor. edit is the surface that is open; everything else is what it
	// is working on. Nothing here writes — staging lands on m.chg.
	edit             editMode
	editSource       string      // which source the target lives in
	editTarget       string      // entry or folder ID
	editParent       string      // destination folder for a create or a move
	editNew          bool        // creating rather than changing
	editFolderTarget bool        // the target is a folder
	editBefore       *edit.Draft // the entry as it was, for the diff
	editForm         form
	moveDests        []vaultFolderRef
	moveSel          int

	// Inline rename: the row under the cursor turned into a field. It shares
	// editTarget and editFolderTarget with the rest, because it is renaming the
	// same things by the same identity.
	inlineInput  textinput.Model
	inlineBefore string // the name it started with, to spot "nothing changed"

	// The Changes tab and the save.
	chgSel        int             // selected row, counting only selectable ones
	chgFold       map[string]bool // folded hunks and folder groups (z)
	chgScroll     int             // first visible row of the Changes tab
	saveConfirm   bool            // the write confirmation is up
	saving        bool            // a save is running off the update loop
	saveConflict  string          // a source whose file moved under us ("" = none)
	quitGuard     bool            // quitting with staged changes
	quitAfterSave bool            // quit once the save lands

	// The full, merged view, so one source can be reloaded without losing the
	// others.
	mergedEntries []vault.Entry
	mergedFolders []vault.Folder

	// The same, projected through the staged set: the vault as it will be. Every
	// view-side lookup uses these — a change staged inside a folder that does
	// not exist on disk yet still has to resolve to a path.
	viewEntries []vault.Entry
	viewFolders []vault.Folder

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
func New(entries []vault.Entry, folders []vault.Folder, configPath string, timeout time.Duration) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "press / to search every source…"
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	roots := buildTree(entries, folders)
	themeName := "charm"
	srcType := map[string]config.Type{}
	var loaded *config.Config
	if configPath != "" {
		if cfg, err := config.Load(configPath); err == nil {
			loaded = cfg
			if cfg.Theme != "" {
				themeName = cfg.Theme
			}
			for _, p := range cfg.Sources {
				srcType[p.Name] = p.Type
			}
		}
	}
	nerd = resolveNerd(loaded) // env > config > default
	return Model{
		genOpts: genOptsFromConfig(loaded),
		matcher: search.New(entries),
		roots:   roots,
		// The merged view is the model's answer to "where does this ID live",
		// which the Changes tab asks about every row. It used to be filled only
		// by rebuild, so before the first reload every change reported itself as
		// living in the root.
		mergedEntries: entries,
		mergedFolders: folders,
		viewEntries:   entries,
		viewFolders:   folders,
		nSrc:          len(roots),
		tsel:          firstFolderWithEntries(roots),
		input:         ti,
		configPath:    configPath,
		srcType:       srcType,
		themeName:     themeName,
		timeout:       timeout,
		staleAfter:    resolveStaleAfter(loaded),
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
func Run(entries []vault.Entry, folders []vault.Folder, configPath string, timeout time.Duration) error {
	_, err := tea.NewProgram(New(entries, folders, configPath, timeout), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	// A locked model with nothing left to ask (every credential came from env or
	// the keyring) opens straight away — no unlock screen flash for saved users.
	if m.locked && len(m.ulSteps) == 0 {
		return tea.Batch(textinput.Blink, m.openCmd(), checkUpdateCmd())
	}
	return tea.Batch(textinput.Blink, checkUpdateCmd())
}

// updateAvailableMsg carries a newer release tag found by the background check.
type updateAvailableMsg struct{ tag string }

// checkUpdateCmd asks GitHub (once, in the background) whether a newer release
// exists and, if so, reports it. Any failure — offline, disabled, no releases —
// is silent: an update hint is a nicety, never a nag.
func checkUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		if updater.Disabled() {
			return nil
		}
		tag, err := updater.LatestTag()
		if err != nil || !updater.IsNewer(version.Version, tag) {
			return nil
		}
		return updateAvailableMsg{tag}
	}
}

// intoBrowsing rebuilds the browsing surface from freshly opened entries and
// leaves the unlock phase.
func (m Model) intoBrowsing(res *session.Result) Model {
	m.handles = res.Handles
	m.writeOK = writableFromConfig(m.sources(), res.Handles)
	m = m.rebuild(res.Entries, res.Folders)
	m.tsel = firstFolderWithEntries(m.roots)
	m.locked = false
	m.cfg = nil
	m.ulSteps = nil
	m.ulBusy = false
	return m
}

// rebuild re-derives the matcher and the tree from a fresh set of entries and
// folders, keeping the user roughly where they were.
//
// Selection is restored by ID rather than by index: a save can add, remove or
// move rows, so the row that was under the cursor is not the row at that index
// any more. Expanded folders are restored the same way, or reloading after a
// save would collapse the tree the user had just arranged.
func (m Model) rebuild(entries []vault.Entry, folders []vault.Folder) Model {
	// Every row's state, not only the open ones. Recording just the open ones
	// meant a rebuild could open but never close, so a folder the reader had
	// collapsed — a source root, most visibly — sprang open again on the next
	// one. Staging rebuilds, so marking anything for deletion reopened the tree.
	expanded := map[string]bool{}
	var collect func(ns []*node)
	collect = func(ns []*node) {
		for _, n := range ns {
			expanded[n.source+"\x00"+n.id] = n.expanded
			collect(n.children)
		}
	}
	collect(m.roots)

	// What the cursor is on, in the terms of whichever pane has it. selEntry
	// answers "which entry is current" even when the tree has focus — that is
	// what the copy keys want — so restoring by it threw the reader into the
	// entry table every time they marked a folder that happened to hold
	// entries. On an empty folder it did nothing, which is why it looked
	// arbitrary.
	var selectedEntry, selectedFolder string
	if m.focus == 1 {
		if e := m.selEntry(); e != nil {
			selectedEntry = e.ID
		}
	}
	if f := m.currentFolder(); f != nil {
		selectedFolder = f.source + "\x00" + f.id
	}

	// The tree is built from the vault as it will be — see project — while the
	// merged fields keep what the file actually says, which is what a reload and
	// a save compare against.
	m.mergedEntries, m.mergedFolders = entries, folders
	m.viewEntries, m.viewFolders = project(entries, folders, m.chg)
	m.matcher = search.New(m.viewEntries)
	m.roots = buildTree(m.viewEntries, m.viewFolders)
	m.nSrc = len(m.roots)

	// A row we knew about keeps exactly the state it had. A row we did not —
	// something just created, or a source that has only now been opened — keeps
	// what buildTree gave it.
	if len(expanded) > 0 {
		var restore func(ns []*node)
		restore = func(ns []*node) {
			for _, n := range ns {
				if was, known := expanded[n.source+"\x00"+n.id]; known {
					n.expanded = was
				}
				restore(n.children)
			}
		}
		restore(m.roots)
	}

	switch {
	case selectedEntry != "":
		m = m.selectEntryByID(selectedEntry)
	case selectedFolder != "":
		m = m.selectFolderByKey(selectedFolder)
	}
	m.refilter()
	return m
}

// restage rebuilds the tree after the staged set changed, so something just
// created is somewhere you can put things and something just renamed answers to
// its new name.
func (m Model) restage() Model { return m.rebuild(m.mergedEntries, m.mergedFolders) }

// selectFolderByKey puts the cursor back on a tree row after a rebuild, leaving
// the focus where it was — the tree.
func (m Model) selectFolderByKey(key string) Model {
	for i, tl := range m.visible() {
		if tl.node.source+"\x00"+tl.node.id == key {
			m.tsel = i
			return m
		}
	}
	return m
}

// selectEntryByID puts the cursor back on an entry after a rebuild, if it is
// still there.
func (m Model) selectEntryByID(id string) Model {
	flat := m.visible()
	for i, tl := range flat {
		for j, e := range tl.node.entries {
			if e.ID == id {
				m.tsel, m.esel, m.focus = i, j, 1
				return m
			}
		}
	}
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

// sources reads the configured sources fresh from disk (so the Settings tab
// reflects edits); an unreadable or empty config yields no rows.
func (m Model) sources() []config.Source {
	if m.configPath == "" {
		return nil
	}
	cfg, err := config.Load(m.configPath)
	if err != nil {
		return nil
	}
	return cfg.Sources
}

// keyringStatus probes the OS keyring for each source's saved password (the
// server password for Pleasant, the per-file password for kdbx). Computed once
// on entering Settings, not per render, to avoid repeated keychain access.
func keyringStatus(profs []config.Source) map[string]bool {
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

	case updateAvailableMsg:
		m.latest = msg.tag
		version.LatestVersion = msg.tag
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
			if m.tab == tabChanges && !m.saveConfirm && m.saveConflict == "" && m.edit == editNone {
				rows := m.changeRows(m.contentW())
				step := 3
				if msg.Button == tea.MouseButtonWheelUp {
					step = -3
				}
				m.chgScroll = clampScroll(m.chgScroll+step, len(rows), m.changesVisibleRows())
				return m, nil
			}
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

	case saveDoneMsg:
		m = m.onSaveDone(msg)
		if m.quitAfterSave && msg.err == nil {
			return m, tea.Sequence(clearClip, tea.Quit)
		}
		m.quitAfterSave = false
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			// Quitting with staged changes throws them away, and ctrl+c is the
			// most reflexive way to leave — so it asks too, not just q.
			if m.dirtyCount() > 0 && !m.quitGuard && !m.saving {
				m.quitGuard = true
				m.confirmSel = 0 // Save and quit leads; discarding is the danger
				return m, nil
			}
			return m, tea.Sequence(clearClip, tea.Quit)
		}
		// The write confirmations are global: ^s can be pressed from any tab, so
		// the overlay it opens has to own the keyboard from any tab too.
		if m.quitGuard {
			return m.updateQuitGuard(key)
		}
		if m.saveConfirm {
			return m.updateSaveConfirm(key)
		}
		if m.saveConflict != "" {
			return m.updateConflict(key)
		}
		if m.saving {
			return m, nil // keys are ignored while the file is being written
		}
		// The unlock phase owns every key until every source is open.
		if m.locked {
			return m.updateUnlock(key, msg)
		}
		// The attachment-save modal owns every key while it is open.
		if m.attach != attachNone {
			return m.updateAttach(key, msg)
		}
		// The unlock confirmation owns every key while it is up.
		if m.confirmUnlock != "" {
			return m.updateWriteConfirm(key), nil
		}
		// So does the editor — and it has to come before the help toggle and the
		// q-quits branch below. A form with free text in it cannot share a
		// keyboard with a single-letter quit: typing "q" into a title would end
		// the session and lose everything staged.
		if m.edit != editNone {
			nm, cmd := m.updateEditor(key, msg)
			return nm, cmd
		}
		if key == "?" && !m.typingText() {
			m.help = !m.help
			m.helpScroll = 0
			return m, nil
		}
		if m.help {
			return m.updateHelp(key), nil
		}
		// q quits everywhere except where a surface is capturing what you type.
		if key == "q" && !m.typingText() {
			if m.dirtyCount() > 0 {
				m.quitGuard = true
				m.confirmSel = 0 // Save and quit leads; discarding is the danger
				return m, nil
			}
			return m, tea.Sequence(clearClip, tea.Quit)
		}
		// ctrl+s asks to write, from anywhere except inside a modal that is
		// waiting for its own answer.
		if key == "ctrl+s" && !m.searchMode && !m.inSettingsModal() {
			return m.askToSave()
		}

		// Tab switching — display order comes from tabOrder(); the digits do not
		// match the internal m.tab indices. Not while something is capturing what
		// you type: 1 to 4 are characters there.
		if !m.typingText() {
			if idx, ok := tabForKey(key); ok {
				return m.switchTab(idx), nil
			}
		}

		if m.tab == tabChanges {
			return m.updateChanges(key)
		}
		// The Settings tab handles its own keys.
		if m.tab == 1 {
			return m.updateSettings(key, msg)
		}
		// The Generate tab handles its own keys.
		if m.tab == 2 {
			return m.updateGenerate(key)
		}

		// ctrl+w unlocks the source under the cursor for writing, or locks it
		// again. Deliberately a control chord rather than a letter: it changes
		// what the program is allowed to do, so it should not be reachable by a
		// stray keystroke.
		if key == "ctrl+w" && !m.searchMode {
			return m.toggleWriteLock(), nil
		}

		// ctrl+b collapses/expands the folder tree (Vault tab), reclaiming width for
		// the right pane. Collapsing moves focus off the now-hidden tree.
		if key == "ctrl+b" && !m.searchMode {
			m.treeCollapsed = !m.treeCollapsed
			if m.treeCollapsed && m.focus == 0 && !m.detail {
				m.focus = 1
			}
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
			case "pgup":
				_, step := m.panelRows()
				m.sel = clampIndex(m.sel-max(1, step), len(m.results))
				return m, nil
			case "pgdown":
				_, step := m.panelRows()
				m.sel = clampIndex(m.sel+max(1, step), len(m.results))
				return m, nil
			case "ctrl+y", "ctrl+u", "ctrl+o", "ctrl+t":
				// The copy chords work from inside the search box, because that
				// is where the result is chosen — ↑↓ are already moving the
				// selection here. They cannot collide with what is being typed:
				// they are chords, not characters. Requiring ↵ first was a rule
				// nobody would guess, and the help promised otherwise.
				cmd := m.copySel(copyWhat(key))
				return m, cmd
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.refilter()
			return m, cmd
		}

		// ENTRY DETAILS — keys are commands.
		if m.detail {
			m.flash = "" // any key dismisses a transient message
			if nm, handled := m.editKey(key); handled {
				return nm, nil
			}
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
			if nm, handled := m.editKey(key); handled {
				return nm, nil
			}
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

		// TREE BROWSE — the base surface.
		m.flash = "" // any key dismisses a transient message
		folder := m.currentFolder()
		if nm, handled := m.editKey(key); handled {
			return nm, nil
		}
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
		// Bulk open/close lives on letters, because a modified arrow is not a key
		// every terminal delivers: ctrl+arrow is a Mission Control shortcut on
		// macOS and never reaches the process, and some terminals send shift+arrow
		// stripped of its modifier. z/Z work everywhere. The shift arrows stay
		// bound for the terminals that do send them, but the help advertises the
		// letters — promising a key that silently does nothing is worse than not
		// offering it.
		case "z":
			return m.toggleSubtree(), nil
		case "Z":
			return m.toggleAll(), nil
		case "shift+right":
			return m.expandSubtree(true), nil
		case "shift+left":
			return m.expandSubtree(false), nil
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

// switchTab moves to a tab and puts its focus somewhere sensible.
func (m Model) switchTab(idx int) Model {
	m.tab, m.detail = idx, false
	switch idx {
	case tabGenerate:
		m.focus = 1 // land on the password, not the options
		if len(m.genList) == 0 {
			m.resetGen() // first visit: roll one so the pane is not empty
		}
	case tabChanges:
		m.chgSel = m.firstChangeSel(m.changeRows(m.contentW()))
	case tabSettings:
		m.focus = 0
		m.setKeyring = keyringStatus(m.sources())
	default:
		m.focus = 0
	}
	return m
}

// Who owns the keyboard.
//
// The global keys — ?, q, ctrl+s and the tab digits — are handled before the
// per-tab dispatch, so any surface that captures what you type has to be
// excluded here or its characters are eaten. It used to name only the search
// box, and the cost was not theoretical: a password containing "q" typed into
// the save-password prompt quit the program mid-entry, and the Generate tab's
// exclude field could not be given the digits 1 to 4 — the very characters
// somebody excluding digits from generated passwords needs.
//
// One predicate, so the next surface that takes text has one place to declare
// itself rather than three ifs to be forgotten in.
func (m Model) typingText() bool {
	switch {
	case m.searchMode:
		return true
	case m.edit == editInline:
		return true // a name is being typed into a row
	case m.inSettingsModal():
		return true // the add/edit form, the password prompt, the remove overlay
	case m.tab == tabGenerate && m.focus == 0 && m.genRow == genExclude:
		return true
	}
	return false
}

// inSettingsModal is the Settings tab showing something that owns the keyboard
// until it is answered or cancelled.
func (m Model) inSettingsModal() bool {
	return m.tab == tabSettings && m.setMode != setList
}

// copyWhat maps a copy chord to the field it copies, so the four bindings do
// not have to be spelled out on every surface that offers them.
func copyWhat(key string) string {
	switch key {
	case "ctrl+u":
		return "username"
	case "ctrl+o":
		return "URL"
	case "ctrl+t":
		return "totp"
	}
	return "password"
}
