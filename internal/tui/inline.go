package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/theme"
)

// Renaming a folder, in place.
//
// A folder's name is the only thing about it there is to edit, and it is one
// word. The modal form asked for a whole screen to collect it — a screen that
// covered the tree, which is the one thing that tells you which folder you are
// renaming. So the row itself becomes the field: the indent, the icon and the
// position stay put, and only the name turns editable.
//
// It stages the RenameGroup the form staged, so the Changes tab, the staged
// colouring and the save path do not know the difference.

// openInlineRename turns the folder row under the cursor into a text field.
func (m Model) openInlineRename(target, name string) Model {
	if h := m.handles[m.editSource]; h == nil {
		m.flash = "this source is not open for writing"
		return m
	}
	if folder, yes := m.inDoomedFolder(target); yes {
		m.flash = "that is going with " + lastSegment(folder) + " — undo the deletion first"
		return m
	}
	if _, doomed := m.stagedDeletion(target); doomed {
		m.flash = "this is staged for deletion — press " + toggleKey(false) + " to keep it first"
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
	ti.Width = max(4, m.inlineWidth())
	ti.PromptStyle = inlineStyle
	ti.TextStyle = inlineStyle

	m.edit = editInline
	m.editTarget = target
	m.inlineBefore = name
	m.inlineInput = ti
	return m
}

// inlineWidth is how much room the name has on the row it is being edited in —
// the same arithmetic treeLines does, because a field that thinks it is wider
// than its row scrolls to the wrong part of a long name.
func (m Model) inlineWidth() int {
	indent := 0
	if flat := m.visible(); m.tsel < len(flat) {
		indent = flat[m.tsel].depth * 2
	}
	// pane − frame − indent − the icon and the space after it − the cursor
	return m.leftPaneW() - 2 - indent - 2 - 1
}

// updateInlineRename owns the keyboard while a row is being renamed.
func (m Model) updateInlineRename(key string, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch key {
	case "esc":
		m.edit = editNone
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
	switch name {
	case "":
		// Not an error worth a modal, but not something to accept either: an
		// empty name would make the row unfindable in every other surface.
		m.flash = "a name cannot be empty — esc cancels"
		return m
	case m.inlineBefore:
		m.edit = editNone
		m.flash = "unchanged"
		return m
	}

	m.edit = editNone
	m.chg, _ = m.chg.Add(edit.Op{
		Kind: edit.RenameGroup, Source: m.editSource, Target: m.editTarget,
		Name: name, Was: m.inlineBefore,
	})
	m.flash = "staged: renamed to " + name + " · nothing is written until you save"
	return m.restage()
}

// renamingRow reports whether this tree row is the one being renamed, so
// treeLines can hand it the field instead of a label.
func (m Model) renamingRow(id string) bool {
	return m.edit == editInline && m.editTarget == id && id != ""
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
