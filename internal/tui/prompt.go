package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/theme"
)

// promptStep is one password to collect and where it belongs in the keyring.
type promptStep struct {
	label string
	kind  string // master, server, kdbx
}

// openSavePassword starts collecting the selected source's password(s): for a
// Pleasant source the shared master (only when not already saved) and the server
// password; for a kdbx source its file password.
func (m Model) openSavePassword(p config.Source) Model {
	m.setMode = setPrompt
	m.setStatusBad, m.setStatus = false, ""
	m.promptName = p.Name
	var q []promptStep
	if p.Type == config.Pleasant {
		if _, ok, _ := keyring.FetchMaster(); !ok {
			q = append(q, promptStep{"harmos master password", "master"})
		}
		q = append(q, promptStep{"server password for " + p.User, "server"})
	} else {
		q = append(q, promptStep{"password for " + p.Name, "kdbx"})
	}
	m.promptQueue = q
	return m.startPrompt()
}

func (m Model) startPrompt() Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Width = 40
	ti.Focus()
	m.promptInput = ti
	return m
}

func (m Model) updatePrompt(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.setMode = setList
		m.setStatusBad, m.setStatus = false, "save-password cancelled"
		return m, nil
	case "enter":
		if len(m.promptQueue) == 0 {
			m.setMode = setList
			return m, nil
		}
		step := m.promptQueue[0]
		pw := secret.New(m.promptInput.Value())
		var err error
		switch step.kind {
		case "master":
			err = keyring.StoreMaster(pw)
		case "server":
			err = keyring.StoreServer(m.promptName, pw)
		default:
			err = keyring.Store(m.promptName, pw)
		}
		if err != nil {
			m.setStatusBad, m.setStatus = true, "could not save: "+err.Error()
			m.setMode = setList
			return m, nil
		}
		m.promptQueue = m.promptQueue[1:]
		if len(m.promptQueue) > 0 {
			return m.startPrompt(), nil
		}
		m.setStatusBad, m.setStatus = false, "saved "+m.promptName+"'s password"
		m.setKeyring = keyringStatus(m.sources())
		m.setMode = setList
		return m, nil
	}
	var cmd tea.Cmd
	m.promptInput, cmd = m.promptInput.Update(msg)
	return m, cmd
}

func (m Model) promptView() string {
	i := ic()
	label := ""
	if len(m.promptQueue) > 0 {
		label = m.promptQueue[0].label
	}
	lines := []string{
		"  " + theme.Faded.Render("saved in your OS keyring · "+m.promptName),
		"",
		"  " + theme.Hi.Render(i.saved) + "  " + theme.Strong.Render(label),
		"  " + m.promptInput.View(),
	}
	if len(m.promptQueue) > 1 {
		lines = append(lines, "", "  "+theme.Faded.Render(fmt.Sprintf("%d more to enter", len(m.promptQueue)-1)))
	}
	return m.modal("harmos", "save password", lines, "↵ save · esc cancel")
}
