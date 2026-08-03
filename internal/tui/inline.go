package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/theme"
)

// Renaming, in place.
//
// A rename is one word, and the modal form asked for a whole screen to collect
// it — a screen that covered the tree, which is the one thing that tells you
// what you are renaming. So the row itself becomes the field: the indent, the
// icon and the position stay put, and only the name turns editable. The
// surrounding vault is still on screen, which is the point.
//
// It stages the same operations the form did — RenameGroup for a folder, an
// EditEntry carrying a new Title for an entry — so the Changes tab, the staged
// colouring and the save path do not know the difference.

// openInlineRename turns the row under the cursor into a text field.
func (m Model) openInlineRename(target, name string, isFolder bool) Model {
	if h := m.handles[m.editSource]; h == nil {
		m.flash = "this source is not open for writing"
		return m
	}
	if why, yes := m.onItsWayOut(target); yes {
		m.flash = why
		return m
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.SetValue(name)
	ti.CursorEnd()
	ti.Focus()
	// The row is as wide as the row; the field cannot be wider. Scrolling inside
	// a long name is the textinput's job — it needs the real width now, at open,
	// because that is when it decides which slice of a long value to show. The
	// renderer trims to the same width again, so a miscount cannot spill into
	// the frame.
	ti.Width = max(4, m.inlineWidth(isFolder))
	ti.PromptStyle = inlineStyle
	ti.TextStyle = inlineStyle

	m.edit = editInline
	m.editTarget = target
	m.editFolderTarget = isFolder
	m.inlineBefore = name
	m.inlineInput = ti
	return m
}

// onItsWayOut reports whether a target is staged to stop existing, and how to
// undo that — either because it is marked itself, or because a folder above it
// is taking it.
//
// Changing something on its way out describes two different futures for one
// thing, and the review can only draw one of them. Shared by every key that
// alters a target so they cannot disagree about it: e used to open the editor
// happily on a row that r refused to rename.
func (m Model) onItsWayOut(target string) (string, bool) {
	if folder, yes := m.inDoomedFolder(target); yes {
		return "that is going with " + lastSegment(folder) + " — undo the deletion first", true
	}
	if op, doomed := m.stagedDeletion(target); doomed {
		return "this is staged for deletion — press " + toggleKey(op.Perm) + " to keep it first", true
	}
	return "", false
}

// inlineWidth is how much room the name has on the row it is being edited in —
// the same arithmetic the two renderers do, because a field that thinks it is
// wider than its row scrolls to the wrong part of a long name.
func (m Model) inlineWidth(isFolder bool) int {
	if isFolder {
		indent := 0
		if flat := m.visible(); m.tsel < len(flat) {
			indent = flat[m.tsel].depth * 2
		}
		// pane − frame − indent − the icon and the space after it − the cursor
		return m.leftPaneW() - 2 - indent - 2 - 1
	}
	// Whichever list is showing owns the Title column, and they are not the same
	// width. A field that thinks it is wider than its row scrolls to the wrong
	// part of a long name — the renderer trims, so nothing spills, but what is
	// under the cursor would be the wrong slice.
	if m.detail {
		return max(8, m.listPaneW()-2-2-9-4) // the detail's label column and margins
	}
	if m.showResults() {
		return max(8, (m.listPaneW()-2)*3/10) - 2 - 1
	}
	titleW, _, _, _ := entryCols(m.listPaneW() - 2)
	return titleW - 2 - 1 // the marker column, and the cursor
}

// updateInlineRename owns the keyboard while a row is being renamed.
func (m Model) updateInlineRename(key string, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch key {
	case "esc":
		m.edit = editNone
		if m.inlineNewSeq != 0 {
			// The row only existed to be typed on. Leaving it behind, named
			// whatever the placeholder said, is not what esc means anywhere else.
			m.chg, _ = m.chg.Revert(m.inlineNewSeq)
			m.inlineNewSeq = 0
			m.flash = "the new folder was not made"
			return m.restage(), nil
		}
		m.flash = "rename cancelled"
		return m, nil
	case "enter":
		return m.stageInlineRename(), nil
	}
	ti, cmd := m.inlineInput.Update(msg)
	m.inlineInput = ti
	return m, cmd
}

// stageInlineRename records the new name and closes the field.
func (m Model) stageInlineRename() Model {
	name := strings.TrimSpace(m.inlineInput.Value())
	if name == "" && m.inlineNewSeq != 0 {
		// A new folder committed without a name keeps the stand-in, the way
		// every file manager does. Refusing here would be refusing the most
		// ordinary thing somebody can do with this key.
		name = m.inlineBefore
	}
	switch name {
	case "":
		// Not an error worth a modal, but not something to accept either: an
		// empty name would make the row unfindable in every other surface.
		m.flash = "a name cannot be empty — esc cancels"
		return m
	case m.inlineBefore:
		// Nothing changed — unless this row was made to be typed on, in which
		// case something did: the folder. Calling that "unchanged" would be
		// describing the field rather than the vault.
		if m.inlineNewSeq == 0 {
			m.edit = editNone
			m.flash = "unchanged"
			return m
		}
	}

	target, isFolder := m.editTarget, m.editFolderTarget
	newSeq := m.inlineNewSeq
	m.edit, m.inlineNewSeq = editNone, 0

	if isFolder {
		// A creation being named rather than an existing folder being renamed.
		// The reduction folds create-then-rename into one create carrying the
		// final name, so the only thing that differs here is what is said.
		if newSeq != 0 {
			if name != m.inlineBefore {
				m.chg, _ = m.chg.Add(edit.Op{
					Kind: edit.RenameGroup, Source: m.editSource, Target: target,
					Name: name, Was: m.inlineBefore,
				})
			}
			m.flash = "staged: new folder " + name + " · nothing is written until you save"
			return m.restage()
		}
		m.chg, _ = m.chg.Add(edit.Op{
			Kind: edit.RenameGroup, Source: m.editSource, Target: target,
			Name: name, Was: m.inlineBefore,
		})
		m.flash = "staged: renamed to " + name + " · nothing is written until you save"
		return m.restage()
	}

	// An entry's name is its Title, and a title change is an edit like any
	// other — which means it needs the whole draft, not just the field that
	// changed, or the save would write an entry with everything else blank.
	d, staged := m.stagedDraft(target)
	var before *edit.Draft
	if !staged {
		h := m.handles[m.editSource]
		if h == nil {
			m.flash = "this source is not open for writing"
			return m
		}
		fromFile, err := h.EntryDraft(target)
		if err != nil {
			m.flash = err.Error()
			return m
		}
		d = fromFile
		was := fromFile
		before = &was
	}
	if home := m.homeOf(target); home != "" {
		d.GroupID = home // a move staged since is where it lives now
	}
	d.Title = name

	m.chg, _ = m.chg.Add(edit.Op{
		Kind: edit.EditEntry, Source: m.editSource, Target: target,
		Before: before, After: &d,
	})
	m.flash = "staged: renamed to " + name + " · nothing is written until you save"
	return m.restage()
}

// renamingRow reports whether this row is the one being renamed, so the two list
// renderers can hand it the field instead of a label.
func (m Model) renamingRow(id string, isFolder bool) bool {
	return m.edit == editInline && m.editFolderTarget == isFolder && m.editTarget == id && id != ""
}

// inlineStyle is how an editable name reads: the amber says the same thing the
// editor's border says — this is a mode, keys go here — and the underline says
// how much room the field has, which is the part a colour alone cannot say and
// a mono terminal still shows.
var inlineStyle = theme.Noted.Underline(true)

// inlineField is the row's name, rendered as the editable field.
//
// It occupies exactly w columns — no ellipsis, no padding past the edge. The
// textinput pads its value out to its own Width, which is one column short of
// this by construction, and the cursor takes the last.
func (m Model) inlineField(w int) string {
	w = max(4, w)
	v := m.inlineInput.View()
	if dw(v) > w {
		v = ansi.Truncate(v, w, "")
	}
	if fill := w - dw(v); fill > 0 {
		v += inlineStyle.Render(strings.Repeat(" ", fill))
	}
	return v
}
