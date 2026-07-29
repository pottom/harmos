package tui

import (
	"strings"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

// The per-source write lock.
//
// A source is read-only until somebody says otherwise, and that answer is
// remembered in the config as `writable = true`. It started out as a per-run
// unlock with nothing persisted, which is the safer default on paper and turned
// out to be a nuisance in practice: anyone who edits regularly re-answers the
// same question every launch, and a prompt answered by reflex has stopped being
// a safeguard.
//
// The default is still read-only. A config that has never heard of the feature
// behaves exactly as it did before, and turning it on is always something
// somebody did on purpose — it just stays done.
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
		m.flash = m.persistWritable(source, false, "locked "+source)
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
	m.confirmSel = 0 // Unlock is the default: it is what the user came for
	return m
}

// updateWriteConfirm handles the unlock confirmation.
func (m Model) updateWriteConfirm(key string) Model {
	if sel, moved := confirmNav(key, m.confirmSel, len(unlockChoices())); moved {
		m.confirmSel = sel
		return m
	}

	unlock := false
	switch key {
	case "enter":
		unlock = m.confirmSel == 0
	case "y", "Y":
		unlock = true // the shortcut still works wherever the cursor is
	case "n", "N", "esc":
	default:
		return m // an unrelated key does not dismiss a confirmation
	}

	if unlock {
		if m.writeOK == nil {
			m.writeOK = map[string]bool{}
		}
		m.writeOK[m.confirmUnlock] = true
		m.flash = m.persistWritable(m.confirmUnlock, true, "unlocked "+m.confirmUnlock)
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
		"  " + theme.Faded.Render("Nothing is written until you save."),
		"  " + theme.Faded.Render("This is remembered, so you are not asked again."),
		"",
	}
	lines = append(lines, confirmButtons(unlockChoices(), m.confirmSel, "←/→ choose · ↵ confirm · esc cancel")...)
	return m.modal("Write lock", source, lines, "")
}

// lockBadge is the write state shown beside a source in the tree.
//
// Three states, not two, and every one of them is shown. An absent badge was the
// original design for a source that can never be written, on the grounds that a
// padlock implies something that can be opened — but absence is the more
// ambiguous signal. It reads equally as "read-only", "not loaded yet" and
// "somebody forgot to draw it". A source that is permanently read-only should
// say so, since that is a fact about it and not a missing feature.
//
// The word carries the meaning, not the glyph: a padlock only tells you which
// way round it is if you already know the convention, and colour is gone under
// NO_COLOR, in a mono terminal, and for a good share of readers.
func (m Model) lockBadge(source string) string {
	if source == "" {
		return ""
	}
	i := ic()

	// Permanently read-only: a Pleasant cache is rebuilt by sync, so there is no
	// unlock to offer. Say so rather than staying silent.
	if _, ok := m.handles[source]; !ok {
		return theme.Faded.Render(i.locked + " ro fixed")
	}
	if ok, _ := m.canWrite(source); !ok {
		// Openable in principle, refused for this file — the format, or the
		// file itself. ^w says which.
		return theme.Faded.Render(i.locked + " ro fixed")
	}
	if m.writeOK[source] {
		return theme.Editable.Render(i.unlocked + " rw")
	}
	return theme.Faded.Render(i.locked + " ro")
}

// lockBadgeWidth is the badge's display width, so a row can reserve space for it
// instead of overflowing the pane — the badge used to be appended after the
// truncation, which is exactly how a column ends up one cell too wide.
func (m Model) lockBadgeWidth(source string) int {
	if b := m.lockBadge(source); b != "" {
		return dw(b) + 2 // the two spaces that separate it from the name
	}
	return 0
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
//
// The frame is the other tabs' frame, to the row: header, panel, a context line,
// then the footer — panelsH is m.h-3 like everywhere else, so the bottom border
// does not jump when switching tabs. The tab indicator belongs to the footer
// alone; the header's right-hand slot carries a note about this tab, the way
// Generate's carries "crypto/rand".
func (m Model) changesView() string {
	panelsH := max(3, m.h-3)
	visible := max(1, panelsH-2)

	// Measure with the same width the box will hand back, scrollbar and all —
	// rows built a column too wide are rows drawn over the border.
	rows := m.changeRows(m.w - 2)
	inner := m.w - 2 - boolToInt(len(rows) > visible)
	rows = m.changeRows(inner)

	start := clampScroll(m.chgScroll, len(rows), visible)
	body := m.renderChangeRows(rows, inner, start, visible)
	if len(rows) == 0 {
		body = m.changesPlaceholder()
	}

	note := ""
	if m.dirtyCount() > 0 {
		note = theme.Noted.Render("unsaved")
	}
	header := spread(m.brandVersion()+theme.Faded.Render("  ·  changes"), note, m.w)

	// The context line is present whether or not it has anything to say, so the
	// geometry never depends on the content.
	ctx := ""
	if m.remaining > 0 {
		ctx = m.countdown()
	} else if n := m.permanentlyRemoved(); n > 0 {
		// The tally on the border says how much. This line is kept for the part
		// of it that no backup inside the file can undo.
		ctx = trunc(" "+theme.Bad.Render(plural(n, "thing", "things")+" would be deleted permanently")+
			theme.Faded.Render("  ·  ^s to review and write"), m.w)
	} else if m.dirtyCount() > 0 {
		ctx = trunc(" "+theme.Faded.Render("^s to review and write"), m.w)
	}

	return header + "\n" +
		boxV("Changes", m.changesPanelInfo(), body, m.w, panelsH, true, len(rows), start, 0) + "\n" +
		ctx + "\n" +
		m.footer(theme.Faded.Render("↑↓ move · PgUp/Dn page · z/Z fold · x revert · ↵ go to it · ^s write"))
}

// persistWritable records the choice in the config and returns the message to
// show — including when it could not be saved, since a setting that silently
// failed to stick is worse than one that was never offered.
func (m Model) persistWritable(source string, on bool, ok string) string {
	if m.configPath == "" {
		return ok // no config to write to (tests, and the onboarding flow)
	}
	if err := config.SetSourceWritable(m.configPath, source, on); err != nil {
		return ok + ", but it could not be saved: " + err.Error()
	}
	return ok
}

// writableFromConfig is the set of sources the config says may be edited.
//
// Read at startup, so the answer given last time still holds. A source whose
// file harmos will not write is dropped here rather than being shown as unlocked
// and then refusing at the first edit.
func writableFromConfig(sources []config.Source, handles map[string]*vault.Handle) map[string]bool {
	out := map[string]bool{}
	for _, p := range sources {
		if !p.Writable {
			continue
		}
		if h := handles[p.Name]; h != nil {
			if ok, _ := h.Writable(); ok {
				out[p.Name] = true
			}
		}
	}
	return out
}
