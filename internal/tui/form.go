package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
)

// formField is one row of the add/edit form: a labelled text input, or the Type
// toggle (which has no input).
type formField struct {
	key    string // name, path, keyfile, url, user, cache, ca
	label  string
	input  textinput.Model
	toggle bool // the Type row
}

// buildForm assembles the fields for a source type. On add, a Type toggle leads;
// on edit the type is fixed. pref pre-fills values (for edit or to keep the name
// across a type toggle).
func buildForm(isPps, editing bool, pref map[string]string) []formField {
	mk := func(key, label string) formField {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Width = 48
		if pref != nil {
			ti.SetValue(pref[key])
		}
		return formField{key: key, label: label, input: ti}
	}
	var f []formField
	if !editing {
		f = append(f, formField{key: "type", label: "Type", toggle: true})
	}
	f = append(f, mk("name", "Name"))
	if isPps {
		f = append(f, mk("url", "URL"), mk("user", "User"), mk("cache", "Cache"), mk("ca", "CA bundle"))
	} else {
		f = append(f, mk("path", "Path"), mk("keyfile", "Keyfile"))
	}
	return f
}

func (m Model) openAddForm() Model {
	m.setMode = setForm
	m.setStatus = ""
	m.formEditing, m.formOrig, m.formPps, m.formFocus = false, "", false, 0
	m.form = buildForm(false, false, nil)
	return m.refocusForm()
}

func (m Model) openEditForm(p config.Profile) Model {
	m.setMode = setForm
	m.setStatus = ""
	m.formEditing, m.formOrig = true, p.Name
	m.formPps = p.Type == config.Pleasant
	m.formFocus = 0
	m.form = buildForm(m.formPps, true, map[string]string{
		"name": p.Name, "path": p.Path, "keyfile": p.Keyfile,
		"url": p.URL, "user": p.User, "cache": p.Cache, "ca": p.CABundle,
	})
	return m.refocusForm()
}

// refocusForm focuses the input at formFocus (blurring the rest).
func (m Model) refocusForm() Model {
	for i := range m.form {
		if i == m.formFocus && !m.form[i].toggle {
			m.form[i].input.Focus()
		} else {
			m.form[i].input.Blur()
		}
	}
	return m
}

func (m Model) formValue(key string) string {
	for _, f := range m.form {
		if f.key == key {
			return strings.TrimSpace(f.input.Value())
		}
	}
	return ""
}

func (m Model) updateForm(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	save := len(m.form) // formFocus == save → the Save button
	switch key {
	case "esc":
		m.setMode = setList
		return m, nil
	case "tab", "down":
		m.formFocus = (m.formFocus + 1) % (save + 1)
		return m.refocusForm(), nil
	case "shift+tab", "up":
		m.formFocus = (m.formFocus - 1 + save + 1) % (save + 1)
		return m.refocusForm(), nil
	case "enter":
		return m.submitForm(), nil
	}

	// the Type toggle
	if m.formFocus < len(m.form) && m.form[m.formFocus].toggle {
		if key == "left" || key == "right" || key == " " {
			m.formPps = !m.formPps
			m.form = buildForm(m.formPps, false, map[string]string{"name": m.formValue("name")})
			if m.formFocus >= len(m.form) {
				m.formFocus = 0
			}
			return m.refocusForm(), nil
		}
		return m, nil
	}

	// route to the focused input
	if m.formFocus < len(m.form) {
		var cmd tea.Cmd
		m.form[m.formFocus].input, cmd = m.form[m.formFocus].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// submitForm validates and writes the profile, then returns to the list.
func (m Model) submitForm() Model {
	name := m.formValue("name")
	if name == "" {
		m.setStatus = "name is required"
		return m
	}

	var verb string
	var err error
	if m.formPps {
		url, user := m.formValue("url"), m.formValue("user")
		if url == "" || user == "" {
			m.setStatus = "url and user are required"
			return m
		}
		cache := m.formValue("cache")
		if cache == "" {
			if cache, err = config.DefaultCachePath(name); err != nil {
				m.setStatus = err.Error()
				return m
			}
		}
		if cache, err = config.AbsPath(cache); err != nil {
			m.setStatus = err.Error()
			return m
		}
		_ = os.MkdirAll(filepath.Dir(cache), 0o700)
		ca := m.formValue("ca")
		if ca != "" {
			if ca, err = config.AbsPath(ca); err != nil {
				m.setStatus = err.Error()
				return m
			}
		}
		verb, err = m.writeProfile(name, func(overwrite bool) (string, error) {
			return config.WritePleasantProfile(m.configPath, name, url, user, cache, ca, overwrite)
		})
	} else {
		path := m.formValue("path")
		if path == "" {
			m.setStatus = "path is required"
			return m
		}
		if path, err = config.AbsPath(path); err != nil {
			m.setStatus = err.Error()
			return m
		}
		keyfile := m.formValue("keyfile")
		if keyfile != "" {
			if keyfile, err = config.AbsPath(keyfile); err != nil {
				m.setStatus = err.Error()
				return m
			}
		}
		verb, err = m.writeProfile(name, func(overwrite bool) (string, error) {
			return config.WriteKdbxProfile(m.configPath, name, path, keyfile, overwrite)
		})
	}
	if err != nil {
		m.setStatus = err.Error()
		return m
	}

	m.setStatus = verb + " " + name
	m.setMode = setList
	profs := m.sources()
	if m.setSel >= len(profs) {
		m.setSel = max(0, len(profs)-1)
	}
	m.setKeyring = keyringStatus(profs)
	return m
}

// writeProfile applies the write, handling a rename on edit and a name clash on
// add. write is called with the overwrite flag.
func (m Model) writeProfile(name string, write func(overwrite bool) (string, error)) (string, error) {
	if m.formEditing {
		if name != m.formOrig {
			// rename: drop the old block, then add under the new name
			if exists, _ := config.ProfileExists(m.configPath, name); exists {
				return "", config.ErrProfileExists
			}
			if _, err := config.RemoveProfile(m.configPath, m.formOrig); err != nil {
				return "", err
			}
			return write(false)
		}
		return write(true)
	}
	return write(false)
}

// formView renders the add/edit overlay.
func (m Model) formView() string {
	title := "Add source"
	if m.formEditing {
		title = "Edit source " + m.formValue("name")
	}
	lines := []string{theme.Brand.Render(title), ""}

	labelW := 10
	for i, f := range m.form {
		sel := i == m.formFocus
		label := theme.Dimmed.Render(pad(f.label, labelW))
		if sel {
			label = theme.Hi.Render(pad(f.label, labelW))
		}
		var val string
		if f.toggle {
			kdbx, pps := theme.Faded.Render("kdbx"), theme.Faded.Render("pps")
			if m.formPps {
				pps = theme.Acc.Render("[pps]")
			} else {
				kdbx = theme.Acc.Render("[kdbx]")
			}
			val = kdbx + "  " + pps + theme.Faded.Render("   ←/→ to change")
		} else {
			val = f.input.View()
		}
		cursor := "  "
		if sel {
			cursor = theme.Acc.Render("▸ ")
		}
		lines = append(lines, cursor+label+"  "+val)
	}

	// Save button
	lines = append(lines, "", "  "+button("Save", false, m.formFocus == len(m.form)))

	foot := "↑↓/tab move · ↵ save · esc cancel"
	if m.setStatus != "" {
		foot = m.setStatus + "   ·   " + foot
	}
	lines = append(lines, "", theme.Faded.Render(trunc(foot, m.w)))

	for len(lines) < m.h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
