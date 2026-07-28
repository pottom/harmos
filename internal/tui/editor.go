package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/pwgen"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/theme"
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
	editDelete
)

// openEntryEditor loads an entry losslessly and stages nothing yet.
func (m Model) openEntryEditor(id string) Model {
	h := m.handles[m.editSource]
	if h == nil {
		m.flash = "this source is not open for writing"
		return m
	}
	d, err := h.EntryDraft(id)
	if err != nil {
		m.flash = err.Error()
		return m
	}
	m.edit = editEntry
	m.editTarget = id
	m.editBefore = &d
	m.editForm = entryForm(d, m.formWidth())
	return m
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
	id, err := h.MintEntryID(groupID)
	if err != nil {
		m.flash = err.Error()
		return m
	}
	d := edit.Draft{ID: id, GroupID: groupID}
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
		textField("totp", "TOTP", "otpauth://…", d.TOTP),
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
		d.GroupID = m.editBefore.GroupID
		d.Expires, d.ExpiryTime = m.editBefore.Expires, m.editBefore.ExpiryTime
	}
	return d
}

// updateEditor owns every key while the editor is open.
func (m Model) updateEditor(key string, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.edit {
	case editDelete:
		return m.updateDeleteConfirm(key), nil
	case editMove:
		return m.updateMovePicker(key), nil
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
	case m.edit == editFolder && m.editNew:
		op.Kind, op.Parent, op.Name = edit.CreateGroup, m.editParent, m.editForm.Value("name")
		op.After = nil
	case m.edit == editFolder:
		op.Kind, op.Name = edit.RenameGroup, m.editForm.Value("name")
		op.After = nil
	case m.editNew:
		op.Kind, op.Parent = edit.CreateEntry, m.editParent
	default:
		op.Kind, op.Before = edit.EditEntry, m.editBefore
	}

	m.chg, _ = m.chg.Add(op)
	m.edit, m.editNew = editNone, false
	m.editBefore = nil
	m.flash = "staged — nothing is written until you save"
	return m
}

// openFolderEditor creates or renames a folder.
func (m Model) openFolderEditor(parentID, existingID, name string) Model {
	h := m.handles[m.editSource]
	if h == nil {
		m.flash = "this source is not open for writing"
		return m
	}
	m.edit = editFolder
	m.editNew = existingID == ""
	m.editParent = parentID
	m.editTarget = existingID
	if m.editNew {
		id, err := h.MintGroupID(parentID)
		if err != nil {
			m.flash = err.Error()
			return m
		}
		m.editTarget = id
	}
	m.editForm = newForm("Stage", m.formWidth(),
		textField("name", "Name", "folder name", name).
			withValidation(func(v string) error {
				if v == "" {
					return errors.New("a folder needs a name")
				}
				return nil
			}),
	)
	return m
}

// openDeleteConfirm asks before staging a deletion.
func (m Model) openDeleteConfirm(target string, isFolder, permanent bool) Model {
	m.edit = editDelete
	m.editTarget = target
	m.editFolderTarget = isFolder
	m.editPerm = permanent
	return m
}

func (m Model) updateDeleteConfirm(key string) Model {
	switch key {
	case "y", "Y", "enter":
		kind := edit.DeleteEntry
		if m.editFolderTarget {
			kind = edit.DeleteGroup
		}
		op := edit.Op{
			Kind: kind, Source: m.editSource, Target: m.editTarget,
			Perm: m.editPerm || !m.binEnabled(m.editSource),
		}
		if !m.editFolderTarget {
			if d, err := m.handles[m.editSource].EntryDraft(m.editTarget); err == nil {
				op.Before = &d
			}
		}
		m.chg, _ = m.chg.Add(op)
		m.flash = "staged — nothing is written until you save"
	}
	m.edit = editNone
	return m
}

// binEnabled reports whether the source keeps deleted items in a recycle bin.
func (m Model) binEnabled(source string) bool {
	h := m.handles[source]
	return h != nil && h.RecycleBinEnabled()
}

// deleteConfirmView says what will actually happen — which is not always what
// the key implies. With the recycle bin switched off, a "move to bin" delete is
// a permanent one, and a prompt that did not say so would be lying.
func (m Model) deleteConfirmView() string {
	what := "entry"
	if m.editFolderTarget {
		what = "folder and everything in it"
	}
	permanent := m.editPerm || !m.binEnabled(m.editSource)

	lines := []string{"", "  " + theme.Strong.Render("Delete this "+what+"?"), ""}
	if permanent {
		lines = append(lines,
			"  "+theme.Bad.Render("PERMANENTLY")+theme.Dimmed.Render(" — it cannot be recovered from the file"))
		if !m.editPerm {
			lines = append(lines,
				"  "+theme.Faded.Render("this database has its recycle bin switched off"))
		}
	} else {
		lines = append(lines, "  "+theme.Dimmed.Render("into the recycle bin, where it can be restored"))
	}
	lines = append(lines, "", "  "+theme.Faded.Render("staged only — nothing is written until you save"), "")
	return m.modal("Delete", m.editSource, lines, "y stage · n cancel")
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

// moveDestinations lists the folders in the same source.
func (m Model) moveDestinations() []vaultFolderRef {
	var out []vaultFolderRef
	var walk func(ns []*node, depth int)
	walk = func(ns []*node, depth int) {
		for _, n := range ns {
			if n.source == m.editSource && n.id != "" && n.id != m.editTarget {
				out = append(out, vaultFolderRef{id: n.id, label: strings.Repeat("  ", depth) + n.name})
			}
			walk(n.children, depth+1)
		}
	}
	walk(m.roots, 0)
	return out
}

type vaultFolderRef struct {
	id    string
	label string
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
			Parent: m.moveDests[m.moveSel].id,
		})
		m.flash = "staged — nothing is written until you save"
		m.edit = editNone
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
	case editDelete:
		return m.deleteConfirmView()
	case editMove:
		return m.movePickerView()
	}

	title := "Edit entry"
	switch {
	case m.edit == editFolder && m.editNew:
		title = "New folder"
	case m.edit == editFolder:
		title = "Rename folder"
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

	entry := m.selEntry()
	folderID, folderName := m.currentFolderID()

	switch key {
	case "e":
		if entry != nil {
			return m.openEntryEditor(entry.ID), true
		}
		if folderID != "" {
			return m.openFolderEditor("", folderID, folderName), true
		}
	case "n":
		if folderID != "" {
			return m.openNewEntry(folderID), true
		}
		m.flash = "pick a folder to create the entry in"
		return m, true
	case "N":
		return m.openFolderEditor(folderID, "", ""), true
	case "r":
		if folderID != "" {
			return m.openFolderEditor("", folderID, folderName), true
		}
	case "d", "D":
		perm := key == "D"
		if entry != nil {
			return m.openDeleteConfirm(entry.ID, false, perm), true
		}
		if folderID != "" {
			return m.openDeleteConfirm(folderID, true, perm), true
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

// currentFolderID is the folder under the cursor, if the cursor is on one.
func (m Model) currentFolderID() (id, name string) {
	flat := m.visible()
	if m.tsel >= len(flat) {
		return "", ""
	}
	n := flat[m.tsel].node
	return n.id, n.name
}
