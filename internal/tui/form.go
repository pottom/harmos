package tui

import (
	"errors"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
)

// The Settings tab's add/edit-source form, built on the reusable component in
// formkit.go. It used to carry its own focus arithmetic and rendering; those now
// live in one place, so the entry editor does not have to repeat them.

// buildSourceForm assembles the fields for a source type. On add, a Type toggle
// leads and switching it rebuilds the field list; on edit the type is fixed.
func buildSourceForm(isPps, editing bool, pref map[string]string, width int) form {
	get := func(k string) string {
		if pref == nil {
			return ""
		}
		return pref[k]
	}
	required := func(what string) func(string) error {
		return func(v string) error {
			if v == "" {
				return errors.New(what + " is required")
			}
			return nil
		}
	}

	var fields []field
	if !editing {
		choice := 0
		if isPps {
			choice = 1
		}
		fields = append(fields, toggleField("type", "Type", []string{"kdbx", "pps"}, choice).
			withToggleHandler(func(f form, c int) form {
				// The type decides which fields exist, so switching it rebuilds
				// the list — carrying the name across, since it applies to both.
				next := buildSourceForm(c == 1, false, map[string]string{"name": f.Value("name")}, f.width)
				next.focus = f.focus
				return next.refocus()
			}))
	}
	fields = append(fields, textField("name", "Name", "e.g. work", get("name")).
		withValidation(required("name")))

	if isPps {
		fields = append(fields,
			textField("url", "URL", "https://pps.example:10001", get("url")).withValidation(required("url")),
			textField("user", "User", "service account", get("user")).withValidation(required("user")),
			textField("cache", "Cache", "default: $XDG_DATA_HOME/harmos/<name>.kdbx", get("cache")),
			textField("ca", "CA bundle", "optional CA bundle", get("ca")),
		)
	} else {
		fields = append(fields,
			textField("path", "Path", "/path/to/vault.kdbx", get("path")).withValidation(required("path")),
			textField("keyfile", "Keyfile", "optional key file", get("keyfile")),
		)
	}
	return newForm("Save", width, fields...)
}

func (m Model) formWidth() int { return max(20, m.w-6) }

func (m Model) openAddForm() Model {
	m.setMode = setForm
	m.setStatusBad, m.setStatus = false, ""
	m.formEditing, m.formOrig, m.formPps = false, "", false
	m.form = buildSourceForm(false, false, nil, m.formWidth())
	return m
}

func (m Model) openEditForm(p config.Source) Model {
	m.setMode = setForm
	m.setStatusBad, m.setStatus = false, ""
	m.formEditing, m.formOrig = true, p.Name
	m.formPps = p.Type == config.Pleasant
	m.form = buildSourceForm(m.formPps, true, map[string]string{
		"name": p.Name, "path": p.Path, "keyfile": p.Keyfile,
		"url": p.URL, "user": p.User, "cache": p.Cache, "ca": p.CABundle,
	}, m.formWidth())
	return m
}

func (m Model) updateForm(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key == "esc" {
		m.setMode = setList
		return m, nil
	}

	f, cmd, submitted := m.form.Update(key, msg)
	m.form = f
	// The toggle can change which type is selected, and everything downstream
	// reads that rather than the field.
	m.formPps = m.form.Choice("type") == 1
	if !submitted {
		return m, cmd
	}

	f, ok := m.form.Validate()
	m.form = f
	if !ok {
		return m, cmd
	}
	return m.submitForm(), cmd
}

// submitForm writes the source and returns to the list.
func (m Model) submitForm() Model {
	name := m.form.Value("name")

	var verb string
	var err error
	if m.formPps {
		cache := m.form.Value("cache")
		if cache == "" {
			if cache, err = config.DefaultCachePath(name); err != nil {
				m.form = m.form.withStatus(err.Error())
				return m
			}
		}
		if cache, err = config.AbsPath(cache); err != nil {
			m.form = m.form.withStatus(err.Error())
			return m
		}
		_ = os.MkdirAll(filepath.Dir(cache), 0o700)

		ca := m.form.Value("ca")
		if ca != "" {
			if ca, err = config.AbsPath(ca); err != nil {
				m.form = m.form.withStatus(err.Error())
				return m
			}
		}
		url, user := m.form.Value("url"), m.form.Value("user")
		verb, err = m.writeSource(name, func(overwrite bool) (string, error) {
			return config.WritePleasantSource(m.configPath, name, url, user, cache, ca, overwrite)
		})
	} else {
		path, perr := config.AbsPath(m.form.Value("path"))
		if perr != nil {
			m.form = m.form.withStatus(perr.Error())
			return m
		}
		keyfile := m.form.Value("keyfile")
		if keyfile != "" {
			if keyfile, err = config.AbsPath(keyfile); err != nil {
				m.form = m.form.withStatus(err.Error())
				return m
			}
		}
		verb, err = m.writeSource(name, func(overwrite bool) (string, error) {
			return config.WriteKdbxSource(m.configPath, name, path, keyfile, overwrite)
		})
	}
	if err != nil {
		m.form = m.form.withStatus(err.Error())
		return m
	}

	m.setStatusBad, m.setStatus = false, verb+" "+name
	if m.onboarding {
		// The first source is added and the vault is still empty, because the
		// sources were opened before this one existed. Nothing on any screen
		// said so, and the word "restart" appeared nowhere in the program — a
		// first-time user concluded harmos could not read their vault.
		m.setStatusBad, m.setStatus = false, verb+" "+name+" — restart harmos to open it"
		m.onboarding = false
	}
	m.setMode = setList
	profs := m.sources()
	if m.setSel >= len(profs) {
		m.setSel = max(0, len(profs)-1)
	}
	m.setKeyring = keyringStatus(profs)
	return m
}

// writeSource applies the write, handling a rename on edit and a name clash on
// add. write is called with the overwrite flag.
func (m Model) writeSource(name string, write func(overwrite bool) (string, error)) (string, error) {
	if m.formEditing {
		if name != m.formOrig {
			// rename: drop the old block, then add under the new name
			if exists, _ := config.SourceExists(m.configPath, name); exists {
				return "", config.ErrSourceExists
			}
			if _, err := config.RemoveSource(m.configPath, m.formOrig); err != nil {
				return "", err
			}
			return write(false)
		}
		return write(true)
	}
	return write(false)
}

// formView renders the add/edit overlay inside a titled box.
func (m Model) formView() string {
	title := "Add source"
	info := "kdbx"
	if m.formPps {
		info = "pps"
	}
	if m.formEditing {
		title, info = "Edit source", m.formOrig
	}
	h := max(3, m.h-1)
	lines, at := m.form.View()
	visible := max(1, h-2)
	// Stateless: the focus is the only thing that scrolls a form, so the
	// offset is derived from it rather than remembered. Computing from the top
	// each frame keeps the focused field on screen with the least scrolling
	// that achieves it.
	scroll := scrollToShow(0, at, visible, len(lines))
	return boxV(title, info, lines[scroll:min(scroll+visible, len(lines))],
		m.w, h, true, len(lines), scroll, 0) + "\n" + m.footer(m.form.Hint())
}
