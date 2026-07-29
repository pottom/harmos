package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

// The screen audit.
//
// internal/tui/AGENTS.md opens with a checklist to run before building a
// surface. The items a machine can check are checked here, over every surface
// the program can show, so "this screen is a bit different from the others" is a
// failing test rather than something a person has to notice and report.
//
// Add a surface here when you add a surface. A screen nobody audits is a screen
// where the next inconsistency will live.

// surface is one renderable state of the program.
type surface struct {
	name string
	view func(t *testing.T) string
}

// auditW, auditH are the terminal the audit renders into. Odd numbers on
// purpose: an off-by-one in a width split shows up as a ragged line rather than
// hiding behind a round number.
const auditW, auditH = 97, 29

// auditSizes are the shapes every surface is checked at. The narrow one is the
// declared minimum (view.go: below 40×10 the program says so instead), and the
// short one is what caught a save confirmation rendering 23 rows on a 20-row
// terminal — with its own irreversibility warning among the rows that scrolled
// off the top.
var auditSizes = [][2]int{{auditW, auditH}, {41, 11}, {60, 20}, {140, 30}}

func auditSurfaces() []surface { return auditSurfacesAt(auditW, auditH) }

func auditSurfacesAt(width, height int) []surface {
	auditW, auditH := width, height
	sized := func(m Model) Model { return up(m, tea.WindowSizeMsg{Width: auditW, Height: auditH}) }

	base := func(t *testing.T) Model {
		t.Helper()
		return sized(New([]vault.Entry{
			{ID: "own:1", GroupID: "own:g:1", Source: "own", Path: "Infra/db", Title: "db-prod",
				Username: "svc_admin", Password: secret.New("p1"), URL: "https://db.example"},
			{ID: "own:2", GroupID: "own:g:1", Source: "own", Path: "Infra/db", Title: "db-stage",
				Username: "svc", Password: secret.New("p2")},
			{ID: "work:1", GroupID: "work:g:1", Source: "work", Path: "Net", Title: "router",
				Username: "admin", Password: secret.New("p3")},
		}, []vault.Folder{
			{ID: "own:g:1", Source: "own", Path: "Infra/db", Name: "db"},
			{ID: "work:g:1", Source: "work", Path: "Net", Name: "Net"},
		}, "", 30*time.Second))
	}

	staged := func(t *testing.T) Model {
		t.Helper()
		m := sized(editModel(t))
		m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // into the entry table
		return up(m, key2("d"))                 // stage a deletion
	}

	return []surface{
		{"vault", func(t *testing.T) string { return base(t).View() }},
		{"vault/tree-focus", func(t *testing.T) string {
			m := base(t)
			m.focus = 0
			return m.View()
		}},
		{"vault/entry-focus", func(t *testing.T) string {
			return up(base(t), tea.KeyMsg{Type: tea.KeyTab}).View()
		}},
		{"vault/detail", func(t *testing.T) string {
			m := up(base(t), tea.KeyMsg{Type: tea.KeyTab})
			return up(m, tea.KeyMsg{Type: tea.KeyRight}).View()
		}},
		{"vault/search", func(t *testing.T) string {
			m := up(base(t), key2("/"))
			return typeStr(m, "db").View()
		}},
		{"vault/tree-collapsed", func(t *testing.T) string {
			return up(base(t), tea.KeyMsg{Type: tea.KeyCtrlB}).View()
		}},
		{"generate", func(t *testing.T) string { return up(base(t), tabKey(tabGenerate)).View() }},
		{"settings", func(t *testing.T) string { return up(base(t), tabKey(tabSettings)).View() }},
		{"changes/empty", func(t *testing.T) string { return up(base(t), tabKey(tabChanges)).View() }},
		{"changes/staged", func(t *testing.T) string {
			return up(staged(t), tabKey(tabChanges)).View()
		}},
		{"changes/hunks", func(t *testing.T) string {
			m := sized(intoEditor(t, sized(editModel(t))))
			m = typeStr(m, "-edited")
			m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
			return up(m, tabKey(tabChanges)).View()
		}},
		{"changes/folded", func(t *testing.T) string {
			m := up(staged(t), tabKey(tabChanges))
			return up(m, key2("Z")).View()
		}},
		{"changes/folder-deleted", func(t *testing.T) string {
			m := sized(editModel(t))
			for i, tl := range m.visible() { // onto a folder row, not an entry
				if tl.node.id != "" {
					m.tsel, m.focus = i, 0
					break
				}
			}
			return up(up(m, key2("d")), tabKey(tabChanges)).View()
		}},
		{"changes/everything", func(t *testing.T) string { return mockChanges(t, auditW, auditH).View() }},
		{"changes/everything-folded", func(t *testing.T) string {
			return up(mockChanges(t, auditW, auditH), key2("Z")).View()
		}},
		{"confirm/save-everything", func(t *testing.T) string {
			m, _ := mockChanges(t, auditW, auditH).askToSave()
			return m.View()
		}},
		{"help", func(t *testing.T) string { return up(base(t), key2("?")).View() }},
		{"editor/entry", func(t *testing.T) string {
			return intoEditor(t, sized(editModel(t))).View()
		}},
		{"editor/folder", func(t *testing.T) string {
			return up(sized(editModel(t)), key2("N")).View()
		}},
		{"editor/move", func(t *testing.T) string {
			m := up(sized(editModel(t)), tea.KeyMsg{Type: tea.KeyTab})
			return up(m, key2("m")).View()
		}},
		{"confirm/unlock", func(t *testing.T) string {
			m := sized(editModel(t))
			m.writeOK = map[string]bool{}
			return up(m, tea.KeyMsg{Type: tea.KeyCtrlW}).View()
		}},
		{"confirm/save", func(t *testing.T) string {
			m := up(staged(t), tabKey(tabChanges))
			return up(m, tea.KeyMsg{Type: tea.KeyCtrlS}).View()
		}},
		{"confirm/quit", func(t *testing.T) string {
			m := staged(t)
			m.quitGuard = true
			return m.View()
		}},
		{"confirm/conflict", func(t *testing.T) string {
			m := staged(t)
			m.saveConflict = "own"
			return up(m, tabKey(tabChanges)).View()
		}},
	}
}

// lines splits a rendered view into plain, ANSI-free rows.
func plainLines(view string) []string {
	return strings.Split(ansi.Strip(strings.TrimSuffix(view, "\n")), "\n")
}

// A view fills its terminal exactly: no short screen, no scrolled-off row.
func TestAuditScreenHeight(t *testing.T) {
	for _, s := range auditSurfaces() {
		t.Run(s.name, func(t *testing.T) {
			if got := len(plainLines(s.view(t))); got != auditH {
				t.Errorf("%d rows, want %d — a surface that does not fill the terminal "+
					"leaves the previous screen showing through", got, auditH)
			}
		})
	}
}

// And it fits every shape the program claims to support, at both ends.
func TestAuditFitsEverySize(t *testing.T) {
	for _, size := range auditSizes {
		w, h := size[0], size[1]
		for _, s := range auditSurfacesAt(w, h) {
			t.Run(fmt.Sprintf("%dx%d/%s", w, h, s.name), func(t *testing.T) {
				rows := plainLines(s.view(t))
				if len(rows) > h {
					t.Errorf("%d rows on a %d-row terminal — the top of this screen is gone", len(rows), h)
				}
				for i, ln := range rows {
					if dw(ln) > w {
						t.Errorf("row %d is %d columns on a %d-column terminal: %q", i, dw(ln), w, ln)
					}
				}
			})
		}
	}
}

// Every row is the same width, and none overflows. Ragged rows are how "the
// padding is different here" starts.
func TestAuditScreenWidth(t *testing.T) {
	for _, s := range auditSurfaces() {
		t.Run(s.name, func(t *testing.T) {
			for i, ln := range plainLines(s.view(t)) {
				if w := dw(ln); w > auditW {
					t.Errorf("row %d is %d columns wide, terminal is %d: %q", i, w, auditW, ln)
				}
			}
		})
	}
}

// idToken is harmos's opaque identity: source, an optional :g:, then base64url.
// It is a key, not a name, and it has no business on a screen.
var idToken = regexp.MustCompile(`[A-Za-z0-9_-]+:(g:)?[A-Za-z0-9_-]{20,}`)

func TestAuditNoRawIdentitiesOnScreen(t *testing.T) {
	for _, s := range auditSurfaces() {
		t.Run(s.name, func(t *testing.T) {
			for i, ln := range plainLines(s.view(t)) {
				if hit := idToken.FindString(ln); hit != "" {
					t.Errorf("row %d shows a raw identity %q — name the thing instead:\n%s", i, hit, ln)
				}
			}
		})
	}
}

// Every surface says what to press. A screen with no hint line is a screen you
// have to already know.
//
// Where the hint goes depends on the kind of surface: a full screen puts it in
// the footer, the bottom row; a modal puts it inside its own box, because it
// owns the middle of the screen and not the edges.
func TestAuditEverySurfaceOffersKeys(t *testing.T) {
	for _, s := range auditSurfaces() {
		t.Run(s.name, func(t *testing.T) {
			view := s.view(t)
			if isModal(s.name) {
				if !strings.Contains(ansi.Strip(view), "·") {
					t.Errorf("a modal must say what its keys are:\n%s", ansi.Strip(view))
				}
				return
			}
			out := plainLines(view)
			for i := len(out) - 1; i >= 0; i-- {
				if strings.TrimSpace(out[i]) == "" {
					continue
				}
				if !strings.Contains(out[i], "·") && !strings.Contains(out[i], "↵") {
					t.Errorf("no key hints on the footer row:\n%s", out[i])
				}
				return
			}
			t.Error("the surface rendered nothing at all")
		})
	}
}

// isModal is the surfaces that own a box in the middle rather than the frame.
func isModal(name string) bool {
	return strings.HasPrefix(name, "confirm/") || strings.HasPrefix(name, "editor/")
}

// Nothing on screen may be distinguishable by colour alone: NO_COLOR, mono
// terminals and a good share of readers see none of it. Every staged state
// carries a glyph, which is the whole non-colour signal now that deletions are
// no longer struck through.
func TestAuditEveryStateHasAGlyph(t *testing.T) {
	seen := map[string]edit.State{}
	for _, st := range []edit.State{edit.New, edit.Modified, edit.Moved, edit.Deleted} {
		_, marker := changeStyle(st)
		if strings.TrimSpace(marker) == "" {
			t.Errorf("state %v has no marker, so it reads as ordinary text without colour", st)
		}
		if other, ok := seen[marker]; ok && other != st {
			t.Errorf("states %v and %v share the marker %q", other, st, marker)
		}
		seen[marker] = st
	}
}

// Confirmations look alike: buttons, and the same three keys to work them.
func TestAuditConfirmationsAreUniform(t *testing.T) {
	for _, s := range auditSurfaces() {
		if !strings.HasPrefix(s.name, "confirm/") {
			continue
		}
		t.Run(s.name, func(t *testing.T) {
			out := ansi.Strip(s.view(t))
			if !strings.Contains(out, "←/→ choose") {
				t.Errorf("a confirmation must say how to move between its buttons:\n%s", out)
			}
			if !strings.Contains(out, "↵ confirm") {
				t.Errorf("a confirmation must say which key takes the focused button:\n%s", out)
			}
		})
	}
}

// The cursor changes which row is current, never what a row means. A row staged
// for deletion keeps the delete colour and the strikethrough underneath it, in
// every list that has rows.
func TestAuditSelectionKeepsTheState(t *testing.T) {
	for _, st := range []edit.State{edit.New, edit.Modified, edit.Deleted} {
		want, _ := changeStyle(st)
		got := selRowStyle(theme.SelRow, st)
		if got.GetForeground() != want.GetForeground() {
			t.Errorf("state %v: selected row keeps the selection colour instead of the state's", st)
		}
		if got.GetBackground() != theme.SelRow.GetBackground() {
			t.Errorf("state %v: the selection background was lost, so the cursor disappears", st)
		}
	}
	if s := selRowStyle(theme.SelRow, 0); s.GetForeground() != theme.SelRow.GetForeground() {
		t.Error("an unstaged row keeps the plain selection colours")
	}
}
