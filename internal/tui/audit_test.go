package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/secret"
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

func auditSurfaces() []surface {
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
