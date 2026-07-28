package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

// The Changes tab and the save.
//
// Saving is the only thing in harmos that writes to a user's vault, and it is
// deliberately the end of a chain of deliberate acts: unlock the source, stage a
// change, look at it, confirm. Each step is reversible until the last.

// saveDoneMsg reports the outcome of a save that ran off the update loop.
//
// It carries what was written as well as what failed. A save can touch several
// sources, and one of them failing says nothing about the ones already on disk:
// keeping their changes staged made a retry write them a second time, entry for
// entry, under the same UUIDs.
type saveDoneMsg struct {
	written []string // sources whose changes are on disk now
	source  string   // the one that failed, if any
	err     error
}

// updateChanges handles the Changes tab.
func (m Model) updateChanges(key string) (Model, tea.Cmd) {
	if m.saveConfirm {
		return m.updateSaveConfirm(key)
	}
	if m.saveConflict != "" {
		return m.updateConflict(key)
	}

	rows := m.changeRows(m.contentW())
	sel := selectableRows(rows)
	cur := m.contextRow(rows)

	visible := m.changesVisibleRows()

	switch key {
	case "up", "ctrl+p":
		if m.chgSel > 0 {
			m.chgSel--
		}
	case "down", "ctrl+n":
		if m.chgSel < len(sel)-1 {
			m.chgSel++
		}
	// The page keys move the cursor a page, as they do in every other list in
	// the program — a page key that scrolled past the cursor would leave the
	// selection somewhere off screen. The wheel is the one that moves the
	// document on its own.
	case "pgup":
		m.chgSel = clampIndex(m.chgSel-visible, len(sel))
	case "pgdown":
		m.chgSel = clampIndex(m.chgSel+visible, len(sel))
	case "home":
		m.chgSel = 0
	case "end":
		m.chgSel = max(0, len(sel)-1)
	// Folding uses the vault tree's keys, doing the vault tree's things, because
	// this is the same shape and the reader already knows how to work it.
	case "z":
		m = m.setFold(cur, !m.folded(cur))
	case "Z":
		m = m.foldAllChanges()
	case "left":
		// Close what is open; from something already closed, step out to the
		// heading above it — the tree's ← exactly.
		if m.folded(cur) || cur.kind == rowFolder && m.folded(cur) {
			m = m.selectHeadingOf(rows, cur)
		} else if cur.target != "" {
			m = m.setFold(cur, true)
		} else {
			m = m.selectHeadingOf(rows, cur)
		}
	case "right":
		// Open what is closed; from something already open, step into it.
		if m.folded(cur) {
			m = m.setFold(cur, false)
		} else if cur.kind == rowFolder {
			m = m.selectFirstChangeIn(rows, cur)
		}
	case "shift+left":
		m = m.foldBranch(rows, cur, true)
	case "shift+right":
		m = m.foldBranch(rows, cur, false)
	case "x":
		// x undoes what the cursor is on — one change, or, on a heading, the
		// whole group under it. Reverting a creation takes everything staged
		// against it too, so say how many rather than surprising anyone.
		switch cur.kind {
		case rowChange:
			if n := len(m.chg.Cascade(cur.seq)); n > 0 {
				m.flash = fmt.Sprintf("reverted, and %d dependent change(s) with it", n)
			} else {
				m.flash = "reverted"
			}
			m.chg, _ = m.chg.Revert(cur.seq)
		case rowFolder:
			n := 0
			for _, r := range rows {
				if r.kind == rowChange && m.groupOf(rows, r) == cur.target {
					m.chg, _ = m.chg.Revert(r.seq)
					n++
				}
			}
			m.flash = fmt.Sprintf("reverted %d change(s) here", n)
		}
		m = m.restage()
		m.chgSel = clampIndex(m.chgSel, len(selectableRows(m.changeRows(m.contentW()))))
	case "enter":
		// Jump to the entry this change is about, so "what is this?" is one key
		// rather than a search.
		if cur.kind == rowChange {
			m = m.switchTab(tabVault).selectEntryByID(cur.target)
		}
	case "ctrl+s":
		return m.askToSave()
	}

	// Whatever the key did to the cursor, keep it on screen.
	rows = m.changeRows(m.contentW())
	m.chgScroll = scrollToShow(m.chgScroll, m.chgCursor(rows), visible, len(rows))
	return m, nil
}

// foldAllChanges folds everything, or unfolds everything if nothing is folded —
// the same decision the vault tree's Z makes.
func (m Model) foldAllChanges() Model {
	rows := m.changeRows(m.contentW())
	anyOpen := false
	for _, r := range rows {
		if r.selectable() && !m.chgFold[foldKey(r.kind, r.target)] {
			anyOpen = true
			break
		}
	}
	fold := map[string]bool{}
	if anyOpen {
		for _, r := range rows {
			if r.selectable() {
				fold[foldKey(r.kind, r.target)] = true
			}
		}
	}
	m.chgFold = fold
	m.chgSel = clampIndex(m.chgSel, len(selectableRows(m.changeRows(m.contentW()))))
	return m
}

// askToSave opens the confirmation, or says why there is nothing to do.
func (m Model) askToSave() (Model, tea.Cmd) {
	if m.chg.Empty() {
		m.flash = "nothing to save"
		return m, nil
	}
	// A source that has been locked again is not written, and saying so before
	// the confirmation is better than a failure after it.
	if locked := m.lockedWithChanges(); len(locked) > 0 {
		m.flash = "locked for writing: " + strings.Join(locked, ", ") + " — ^w unlocks it"
		return m, nil
	}
	m.saveConfirm = true
	_, m.confirmSel = saveChoices(m.permanentDeletes() > 0)
	return m, nil
}

// lockedWithChanges is the sources that have staged changes and are no longer
// unlocked for writing.
func (m Model) lockedWithChanges() []string {
	var out []string
	for _, src := range m.chg.Sources() {
		if !m.writeOK[src] {
			out = append(out, src)
		}
	}
	return out
}

// permanentDeletes counts the staged deletions that skip the recycle bin. It is
// what decides how this confirmation leads — see saveChoices.
func (m Model) permanentDeletes() int {
	n := 0
	for _, op := range m.chg.Effective() {
		if op.Perm && (op.Kind == edit.DeleteEntry || op.Kind == edit.DeleteGroup) {
			n++
		}
	}
	return n
}

func (m Model) updateSaveConfirm(key string) (Model, tea.Cmd) {
	choices, _ := saveChoices(m.permanentDeletes() > 0)
	if sel, moved := confirmNav(key, m.confirmSel, len(choices)); moved {
		m.confirmSel = sel
		return m, nil
	}

	write := false
	switch key {
	case "enter":
		write = m.confirmSel == 0
	case "y", "Y":
		write = true
	case "esc", "n", "N":
	default:
		return m, nil // an unrelated key does not dismiss a confirmation
	}

	m.saveConfirm = false
	if !write {
		return m, nil
	}
	m.saving = true
	return m, m.saveCmd()
}

// saveCmd applies and writes, off the update loop.
//
// Argon2 takes the better part of a second, and re-deriving the key is
// unavoidable on a save. Doing it inside Update would freeze the interface at
// exactly the moment the user most wants to see that something is happening.
func (m Model) saveCmd() tea.Cmd {
	sources := m.chg.Sources()
	handles := m.handles
	unlocked := m.writeOK
	set := m.chg

	return func() tea.Msg {
		var written []string
		for _, src := range sources {
			h := handles[src]
			if h == nil {
				return saveDoneMsg{written: written, source: src, err: errors.New("this source is not open for writing")}
			}
			// The last gate before the only writer. askToSave refuses a locked
			// source too, but this is the one that runs next to the write.
			if !unlocked[src] {
				return saveDoneMsg{written: written, source: src,
					err: errors.New("locked for writing — ^w unlocks it")}
			}
			if err := h.Apply(set.ForSource(src)); err != nil {
				return saveDoneMsg{written: written, source: src, err: err}
			}
			if err := h.Save(); err != nil {
				return saveDoneMsg{written: written, source: src, err: err}
			}
			written = append(written, src)
		}
		return saveDoneMsg{written: written}
	}
}

// onSaveDone folds the result back in.
func (m Model) onSaveDone(msg saveDoneMsg) Model {
	m.saving = false

	// Whatever else happened, the sources that were written are written: re-read
	// them and take their changes off the staged set. Leaving them on it made the
	// next attempt write them again — three saves, three copies of the same
	// entry, each carrying the same kdbx UUID.
	for _, src := range msg.written {
		if v, err := m.reload(src); err == nil {
			m = m.rebuild(v.Entries, v.Folders)
		}
		m.chg = m.chg.DropSource(src)
	}

	if msg.err != nil {
		// Apply mutates the handle's database operation by operation and does
		// not roll back, so a set that failed halfway has left that database
		// disagreeing with the file. Re-reading it is what keeps the next save
		// from writing damage nobody reviewed — the sequence that found this
		// deleted a folder and its entries with no line about it anywhere.
		m = m.discardHandleState(msg.source)

		if errors.Is(msg.err, vault.ErrChangedUnderneath) {
			// Somebody else wrote the file while we had it open. Refuse, and
			// keep the staged set intact — the decision is the user's, and
			// throwing their work away to make the error simpler would be the
			// wrong trade.
			m.saveConflict = msg.source
			m.confirmSel = 0 // Reload leads: it is the answer that loses nothing
			return m
		}
		m.flash = "save failed: " + msg.err.Error()
		return m
	}

	m.chg = edit.Set{}
	m.chgSel = 0
	m.chgFold, m.chgScroll = nil, 0
	m.flash = "saved"
	return m
}

// discardHandleState throws away a half-applied in-memory database by reopening
// the file. The staged set is untouched: it is still what the user asked for,
// and it can be applied again to a handle that matches the file.
func (m Model) discardHandleState(source string) Model {
	h := m.handles[source]
	if h == nil {
		return m
	}
	fresh, err := vault.Reopen(h)
	if err != nil {
		return m
	}
	m.handles[source] = fresh
	v := fresh.Snapshot()

	entries := make([]vault.Entry, 0, len(m.mergedEntries))
	for _, e := range m.mergedEntries {
		if e.Source != source {
			entries = append(entries, e)
		}
	}
	folders := make([]vault.Folder, 0, len(m.mergedFolders))
	for _, f := range m.mergedFolders {
		if f.Source != source {
			folders = append(folders, f)
		}
	}
	return m.rebuild(append(entries, v.Entries...), append(folders, v.Folders...))
}

// reload re-reads one source through its handle and merges it into the model's
// view of every source.
func (m *Model) reload(source string) (*vault.Vault, error) {
	h := m.handles[source]
	if h == nil {
		return nil, errors.New("no handle")
	}
	fresh, err := vault.Reopen(h)
	if err != nil {
		return nil, err
	}
	m.handles[source] = fresh

	v := fresh.Snapshot()
	entries := make([]vault.Entry, 0, len(m.mergedEntries))
	for _, e := range m.mergedEntries {
		if e.Source != source {
			entries = append(entries, e)
		}
	}
	entries = append(entries, v.Entries...)

	folders := make([]vault.Folder, 0, len(m.mergedFolders))
	for _, f := range m.mergedFolders {
		if f.Source != source {
			folders = append(folders, f)
		}
	}
	folders = append(folders, v.Folders...)

	m.mergedEntries, m.mergedFolders = entries, folders
	return &vault.Vault{Source: source, Entries: entries, Folders: folders}, nil
}

func (m Model) updateConflict(key string) (Model, tea.Cmd) {
	if sel, moved := confirmNav(key, m.confirmSel, len(conflictChoices())); moved {
		m.confirmSel = sel
		return m, nil
	}
	if key == "enter" {
		if m.confirmSel == 0 {
			key = "r"
		} else {
			key = "esc"
		}
	}
	switch key {
	case "r", "R":
		// Re-read and keep the staged changes, so they can be reviewed against
		// what the file now says before being applied again.
		if v, err := m.reload(m.saveConflict); err == nil {
			m = m.rebuild(v.Entries, v.Folders)
			m.flash = "reloaded — your changes are still staged"
		}
		m.saveConflict = ""
	case "esc", "n", "N":
		m.saveConflict = ""
	}
	return m, nil
}

// saveConfirmView names everything the save is about to do.
//
// This is the only confirmation in the editing flow: staging asks nothing,
// because staging writes nothing and every staged change can be taken back from
// this tab. So this one has to carry the whole weight — which file, how much,
// where the backup goes, and above all what cannot be undone afterwards.
func (m Model) saveConfirmView() string {
	counts := m.chg.Counts()
	lines := []string{"", "  " + theme.Strong.Render("Write these changes?"), ""}

	// What cannot be undone comes first, and counted in things rather than in
	// operations: one keystroke on a folder can remove forty entries, and this
	// is the number the reader is agreeing to.
	if n := m.permanentlyRemoved(); n > 0 {
		lines = append(lines,
			"  "+theme.Bad.Render(plural(n, "thing", "things")+" deleted permanently")+
				theme.Dimmed.Render(" — not into the recycle bin"),
			"  "+theme.Faded.Render("the backup below still holds them; the vault will not"),
			"")
	}

	for _, src := range m.chg.Sources() {
		h := m.handles[src]
		path := src
		backup := ""
		if h != nil {
			path = h.Path()
			backup = h.BackupPath()
		}
		lines = append(lines,
			"  "+theme.Noted.Render(fmt.Sprintf("%s — %s", src, plural(counts[src], "change", "changes"))),
		)
		// What the changes come to, counted in the things a vault is made of.
		// The operation count is a number about the program; this is the one the
		// reader is actually agreeing to.
		lines = append(lines, m.impactOf(src).lines()...)
		lines = append(lines,
			"  "+theme.Dimmed.Render(trunc(path, max(10, m.w-8))),
		)
		if backup != "" {
			lines = append(lines, "  "+theme.Faded.Render("backup: "+trunc(backup, max(10, m.w-16))))
		}
		lines = append(lines, "")
	}
	choices, _ := saveChoices(m.permanentDeletes() > 0)
	lines = append(lines, confirmButtons(choices, m.confirmSel, "←/→ choose · ↵ confirm · esc cancel")...)
	return m.modal("Save", m.impactTally(), lines, "")
}

// conflictView is what happens when the file moved under us.
func (m Model) conflictView() string {
	lines := []string{
		"",
		"  " + theme.Bad.Render("The file changed on disk since it was opened."),
		"",
		"  " + theme.Dimmed.Render("Something else — KeePassXC, a sync tool — wrote to"),
		"  " + theme.Dimmed.Render(m.saveConflict+" while harmos had it open."),
		"",
		"  " + theme.Faded.Render("Nothing was written. Your changes are still staged."),
		"",
	}
	lines = append(lines, confirmButtons(conflictChoices(), m.confirmSel, "←/→ choose · ↵ confirm · esc leave it")...)
	return m.modal("Conflict", m.saveConflict, lines, "")
}

// quitGuardView asks before throwing staged work away.
func (m Model) quitGuardView() string {
	lines := []string{
		"",
		"  " + theme.Strong.Render(fmt.Sprintf("%d change(s) are staged and not written.", m.dirtyCount())),
		"",
		"  " + theme.Dimmed.Render(m.chg.Summary()),
		"",
	}
	lines = append(lines, confirmButtons(quitChoices(), m.confirmSel, "←/→ choose · ↵ confirm · esc stay")...)
	return m.modal("Quit", "unsaved", lines, "")
}

func (m Model) updateQuitGuard(key string) (Model, tea.Cmd) {
	if sel, moved := confirmNav(key, m.confirmSel, len(quitChoices())); moved {
		m.confirmSel = sel
		return m, nil
	}
	if key == "enter" {
		key = []string{"s", "d", "esc"}[m.confirmSel]
	}
	switch key {
	case "s", "S":
		m.quitGuard = false
		m.quitAfterSave = true
		m.saving = true
		return m, m.saveCmd()
	case "d", "D":
		return m, tea.Sequence(clearClip, tea.Quit)
	case "esc", "n", "N":
		m.quitGuard = false
	}
	return m, nil
}

func ansiStrip(s string) string {
	// The selected row renders a plain string; anything already styled would
	// fight the row background.
	for strings.Contains(s, "\x1b") {
		i := strings.Index(s, "\x1b")
		j := strings.Index(s[i:], "m")
		if j < 0 {
			break
		}
		s = s[:i] + s[i+j+1:]
	}
	return s
}
