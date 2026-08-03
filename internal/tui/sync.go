package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/source/pleasant"
	"github.com/pottom/harmos/internal/theme"
)

type syncProgressMsg struct {
	phase       string
	done, total int64
}

type syncDoneMsg struct {
	err     error
	summary string
}

// startSync kicks off a Pleasant sync for p, streaming progress through a channel
// so the UI stays responsive. Credentials must already be saved (env/keyring) —
// the master (encrypts the cache) and the server password (logs in); otherwise it
// sets a status telling the user to save them first (press p).
func (m Model) startSync(p config.Source) (Model, tea.Cmd) {
	if p.Type != config.Pleasant {
		m.setStatusBad, m.setStatus = false, p.Name+" is a kdbx source — nothing to sync"
		return m, nil
	}
	master, ok := resolveMasterForTUI()
	if !ok {
		m.setStatusBad, m.setStatus = true, "no master saved — press p to save it, then sync"
		return m, nil
	}
	server, ok, _ := keyring.FetchServer(p.Name)
	if !ok {
		m.setStatusBad, m.setStatus = false, "no server password saved — press p to save it, then sync"
		return m, nil
	}

	ch := make(chan syncProgressMsg, 32)
	m.syncCh = ch
	m.syncName = p.Name
	m.syncPhase = "starting"
	m.syncDone, m.syncTotal = 0, -1
	m.setMode = setSyncing
	m.setStatusBad, m.setStatus = false, ""

	run := func() tea.Msg {
		hc, err := pleasant.NewHTTPClient(p.CABundle)
		if err != nil {
			close(ch)
			return syncDoneMsg{err: err}
		}
		c := pleasant.New(p.URL, pleasant.WithHTTPClient(hc))
		if err := c.Login(context.Background(), p.User, server); err != nil {
			close(ch)
			return syncDoneMsg{err: err}
		}
		keyfile, err := config.DefaultCacheKeyfilePath(p.Name)
		if err == nil {
			err = config.EnsureKeyfile(keyfile)
		}
		if err != nil {
			close(ch)
			return syncDoneMsg{err: err}
		}
		rep := &pleasant.Reporter{
			Phase: func(name string) { ch <- syncProgressMsg{phase: name, total: -1} },
			Bytes: func(d, t int64) {
				ch <- syncProgressMsg{phase: "downloading offline package", done: d, total: t}
			},
		}
		res, err := pleasant.Sync(context.Background(), c, p.URL, pleasant.SyncOptions{
			Comment: "harmos sync (tui)", CachePath: p.Cache, Master: master, Keyfile: keyfile, Report: rep,
		})
		close(ch)
		if err != nil {
			return syncDoneMsg{err: err}
		}
		return syncDoneMsg{summary: fmt.Sprintf("synced %s: %d entries, %d folders, %d attachments",
			p.Name, res.Entries, res.Folders, res.Attachments)}
	}
	return m, tea.Batch(run, listenSync(ch))
}

// resolveMasterForTUI gets the master from HARMOS_MASTER or the keyring (no
// prompt — the TUI unlock/prompt flow is separate).
func resolveMasterForTUI() (secret.Secret, bool) {
	if v, ok := os.LookupEnv("HARMOS_MASTER"); ok && v != "" {
		return secret.New(v), true
	}
	pw, ok, _ := keyring.FetchMaster()
	return pw, ok
}

// listenSync waits for the next progress message; a closed channel ends the loop.
func listenSync(ch chan syncProgressMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m Model) syncView() string {
	i := ic()
	lines := []string{
		"  " + theme.Acc.Render(i.pps) + "  " + theme.Strong.Render(m.syncName),
		"",
		"  " + theme.Ok.Render("●") + " " + theme.Dimmed.Render(m.syncPhase+"…"),
	}
	if m.syncDone > 0 || m.syncTotal > 0 {
		lines = append(lines, "", "  "+m.progressBar())
	}
	return m.modal("harmos", "sync", lines, "please wait — this can take a while")
}

func (m Model) progressBar() string {
	if m.syncTotal > 0 {
		pct := min(m.syncDone*100/m.syncTotal, 100)
		w := max(10, min(40, m.w-30))
		filled := int(int64(w) * pct / 100)
		bar := theme.Acc.Render(strings.Repeat("█", filled)) + theme.Faded.Render(strings.Repeat("░", w-filled))
		return fmt.Sprintf("%s  %s / %s (%d%%)", bar, humanBytes(m.syncDone), humanBytes(m.syncTotal), pct)
	}
	return humanBytes(m.syncDone)
}

// humanBytes formats a byte count (KiB/MiB/…), matching the CLI.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
