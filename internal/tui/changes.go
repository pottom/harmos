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
type saveDoneMsg struct {
	source string
	err    error
}

// updateChanges handles the Changes tab.
func (m Model) updateChanges(key string) (Model, tea.Cmd) {
	if m.saveConfirm {
		return m.updateSaveConfirm(key)
	}
	if m.saveConflict != "" {
		return m.updateConflict(key)
	}

	changes := m.chg.Diff()
	switch key {
	case "up", "ctrl+p":
		if m.chgSel > 0 {
			m.chgSel--
		}
	case "down", "ctrl+n":
		if m.chgSel < len(changes)-1 {
			m.chgSel++
		}
	case "x":
		if m.chgSel < len(changes) {
			c := changes[m.chgSel]
			// Reverting a creation takes everything staged against it with it,
			// so say how many rather than surprising anyone.
			if n := len(m.chg.Cascade(c.Seq)); n > 0 {
				m.flash = fmt.Sprintf("reverted, and %d dependent change(s) with it", n)
			} else {
				m.flash = "reverted"
			}
			m.chg, _ = m.chg.Revert(c.Seq)
			m.chgSel = min(m.chgSel, max(0, len(m.chg.Diff())-1))
		}
	case "enter":
		// Jump to the entry this change is about, so "what is this?" is one key
		// rather than a search.
		if m.chgSel < len(changes) {
			m = m.switchTab(tabVault).selectEntryByID(changes[m.chgSel].Target)
		}
	case "ctrl+s":
		return m.askToSave()
	}
	return m, nil
}

// askToSave opens the confirmation, or says why there is nothing to do.
func (m Model) askToSave() (Model, tea.Cmd) {
	if m.chg.Empty() {
		m.flash = "nothing to save"
		return m, nil
	}
	m.saveConfirm = true
	_, m.confirmSel = saveChoices(m.permanentDeletes() > 0)
	return m, nil
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
	set := m.chg

	return func() tea.Msg {
		for _, src := range sources {
			h := handles[src]
			if h == nil {
				return saveDoneMsg{source: src, err: errors.New("this source is not open for writing")}
			}
			if err := h.Apply(set.ForSource(src)); err != nil {
				return saveDoneMsg{source: src, err: err}
			}
			if err := h.Save(); err != nil {
				return saveDoneMsg{source: src, err: err}
			}
		}
		return saveDoneMsg{}
	}
}

// onSaveDone folds the result back in.
func (m Model) onSaveDone(msg saveDoneMsg) Model {
	m.saving = false

	if msg.err != nil {
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

	// Re-read what is now on disk, so the tree shows the file rather than our
	// idea of it, and the next save's conflict check compares against reality.
	for _, src := range m.chg.Sources() {
		if v, err := m.reload(src); err == nil {
			m = m.rebuild(v.Entries, v.Folders)
		}
	}
	m.chg = edit.Set{}
	m.chgSel = 0
	m.flash = "saved"
	return m
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

	// The permanent deletes come first and in their own words. Everything else
	// here is recoverable from the backup; this is not recoverable from the file.
	if n := m.permanentDeletes(); n > 0 {
		lines = append(lines,
			"  "+theme.Bad.Render(fmt.Sprintf("%d permanent deletion(s)", n))+
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
			"  "+theme.Noted.Render(fmt.Sprintf("%s — %d change(s)", src, counts[src])),
			"  "+theme.Dimmed.Render(trunc(path, max(10, m.w-8))),
		)
		if backup != "" {
			lines = append(lines, "  "+theme.Faded.Render("backup: "+trunc(backup, max(10, m.w-16))))
		}
		lines = append(lines, "")
	}
	choices, _ := saveChoices(m.permanentDeletes() > 0)
	lines = append(lines, confirmButtons(choices, m.confirmSel, "←/→ choose · ↵ confirm · esc cancel")...)
	return m.modal("Save", m.chg.Summary(), lines, "")
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

// changesBody is the diff, or the explanation of why there is none.
func (m Model) changesBody(w int) []string {
	changes := m.chg.Diff()
	if len(changes) == 0 {
		return m.changesPlaceholder()
	}

	var out []string
	for i, c := range changes {
		style, marker := changeStyle(c.State)
		// One row shape, whether or not the cursor is on it: marker, name,
		// what is happening, which source. The selected row used to drop the
		// source and the deletion's strikethrough, so the row under the cursor
		// was the one row that did not say what it was — while being the row
		// the reader is looking at.
		text := marker + " " + c.Title + "  " + c.Detail + "  " + c.Source
		if i == m.chgSel {
			out = append(out, selRowStyle(theme.SelRow.Width(w), c.State).Render(trunc(ansiStrip(text), w)))
		} else {
			out = append(out, style.Render(marker+" "+c.Title)+
				theme.Dimmed.Render("  "+c.Detail)+theme.Faded.Render("  "+c.Source))
		}

		for _, l := range c.Lines {
			out = append(out, "      "+theme.Faded.Render(l.Op.Marker())+" "+
				theme.Dimmed.Render(pad(l.Field, 12))+" "+
				theme.Faded.Render(trunc(l.Old, 24))+
				theme.Dimmed.Render(" → ")+
				theme.Strong.Render(trunc(l.New, max(8, w-50))))
		}
		out = append(out, "")
	}
	return out
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
