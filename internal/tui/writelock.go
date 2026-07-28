package tui

import (
	"strings"

	"github.com/pottom/harmos/internal/theme"
)

// The per-source write lock.
//
// Every source starts locked on every run. Nothing is persisted: a vault is
// editable only because the user said so, in this session, on purpose. That is
// the whole safety story for a program whose previous version could not write at
// all — the default is the old behaviour, and leaving it is a deliberate act.
//
// It is a separate concept from m.locked, which means the unlock phase (the
// password prompts at startup). Two things called "locked" in one model would be
// a bug waiting to happen, so this one is writeOK.

// writeUnlocked reports whether the user has unlocked a source for writing.
func (m Model) writeUnlocked(source string) bool { return m.writeOK[source] }

// writeCandidate is the source the lock keys act on: the one the cursor is in.
func (m Model) writeCandidate() string {
	if e := m.selEntry(); e != nil {
		return e.Source
	}
	flat := m.visible()
	if m.tsel < len(flat) {
		return flat[m.tsel].node.source
	}
	return ""
}

// canWrite reports whether a source could be written at all, and if not, why.
//
// Two different refusals, and the difference matters to the user: a source
// harmos declines to write (a format it cannot round-trip, a file it cannot
// replace) is a dead end, whereas a locked one is a keystroke away.
func (m Model) canWrite(source string) (bool, string) {
	h := m.handles[source]
	if h == nil {
		return false, "this source cannot be written — " + m.whyNoHandle(source)
	}
	return h.Writable()
}

func (m Model) whyNoHandle(source string) string {
	if m.srcType[source] == "pleasant" {
		return "a Pleasant cache is rebuilt by sync, so an edit would be lost"
	}
	return "it was not opened for writing"
}

// toggleWriteLock asks to unlock, or locks again immediately.
//
// Locking needs no confirmation — it only ever takes a capability away. Unlocking
// asks, because it is the moment the program stops being read-only.
func (m Model) toggleWriteLock() Model {
	source := m.writeCandidate()
	if source == "" {
		return m
	}
	if m.writeOK[source] {
		delete(m.writeOK, source)
		m.flash = "locked " + source
		return m
	}
	// Two questions, in order. First the cheap one — is there a handle at all,
	// and did the format checks pass — which also covers a source that was never
	// opened for writing.
	if ok, why := m.canWrite(source); !ok {
		m.flash = why
		return m
	}
	// Then the expensive proof, once per source, at the moment its answer
	// matters. At startup every session would have paid for it.
	if ok, why := m.handles[source].VerifyWritable(); !ok {
		m.flash = why
		return m
	}
	m.confirmUnlock = source
	return m
}

// updateWriteConfirm handles the unlock confirmation.
func (m Model) updateWriteConfirm(key string) Model {
	switch key {
	case "y", "Y", "enter":
		if m.writeOK == nil {
			m.writeOK = map[string]bool{}
		}
		m.writeOK[m.confirmUnlock] = true
		m.flash = "unlocked " + m.confirmUnlock + " for this run"
	}
	m.confirmUnlock = ""
	return m
}

// writeConfirmView asks before the program stops being read-only.
func (m Model) writeConfirmView() string {
	source := m.confirmUnlock
	path := source
	if h := m.handles[source]; h != nil {
		path = h.Path()
	}

	lines := []string{
		"",
		"  " + theme.Strong.Render("Unlock "+source+" for editing?"),
		"",
		"  " + theme.Dimmed.Render(trunc(path, max(10, m.w-8))),
		"",
		"  " + theme.Faded.Render("Nothing is written until you save, and the unlock"),
		"  " + theme.Faded.Render("lasts for this run only — it is not remembered."),
		"",
	}
	return m.modal("Write lock", source, lines, "y unlock · n cancel")
}

// lockBadge is the padlock shown beside a source in the tree.
//
// The word beside the glyph is not decoration: a padlock is only meaningful if
// you already know which way round it is, and colour is not available in a mono
// terminal or under NO_COLOR.
func (m Model) lockBadge(source string) string {
	if source == "" {
		return ""
	}
	// Writability first. A source harmos cannot write shows no padlock at all —
	// one would imply it could be opened — and that has to hold even if some
	// path managed to mark it unlocked, which the badge should never paper over.
	if ok, _ := m.canWrite(source); !ok {
		return ""
	}
	i := ic()
	if m.writeOK[source] {
		return theme.Noted.Render(i.unlocked + " rw")
	}
	return theme.Faded.Render(i.locked + " ro")
}

// dirtyCount is how many effective changes are staged, for the footer and the
// quit guard.
func (m Model) dirtyCount() int { return len(m.chg.Effective()) }

// changesPlaceholder is what the Changes tab says when nothing is pending.
func (m Model) changesPlaceholder() []string {
	if len(m.handles) == 0 {
		return []string{"", "  " + theme.Faded.Render("no source in this session can be edited")}
	}
	var unlocked []string
	for src := range m.writeOK {
		unlocked = append(unlocked, src)
	}
	if len(unlocked) == 0 {
		return []string{
			"", "  " + theme.Faded.Render("nothing pending"),
			"", "  " + theme.Faded.Render("no source is unlocked for writing — ^w unlocks the one you are in"),
		}
	}
	return []string{
		"", "  " + theme.Faded.Render("nothing pending"),
		"", "  " + theme.Faded.Render("unlocked: "+strings.Join(unlocked, ", ")),
	}
}

// changesView is the Changes tab.
func (m Model) changesView() string {
	body := m.changesBody(m.w - 4)
	panelsH := max(3, m.h-2)
	header := spread(m.brandVersion()+theme.Faded.Render("  ·  changes"), m.tabIndicator(), m.w)
	return header + "\n" +
		box("Changes", m.chg.Summary(), body, m.w, panelsH, true) + "\n" +
		m.footer(theme.Faded.Render("↑↓ pick · x revert · ↵ go to it · ^s save · 1 back to the vault"))
}
