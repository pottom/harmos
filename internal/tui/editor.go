package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/pwgen"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

// The editor.
//
// Nothing here writes a file. Every action stages an operation on m.chg; the
// vault handle is only touched to mint an identity for something new, and Save —
// which lands with the Changes tab — is still the only writer.
//
// The whole editor is gated above the global q-quits key. A form with free text
// in it cannot share a keyboard with a single-letter quit: typing "q" into a
// title would end the session, losing everything staged. That gating is pinned
// by a test, because it is the kind of thing a refactor silently undoes.

type editMode int

const (
	editNone editMode = iota
	editEntry
	editFolder
	editMove
	// editInline is the odd one out: it takes the keyboard like the others, but
	// it draws nothing of its own. The row being renamed is already on screen,
	// so the vault stays visible behind — see inline.go.
	editInline
)

// openEntryEditor loads an entry losslessly and stages nothing yet.
//
// An entry that has only been staged is not in the file, so the file cannot be
// asked for it: e on something created a moment ago used to fail with "no
// entry", which is a strange thing to be told about a row you are looking at.
// The staged draft is the entry, until a save makes it one.
func (m Model) openEntryEditor(id string) Model {
	h := m.handles[m.editSource]
	if h == nil {
		m.flash = "this source is not open for writing"
		return m
	}

	d, staged := m.stagedDraft(id)
	if !staged {
		fromFile, err := h.EntryDraft(id)
		if err != nil {
			m.flash = err.Error()
			return m
		}
		d = fromFile
	}

	m.edit = editEntry
	m.editTarget = id
	m.editNew = m.chg.StateOf(id) == edit.New
	// Where it lives is where the projection says it lives — the draft's own
	// group can be older than a move staged since.
	m.editParent = d.GroupID
	if home := m.homeOf(id); home != "" {
		m.editParent = home
	}
	m.editBefore = nil
	if !m.editNew {
		before := d
		m.editBefore = &before
	}
	m.editForm = entryForm(d, m.formWidth())
	return m
}

// stagedDraft is the latest staged version of an entry, if it has one. Editing
// twice has to start from what the first edit said, not from what the file says.
func (m Model) stagedDraft(id string) (edit.Draft, bool) {
	for _, op := range m.chg.Effective() {
		if op.Target == id && op.After != nil {
			return *op.After, true
		}
	}
	return edit.Draft{}, false
}

// openNewEntry mints an identity for an entry that does not exist yet.
//
// The identity comes first, and deliberately: everything staged afterwards — an
// edit, a move, a delete, an undo — names the same target, so there is no second
// identity space to reconcile when the set is finally applied. The probe creates
// and immediately removes the entry, leaving the file untouched.
func (m Model) openNewEntry(groupID string) Model {
	h := m.handles[m.editSource]
	if h == nil {
		m.flash = "this source is not open for writing"
		return m
	}
	if folder, yes := m.doomedParent(groupID); yes {
		m.flash = "that folder is going with " + folder + " — undo the deletion first"
		return m
	}
	d := edit.Draft{ID: h.MintEntryID(), GroupID: groupID}
	id := d.ID
	m.edit = editEntry
	m.editTarget = id
	m.editNew = true
	m.editParent = groupID
	m.editBefore = nil
	m.editForm = entryForm(d, m.formWidth())
	return m
}

// entryForm is the editor's field list.
func entryForm(d edit.Draft, width int) form {
	rows := make([]fieldRow, 0, len(d.Fields))
	for _, f := range d.Fields {
		rows = append(rows, newRow(f.Key, f.Value, f.Protected))
	}
	return newForm("Stage", width,
		textField("title", "Title", "what this is", d.Title).
			withValidation(func(v string) error {
				if v == "" {
					return errors.New("a title is required")
				}
				return nil
			}),
		textField("username", "Username", "", d.Username),
		maskedField("password", "Password", d.Password.Reveal()),
		textField("url", "URL", "", d.URL),
		// Masked, like the password: an otpauth:// URI *is* the shared seed, and
		// every other surface treats it as a secret — the detail pane shows only
		// the derived code, the diff masks it. The editor was printing it in
		// full, on e, with no reveal keypress, onto a screen that is in the
		// scrollback. ^r reveals it here, as it does the password.
		maskedField("totp", "TOTP", d.TOTP),
		textField("tags", "Tags", "separated by ;", d.Tags),
		multiField("notes", "Notes", d.Notes, 4),
		rowsField("fields", "Fields", rows),
	)
}

// draftFromForm reads the editor back into a draft.
func (m Model) draftFromForm() edit.Draft {
	keys, values, protected := m.editForm.RowValues("fields")
	fields := make([]edit.DraftField, len(keys))
	for i := range keys {
		fields[i] = edit.DraftField{Key: keys[i], Value: values[i], Protected: protected[i]}
	}
	d := edit.Draft{
		ID:       m.editTarget,
		GroupID:  m.editParent,
		Title:    m.editForm.Value("title"),
		Username: m.editForm.Value("username"),
		URL:      m.editForm.Value("url"),
		Notes:    m.editForm.Raw("notes"),
		Tags:     m.editForm.Value("tags"),
		Password: secret.New(m.editForm.Raw("password")),
		TOTP:     m.editForm.Value("totp"),
		Fields:   fields,
	}
	if m.editBefore != nil {
		d.Expires, d.ExpiryTime = m.editBefore.Expires, m.editBefore.ExpiryTime
		if d.GroupID == "" {
			// Where it lives is not the editor's business, and the draft it was
			// loaded from came off the file: taking the group from there undid a
			// move staged earlier, so the projection put the entry in one folder
			// while its GroupID named another.
			d.GroupID = m.editBefore.GroupID
		}
	}
	return d
}

// updateEditor owns every key while the editor is open.
func (m Model) updateEditor(key string, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.edit {
	case editMove:
		return m.updateMovePicker(key), nil
	case editInline:
		return m.updateInlineRename(key, msg)
	}

	switch key {
	case "esc":
		m.edit, m.editNew = editNone, false
		m.editBefore = nil
		return m, nil
	case "ctrl+g":
		// Roll a password with the generator the Generate tab already
		// configured, so the two cannot disagree about what a good one is.
		if pw, err := pwgen.Generate(m.genOpts); err == nil {
			m.editForm = m.editForm.setValue("password", pw)
			m.flash = "generated a password"
		} else {
			m.flash = err.Error()
		}
		return m, nil
	}

	f, cmd, submitted := m.editForm.Update(key, msg)
	m.editForm = f
	if !submitted {
		return m, cmd
	}

	f, ok := m.editForm.Validate()
	m.editForm = f
	if !ok {
		return m, cmd
	}
	return m.stageEdit(), cmd
}

// stageEdit records the change and closes the editor. Nothing is written.
func (m Model) stageEdit() Model {
	after := m.draftFromForm()

	op := edit.Op{Source: m.editSource, Target: m.editTarget, After: &after}
	switch {
	case m.edit == editFolder:
		op.Kind, op.Parent, op.Name = edit.CreateGroup, m.editParent, m.editForm.Value("name")
		op.After = nil
	case m.editNew:
		op.Kind, op.Parent = edit.CreateEntry, m.editParent
	default:
		op.Kind, op.Before = edit.EditEntry, m.editBefore
	}

	m.chg, _ = m.chg.Add(op)
	created, wasFolder := m.editNew, m.edit == editFolder
	target := m.editTarget
	m.edit, m.editNew = editNone, false
	m.editBefore = nil
	m.flash = "staged — nothing is written until you save"

	m = m.restage()
	if created {
		// Show what was just made, wherever it landed.
		m = m.revealTarget(target, wasFolder)
	}
	return m
}

// openNewFolder names a folder that does not exist yet.
//
// Renaming one no longer comes through here: a name is edited on its own row
// (inline.go), and a modal that covers the tree to ask for one word was the
// wrong shape for the question. This surface stays because a folder that does
// not exist yet has no row to edit.
func (m Model) openNewFolder(parentID string) Model {
	h := m.handles[m.editSource]
	if h == nil {
		m.flash = "this source is not open for writing"
		return m
	}
	if folder, yes := m.doomedParent(parentID); yes {
		m.flash = "that folder is going with " + folder + " — undo the deletion first"
		return m
	}
	m.edit = editFolder
	m.editNew = true
	m.editParent = parentID
	m.editTarget = h.MintGroupID()
	m.editForm = newForm("Stage", m.formWidth(),
		textField("name", "Name", "folder name", "").
			withValidation(func(v string) error {
				if v == "" {
					return errors.New("a folder needs a name")
				}
				return nil
			}),
	)
	return m
}

// stageDelete stages a deletion. It does not ask.
//
// There used to be a confirmation here, and it was answering a question nobody
// had: staging writes nothing, the row turns red and takes the delete marker the
// moment you press the key, and x on the Changes tab takes it back. A prompt in front
// of a reversible act is not a safeguard — it is a keystroke people learn to
// dismiss without reading, which is exactly what you do not want them doing at
// the one prompt that matters, the write.
//
// So the confirmation lives at the write, once, and it names what is about to
// happen — including a permanent delete, which is the only part of this that
// cannot be taken back afterwards.
// The key is a toggle. Pressing it again on the same row takes the staging back,
// because "I did not mean that" arrives one keystroke after "delete this", and
// making the reader travel to another tab to undo something they have not done
// yet is a strange thing to ask. It needs no second binding: the row already
// says which state it is in, so the same key can mean both directions.
//
// D on a row already staged for the bin does not toggle it off — it changes
// which deletion it is. Otherwise upgrading would mean pressing D twice, with
// the row briefly un-staged in between, which reads as the key having failed.
func (m Model) stageDelete(target, name string, isFolder, permanent bool) Model {
	if folder, yes := m.inDoomedFolder(target); yes {
		// It is already going, with the folder above it. Staging it separately
		// produced a set that could not be applied — and on the paths where it
		// could, the child was pulled out of the folder it was deleted with.
		m, _ = m.advanceCursor()
		m.flash = "already going with " + lastSegment(folder)
		return m
	}

	kind := edit.DeleteEntry
	if isFolder {
		kind = edit.DeleteGroup
	}
	perm := permanent || !m.binEnabled(m.editSource)

	if prev, ok := m.stagedDeletion(target); ok {
		m.chg, _ = m.chg.Revert(prev.Seq)
		if prev.Perm == perm {
			m = m.restage()
			m, moved := m.advanceCursor()
			m.flash = "no longer staged for deletion" + describes(name) + backTo(moved, permanent)
			return m
		}
	}

	if isFolder {
		// Anything under it that was already staged for deletion goes with the
		// folder now. Leaving both staged applied both, and the child was pulled
		// out of the folder it was deleted with.
		m = m.dropDeletionsUnder(target)
	}

	op := edit.Op{Kind: kind, Source: m.editSource, Target: target, Name: name, Perm: perm}
	if h := m.handles[m.editSource]; h != nil && !isFolder {
		if d, err := h.EntryDraft(target); err == nil {
			op.Before = &d
		}
	}
	m.chg, _ = m.chg.Add(op)

	// Say which of the two deletions this is — with the recycle bin switched
	// off, d is a permanent delete and silence would be a lie — and say how to
	// take it back.
	what := "entry"
	if isFolder {
		what = "folder and its contents"
	}
	where := "to the recycle bin"
	if perm {
		where = "permanently"
		if !permanent {
			where = "permanently (this database has no recycle bin)"
		}
	}
	// Move on. Working down a list should cost one key per row, not a key and an
	// arrow, which is how every file manager has done it for thirty years.
	//
	// The key always advances, whatever it did — staged, un-staged, or refused
	// because the thing is already going with its folder. It used to advance
	// only on staging, on the argument that un-staging is a correction and you
	// want to see what you corrected; but that is a rule with an exception you
	// have to know, and working down a list it stalled on exactly the rows you
	// had already dealt with.
	m = m.restage()
	m, moved := m.advanceCursor()

	m.flash = "staged: delete " + what + " " + where + backTo(moved, permanent)
	return m
}

// backTo names the key that undoes what just happened, from wherever the cursor
// now is.
func backTo(moved, permanent bool) string {
	if moved {
		return " · ↑ then " + toggleKey(permanent) + " undoes it"
	}
	return " · " + toggleKey(permanent) + " again undoes it"
}

// advanceCursor steps one row down whichever list has focus, and reports whether
// it actually moved — at the end of a list it stays, and the caller has to know,
// because a hint that names a key for "the row above" is a lie if there is none.
func (m Model) advanceCursor() (Model, bool) {
	// Only in a list. In the entry-detail split there is nothing to advance
	// through — moving the selection swaps the entry the pane is rendering
	// while the reader believes they are still looking at the one they marked,
	// which is a plausible route to copying the wrong password. In the results
	// list the move is to the tree cursor, which is not even on screen.
	if m.detail || m.showResults() {
		return m, false
	}
	if m.focus == 1 {
		if f := m.currentFolder(); f != nil && m.esel < len(f.entries)-1 {
			m.esel++
			return m, true
		}
		return m, false
	}
	if m.tsel < len(m.visible())-1 {
		m.tsel, m.esel = m.tsel+1, 0
		return m, true
	}
	return m, false
}

// toggleKey is the key that staged this deletion, so the hint names the key the
// reader just pressed rather than a general one.
func toggleKey(permanent bool) string {
	if permanent {
		return "D"
	}
	return "d"
}

// describes appends a name to a message when there is one worth showing.
func describes(name string) string {
	if name == "" {
		return ""
	}
	return ": " + name
}

// stagedDeletion is the deletion staged against a target, if any. It is what
// makes the delete key a toggle.
func (m Model) stagedDeletion(target string) (edit.Op, bool) {
	for _, op := range m.chg.Effective() {
		if op.Target != target {
			continue
		}
		if op.Kind == edit.DeleteEntry || op.Kind == edit.DeleteGroup {
			return op, true
		}
	}
	return edit.Op{}, false
}

// binEnabled reports whether the source keeps deleted items in a recycle bin.
func (m Model) binEnabled(source string) bool {
	h := m.handles[source]
	return h != nil && h.RecycleBinEnabled()
}

// openMovePicker chooses a destination folder.
func (m Model) openMovePicker(target string, isFolder bool) Model {
	m.edit = editMove
	m.editTarget = target
	m.editFolderTarget = isFolder
	m.moveSel = 0
	m.moveDests = m.moveDestinations()
	if len(m.moveDests) == 0 {
		m.edit = editNone
		m.flash = "nowhere to move it to"
	}
	return m
}

// moveDestinations lists the folders in the same source that the target could
// actually go to: not itself, and not where it already is.
func (m Model) moveDestinations() []vaultFolderRef {
	home := m.homeOf(m.editTarget)

	// A folder cannot be moved inside itself, and the guard for that lived in
	// the vault — after the review and the confirmation. The picker is where a
	// destination stops being offered.
	inside := m.editFolderTarget && m.editTarget != ""
	targetPath := ""
	if inside {
		if f, ok := m.folderByID(m.editTarget); ok {
			targetPath = f.Path
		}
	}

	var out []vaultFolderRef
	var walk func(ns []*node, depth int)
	walk = func(ns []*node, depth int) {
		for _, n := range ns {
			ok := n.source == m.editSource && n.id != "" && n.id != m.editTarget && n.id != home
			if ok && targetPath != "" && strings.HasPrefix(m.pathOfNode(n), targetPath+"/") {
				ok = false // its own descendant
			}
			if ok {
				if _, doomed := m.doomedParent(n.id); doomed {
					ok = false // about to stop existing
				}
			}
			if ok {
				out = append(out, vaultFolderRef{
					id:    n.id,
					label: strings.Repeat("  ", depth) + n.name,
					path:  strings.ReplaceAll(m.pathOfNode(n), "/", " › "),
				})
			}
			walk(n.children, depth+1)
		}
	}
	walk(m.roots, 0)
	return out
}

// homeOf is the folder something is in now — offering it as a destination is
// offering to do nothing.
func (m Model) homeOf(id string) string {
	for _, e := range m.viewEntries {
		if e.ID == id {
			return e.GroupID
		}
	}
	for _, f := range m.viewFolders {
		if f.ID == id {
			return f.ParentID
		}
	}
	return ""
}

type vaultFolderRef struct {
	id    string
	label string // indented, for the picker
	path  string // plain, for the messages
}

func (m Model) updateMovePicker(key string) Model {
	switch key {
	case "esc", "n":
		m.edit = editNone
	case "up", "ctrl+p":
		if m.moveSel > 0 {
			m.moveSel--
		}
	case "down", "ctrl+n":
		if m.moveSel < len(m.moveDests)-1 {
			m.moveSel++
		}
	case "enter":
		kind := edit.MoveEntry
		if m.editFolderTarget {
			kind = edit.MoveGroup
		}
		m.chg, _ = m.chg.Add(edit.Op{
			Kind: kind, Source: m.editSource, Target: m.editTarget,
			// The name and the origin travel with the operation: a review that
			// cannot say what moved, or where from, is not a review. The
			// destination the view reads back off the projection.
			Name:   m.nameOfTarget(m.editTarget, m.editFolderTarget),
			Was:    m.readablePath(m.homeOf(m.editTarget)),
			Parent: m.moveDests[m.moveSel].id,
		})
		m.flash = "staged: move to " + m.moveDests[m.moveSel].path + " · nothing is written until you save"
		m.edit = editNone
		m = m.restage()
	}
	return m
}

func (m Model) movePickerView() string {
	rows := max(3, m.h-10)
	start := windowStart(m.moveSel, rows, len(m.moveDests))
	end := min(start+rows, len(m.moveDests))

	lines := []string{""}
	for i := start; i < end; i++ {
		d := m.moveDests[i]
		if i == m.moveSel {
			lines = append(lines, theme.SelRow.Width(max(10, m.w-10)).Render("  "+d.label))
			continue
		}
		lines = append(lines, "  "+theme.Strong.Render(d.label))
	}
	lines = append(lines, "")
	return m.modal("Move to", m.editSource, lines, "↑↓ pick · ↵ stage · esc cancel")
}

// editorView renders whichever editor surface is open.
func (m Model) editorView() string {
	switch m.edit {
	case editMove:
		return m.movePickerView()
	}

	title := "Edit entry"
	switch {
	case m.edit == editFolder:
		title = "New folder"
	case m.editNew:
		title = "New entry"
	}

	// The amber border and the mode line are the same statement twice: being in
	// edit mode is a mode, and losing track of which one you are in is how a
	// password ends up typed into a search box.
	body := boxState(title, m.editSource, m.editForm.View(), m.w, max(3, m.h-1), panelEditing)
	mode := theme.Noted.Render("-- EDIT --") + "  " + m.editForm.Hint()
	return body + "\n" + m.footer(mode)
}

// editKey handles the editing hotkeys, and reports whether it took the key.
//
// They only exist on a source the user has unlocked. On a locked one the letters
// stay free for whatever they meant before, and pressing one says what to do
// rather than silently doing nothing — a key that appears dead is worse than one
// that explains itself.
func (m Model) editKey(key string) (Model, bool) {
	switch key {
	case "e", "n", "N", "d", "D", "m", "r":
	default:
		return m, false
	}

	source := m.writeCandidate()
	if source == "" {
		return m, false
	}
	if !m.writeUnlocked(source) {
		// Only claim the key if editing is possible at all here; otherwise leave
		// it to whatever else wants it.
		if ok, _ := m.canWrite(source); !ok {
			return m, false
		}
		m.flash = "^w unlocks " + source + " for editing"
		return m, true
	}
	m.editSource = source

	entry := m.editEntryTarget()
	folderID, folderName := m.currentFolderID()

	// In the results list the tree cursor is not on screen, so it cannot be the
	// parent for anything created here: it belongs to whichever source the user
	// last browsed, and staging against it produced ops whose source and parent
	// came from different vaults. The result's own folder is the answer.
	if m.showResults() {
		folderID, folderName = "", ""
		if entry != nil {
			folderID = entry.GroupID
			if f, ok := m.folderByID(folderID); ok {
				folderName = f.Name
			}
		}
	}

	switch key {
	case "e":
		if entry != nil {
			return m.openEntryEditor(entry.ID), true
		}
		if folderID != "" {
			// A folder has one editable thing, its name, so e lands where r does
			// rather than on a form with a single field in it.
			return m.openInlineRename(folderID, folderName, true), true
		}
	case "n":
		// An empty folder ID is the source's own root group, which is a real
		// folder in the file even though the tree shows the source there.
		return m.openNewEntry(folderID), true
	case "N":
		return m.openNewFolder(folderID), true
	case "r":
		// In place, on the row itself. The name is one word and the tree is what
		// tells you which one you are changing; a modal that covers the tree to
		// ask for it is the wrong shape for the question.
		if entry != nil {
			return m.openInlineRename(entry.ID, entry.Title, false), true
		}
		if folderID != "" {
			return m.openInlineRename(folderID, folderName, true), true
		}
	case "d", "D":
		perm := key == "D"
		if entry != nil {
			return m.stageDelete(entry.ID, entry.Title, false, perm), true
		}
		if folderID != "" {
			return m.stageDelete(folderID, folderName, true, perm), true
		}
	case "m":
		if entry != nil {
			return m.openMovePicker(entry.ID, false), true
		}
		if folderID != "" {
			return m.openMovePicker(folderID, true), true
		}
	}
	return m, true
}

// editEntryTarget is the entry an edit key acts on — and it is nil when the
// cursor is in the tree.
//
// selEntry answers a different question: which entry is "current", which is the
// selected row of the open folder's table whether or not the table has focus,
// because that is what the copy keys and the detail pane want. Using it here
// meant d on a folder row deleted an entry inside the folder instead of the
// folder — the wrong object, silently, with no way to tell from the screen.
func (m Model) editEntryTarget() *vault.Entry {
	if m.showResults() {
		return m.selEntry() // in the results list there are only entries
	}
	if m.focus != 1 {
		return nil // the tree has focus: the folder under the cursor is the target
	}
	return m.selEntry()
}

// currentFolderID is the folder under the cursor, if the cursor is on one.
func (m Model) currentFolderID() (id, name string) {
	flat := m.visible()
	if m.tsel >= len(flat) {
		return "", ""
	}
	n := flat[m.tsel].node
	return n.id, n.name
}
