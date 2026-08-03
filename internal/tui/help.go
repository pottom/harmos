package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/pottom/harmos/internal/edit"
	"strings"

	"github.com/pottom/harmos/internal/theme"
)

// The help overlay is a full-screen, two-pane reference in the same frame as the
// Vault/Generate/Settings tabs: a compact key list for the active tab on the
// left, and a scrollable search manual on the right (the query language deserves
// worked examples, not a line).

// helpLeftW is the width of the key-list pane; the search manual takes the rest.
//
// The cap used to be 48 whatever the terminal was, which left every description
// truncated to about thirty characters at 100, 140 and 200 columns alike — so
// "delete → bin / permanently (again undoes)" read as "delete → bin /
// permanently (a…", and the clause that was cut is the reassurance. The manual
// beside it wants room too, so the pane takes a share rather than a constant,
// with a ceiling that leaves the manual at least half a wide screen.
func helpLeftW(w int) int { return min(max(48, w/2), max(28, w*9/20)) }

// helpViewport is the width and visible-line count of the (scrollable) search
// manual pane, so scroll bounds match what is drawn.
func (m Model) helpViewport() (w, visible int) {
	panelsH := max(3, m.h-3) // header line + a context line + the footer
	return m.w - helpLeftW(m.w) - 1, max(1, panelsH-2)
}

// helpTotal is the line count of the taller pane — the scroll extent shared by
// both panes. Line counts are width-independent, so any width gives the count.
func (m Model) helpTotal() int {
	return max(len(m.keyList(1<<30)), len(m.searchGuide(1<<30)))
}

// updateHelp scrolls the reference or closes the overlay.
func (m Model) updateHelp(key string) Model {
	_, vis := m.helpViewport()
	total := m.helpTotal()
	switch key {
	case "up", "ctrl+p", "k":
		m.helpScroll = max(0, m.helpScroll-1)
	case "down", "ctrl+n", "j":
		m.helpScroll = clampScroll(m.helpScroll+1, total, vis)
	case "pgup":
		m.helpScroll = max(0, m.helpScroll-max(1, vis-1))
	case "pgdown", " ":
		m.helpScroll = clampScroll(m.helpScroll+max(1, vis-1), total, vis)
	case "home", "g":
		m.helpScroll = 0
	case "end", "G":
		m.helpScroll = clampScroll(total, total, vis)
	default: // esc, ?, q, enter, 1, 2, … → close
		m.help = false
		m.helpScroll = 0
	}
	return m
}

func (m Model) helpView() string {
	panelsH := max(3, m.h-3)
	leftW := helpLeftW(m.w)
	rightW := m.w - leftW - 1

	keys := m.keyList(leftW - 2)
	guide := m.searchGuide(rightW - 2)
	_, vis := m.helpViewport()
	// One scroll offset drives both panes; each is clamped to its own length so the
	// shorter pane simply parks at its end while the longer one keeps scrolling.
	scroll := clampScroll(m.helpScroll, m.helpTotal(), vis)
	lo := clampScroll(scroll, len(keys), vis)
	ro := clampScroll(scroll, len(guide), vis)

	left := boxV("Keys", "", keys[lo:min(lo+vis, len(keys))], leftW, panelsH, false, len(keys), lo, 0)
	info := ""
	if m.helpTotal() > vis {
		info = "scroll ↑↓"
	}
	right := boxV("Search", info, guide[ro:min(ro+vis, len(guide))],
		rightW, panelsH, true, len(guide), ro, 0)

	// The indicator belongs to the footer, as on every other surface — this was
	// the one place it sat in the header, which is also what pushed the header
	// past the edge on a narrow terminal.
	header := trunc(brand()+theme.Dimmed.Render("  ·  help"), m.w)
	footer := m.footer(theme.Faded.Render("↑↓/jk scroll · PgUp/PgDn page · g/G top/bottom · esc closes"))
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	// A context line, present whether or not it has anything to say — every tab
	// has one, and without it the help's frame sat a row lower than theirs, so
	// pressing ? moved the whole panel down and back.
	ctx := theme.Faded.Render(trunc("  press ? again, or esc, to go back", m.w))
	return header + "\n" + panels + "\n" + ctx + "\n" + footer
}

// keyList is the left pane: the key bindings for the active tab, grouped.
func (m Model) keyList(w int) []string {
	kw := 9
	head := func(s string) string { return "  " + theme.Acc.Render(s) }
	row := func(k, d string) string {
		return "  " + theme.Strong.Render(pad(k, kw)) + theme.Dimmed.Render(trunc(d, max(4, w-4-kw)))
	}

	var out []string
	switch m.tab {
	case 1:
		out = append(out,
			head("CATEGORIES"),
			row("↑↓", "move between categories"),
			row("→ ↵ ⇥", "open the selected one"),
			row("t / i", "straight to Theme / Icons"),
			row("PgUp/Dn", "first / last category"),
			"",
			head("SOURCES"),
			row("a / e", "add / edit a source"),
			row("s", "sync a Pleasant source"),
			row("p / x", "save / clear a password"),
			row("d", "remove a source"),
			row("← esc ⇥", "back to the categories"),
			"",
			head("THEME · ICONS · PREFS"),
			row("↑↓", "preview a theme live"),
			row("↵", "save the previewed theme"),
			row("space", "toggle Nerd Font icons"),
			row("←/→ -/+", "adjust a preference"),
			row("esc ⇥", "leave (an unsaved theme reverts)"),
		)
	case tabChanges:
		out = append(out,
			head("REVIEW"),
			row("↑↓", "move between changes"),
			row("PgUp/Dn", "a page at a time"),
			row("home/end", "top / bottom · wheel scrolls"),
			row("↵", "go to it in the vault"),
			"",
			head("FOLD  (as in the tree)"),
			row("z / Z", "fold this / every change"),
			row("← →", "close / open one step"),
			row("⇧← ⇧→", "close / open a whole folder"),
			"",
			head("STAGED WORK"),
			row("x", "revert this change, or a folder's"),
			row("^s", "review and write everything"),
			row("1", "back to the vault"),
		)
	case 2:
		out = append(out,
			head("GENERATE — OPTIONS"),
			row("↑↓ jk", "move between options"),
			row("space", "toggle a class / option"),
			row("←/→ -/+", "adjust the length"),
			row("↵ ⇥ g", "jump to the password"),
			"",
			head("GENERATE — PASSWORD"),
			row("↑↓ jk", "browse recent rolls"),
			row("↵ ^y c", "copy the password"),
			row("r g space", "reroll a new one"),
			row("esc ⇥ ←", "back to options"),
			row("click", "pick recent · dbl/right copies"),
		)
	default:
		out = append(out,
			head("NAVIGATE"),
			row("↑↓", "move: tree · table · results"),
			row("⇥", "tree ⇄ table"),
			row("→", "expand a folder · open an entry"),
			row("←", "collapse · back to the tree"),
			row("z / Z", "fold this branch / every folder"),
			row("⇧← ⇧→", "fold shut / open, where ⇧ survives"),
			row("↵", "expand a folder · copy password"),
			row("^b", "hide / show the folder tree"),
			row("^w", "unlock / lock this source for writing"),
			row("/", "search every source"),
			row("PgUp/Dn", "page any list"),
			"",
			head("SEARCH RESULTS"),
			row("↑↓", "move through the hits"),
			row("→ ⇥", "open the entry details"),
			row("↵", "copy the password"),
			row("g", "jump to the entry's folder"),
			row("esc", "clear the results"),
			"",
			head("STAGED MARKS"),
			row(markerFor(edit.New), "new — will be created"),
			row(markerFor(edit.Modified), "changed"),
			row(markerFor(edit.Moved), "moved to another folder"),
			row(markerFor(edit.Deleted), "going to the recycle bin"),
			row(markerFor(edit.Purged), "gone for good — no bin, no undo"),
			row("", "on a folder: something inside it, not the folder"),
			"",
			head("EDIT  (unlocked sources)"),
			row("e", "edit the entry — the whole form"),
			row("r", "rename in place: a folder, or an entry's title"),
			row("n / N", "new entry (a form) / new folder (on its row)"),
			row("d / D", "delete → bin / permanently (again undoes)"),
			row("m", "pick it up; steer with the tree, ↵ drops it"),
			row("^g", "roll a password, in the editor"),
			row("^s", "review and write the staged changes"),
			"",
			head("ANY SELECTED ENTRY"),
			row("^y ^u", "copy password · username"),
			row("^o ^t", "copy URL · TOTP code"),
			row("c", "copy a harmos get command"),
			"",
			head("ENTRY DETAILS"),
			row("^r", "reveal the password"),
			row("s", "save attachments"),
			row("↑↓", "scroll the pane"),
			row("esc ←", "back"),
		)
	}
	out = append(out,
		"",
		head("GENERAL"),
		row("1 2 3 4", tabNames()),
		row("? q", "help · quit (clears the clipboard)"),
		row("^p ^n", "up / down in a list"),
	)

	if len(m.excluded) > 0 {
		out = append(out, "", "  "+theme.Bad.Render("UNAVAILABLE"))
		for _, ex := range m.excluded {
			out = append(out, "  "+theme.Strong.Render(pad(trunc(ex.Source, kw), kw))+theme.Faded.Render(trunc(ex.Reason, max(4, w-4-kw))))
		}
	}
	return out
}

// searchGuide is the right pane: the query language, with worked examples. w is
// the inner width, used to fit and wrap the longer lines.
func (m Model) searchGuide(w int) []string {
	qw := 16
	head := func(s string) string { return "  " + theme.Acc.Render(s) }
	ex := func(q, d string) string {
		return "  " + theme.Hi.Render(pad(q, qw)) + " " + theme.Dimmed.Render(trunc(d, max(4, w-4-qw)))
	}
	note := func(s string) string { return "  " + theme.Faded.Render(trunc(s, max(4, w-2))) }

	return []string{
		note("Press / to search every source. esc clears it."),
		note("AND is a space, OR is |, NOT is a leading -."),
		"",
		head("BASICS"),
		ex("db", "match “db” anywhere"),
		ex("db prod", "AND — both terms must match"),
		ex("db | web", "OR — either side matches"),
		ex(`"db prod"`, "an exact phrase (spaces kept)"),
		"",
		head("SCOPE TO A FIELD"),
		ex("title:db", "the title only"),
		ex("user:svc", "the username"),
		ex("url:vault", "the URL"),
		ex("path:infra", "the folder path"),
		ex("tag:prod", "the tags"),
		ex("notes:rotated", "the notes"),
		ex("field:token", "any custom field"),
		ex("file:id.ppk", "an attachment file name"),
		ex("src:own", "one source only (also source:)"),
		"",
		head("EXCLUDE  (-)"),
		ex("db -stage", "has “db”, not “stage”"),
		ex("db -user:root", "“db”, but not user root"),
		"",
		head("COMBINE"),
		"  " + theme.Hi.Render("url:vault prod | web"),
		note("   → (url:vault AND prod) OR web"),
		"",
		note("Matches are highlighted in the results, the"),
		note("breadcrumb and the detail pane; a badge on each"),
		note("result shows which field matched."),
		note("Custom fields match by name always, by value"),
		note("only when the value is not a protected secret."),
	}
}

// markerFor is a staged state's glyph, for the legend. The marks are the whole
// visual language of a staged session and they were defined nowhere: a reader
// who deleted one entry saw its folder, and its folder's folder, take the same
// "-" and reasonably read it as "these are being deleted too".
func markerFor(st edit.State) string {
	_, m := changeStyle(st)
	return strings.TrimSpace(m)
}
