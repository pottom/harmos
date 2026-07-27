package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	gokeyring "github.com/zalando/go-keyring"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

func TestSettingsThemePicker(t *testing.T) {
	defer theme.Apply(theme.Charm)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WriteKdbxProfile(cfgPath, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	m := up(New(nil, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 80, Height: 16})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}) // jump into the Theme pane
	if m.setCat != catTheme || m.focus != 1 {
		t.Fatalf("t should open the Theme pane (cat=%d focus=%d)", m.setCat, m.focus)
	}
	before := m.themeName
	m = up(m, tea.KeyMsg{Type: tea.KeyDown}) // preview the next theme
	if m.themeName == before {
		t.Error("moving should preview a different theme")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // save
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != m.themeName {
		t.Errorf("saved theme %q != active %q", cfg.Theme, m.themeName)
	}
}

func TestSettingsSavePassword(t *testing.T) {
	gokeyring.MockInit()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WriteKdbxProfile(cfgPath, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	m := up(New(nil, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 80, Height: 18})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // into the Sources pane
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if m.setMode != setPrompt {
		t.Fatal("p should open the save-password prompt")
	}
	m = typeStr(m, "secret")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.setMode != setList {
		t.Fatalf("enter should store and return to the list (status %q)", m.setStatus)
	}
	if pw, ok, _ := keyring.Fetch("own"); !ok || pw.Reveal() != "secret" {
		t.Errorf("kdbx password not saved: ok=%v pw=%q", ok, pw.Reveal())
	}
}

func TestSettingsSyncNeedsCredentials(t *testing.T) {
	gokeyring.MockInit() // empty keyring
	t.Setenv("HARMOS_MASTER", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WritePleasantProfile(cfgPath, "work", "https://x.invalid", "u", filepath.Join(dir, "w.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	m := up(New(nil, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 80, Height: 18})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // into the Sources pane
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if m.setMode == setSyncing {
		t.Fatal("sync should not start without saved credentials")
	}
	if !strings.Contains(m.setStatus, "master") {
		t.Errorf("expected a 'save the master' hint, got %q", m.setStatus)
	}
}

func TestSettingsAddForm(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	m := up(New(nil, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 20})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}}) // Settings
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})                       // into the Sources pane
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) // add form
	if m.setMode != setForm {
		t.Fatal("a should open the add form")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // Type toggle → Name
	m = typeStr(m, "own")
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // Name → Path
	m = typeStr(m, "/vault/own.kdbx")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // submit
	if m.setMode != setList {
		t.Fatalf("submit should return to the list (mode=%d, status=%q)", m.setMode, m.setStatus)
	}
	profs := m.sources()
	if len(profs) != 1 || profs[0].Name != "own" {
		t.Fatalf("form add failed: %+v (status %q)", profs, m.setStatus)
	}
	if !strings.HasSuffix(profs[0].Path, "own.kdbx") {
		t.Errorf("path = %q, want …/own.kdbx", profs[0].Path)
	}
}

func TestSettingsRemove(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if _, err := config.WriteKdbxProfile(cfgPath, "a", filepath.Join(dir, "a.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteKdbxProfile(cfgPath, "b", filepath.Join(dir, "b.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}

	m := up(New(nil, cfgPath, 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}}) // Settings
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})                       // into the Sources pane
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}) // remove the selected (a)
	if m.setMode != setRemove {
		t.Fatal("d should open the remove confirmation")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // confirm (no toggles)
	if got := m.sources(); len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("after removing a, want [b], got %+v", got)
	}
}

// PageUp/PageDown page the results list a full screenful at a time and clamp at
// the ends.
func TestPageKeysPageThroughResults(t *testing.T) {
	var es []vault.Entry
	for i := 0; i < 40; i++ {
		es = append(es, vault.Entry{Source: "s", Path: "F", Title: fmt.Sprintf("host-%02d", i), Password: secret.New("p")})
	}
	m := New(es, "", 30*time.Second)
	m = up(m, tea.WindowSizeMsg{Width: 100, Height: 20})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "host")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // apply the filter → results browse
	if len(m.results) != 40 {
		t.Fatalf("want 40 results, got %d", len(m.results))
	}
	if m.sel != 0 {
		t.Fatalf("cursor should start at the top, got %d", m.sel)
	}

	_, step := m.panelRows()
	if step < 2 {
		t.Fatalf("test needs a multi-row page, got step=%d", step)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.sel != step {
		t.Fatalf("pgdown should jump one page (%d), got %d", step, m.sel)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.sel != 0 {
		t.Fatalf("pgup should return to the top, got %d", m.sel)
	}
	for i := 0; i < 20; i++ { // paging past the end clamps at the last result
		m = up(m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if m.sel != len(m.results)-1 {
		t.Fatalf("pgdown should clamp at the last result, got %d", m.sel)
	}
}

func testModel() Model {
	return New([]vault.Entry{
		{Source: "work", Path: "Infra", Title: "db-prod", Username: "svc_admin", Password: secret.New("p1")},
		{Source: "work", Path: "Infra", Title: "db-staging", Username: "svc", Password: secret.New("p2")},
		{Source: "personal", Path: "Net", Title: "router", Username: "admin", Password: secret.New("p3"), URL: "https://10.0.0.1"},
	}, "", 30*time.Second)
}

func up(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func typeStr(m Model, s string) Model {
	for _, r := range s {
		m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// The tree is the base surface: it renders on an empty query with no typing.
func TestTreeIsTheBase(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	out := m.View()
	// both source roots show as folders in the left pane
	for _, want := range []string{"personal", "work", "sources"} {
		if !strings.Contains(out, want) {
			t.Errorf("browse view missing %q", want)
		}
	}
	if m.searching() {
		t.Error("fresh model must not be searching")
	}
}

// Renders at every breakpoint, including a resize sequence, and refuses when tiny.
func TestRendersAcrossBreakpoints(t *testing.T) {
	m := testModel()
	sizes := [][2]int{{200, 50}, {100, 30}, {80, 24}, {60, 20}, {40, 12}, {30, 8}, {100, 30}}
	for _, sz := range sizes {
		m = up(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		out := m.View()
		if out == "" {
			t.Fatalf("%dx%d rendered empty", sz[0], sz[1])
		}
		if (sz[0] < 40 || sz[1] < 10) && !strings.Contains(out, "too small") {
			t.Errorf("%dx%d should refuse to render", sz[0], sz[1])
		}
	}
}

// In tree browse, letters are free for future hotkeys — they must not type into
// the search box. Only "/" opens search.
func TestLettersAreFreeUntilSlash(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = typeStr(m, "abc")
	if m.searchMode || m.input.Value() != "" {
		t.Fatalf("letters must not start a search: mode=%v value=%q", m.searchMode, m.input.Value())
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !m.searchMode {
		t.Fatal("/ should open the search box")
	}
}

// / opens search, typing filters, enter leaves the box keeping the filter, and
// esc clears the filter back to the tree.
func TestSlashSearchFlow(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "db-prod")
	if !m.searchMode {
		t.Fatal("should be in search mode while typing")
	}
	if len(m.results) == 0 || m.results[0].Entry.Title != "db-prod" {
		t.Fatalf("top result wrong: %+v", m.results)
	}
	if !strings.Contains(m.View(), "Search results") {
		t.Error("search should show the results pane")
	}
	// enter leaves the box but keeps the filter/results
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.searchMode {
		t.Error("enter should leave the search box")
	}
	if !m.showResults() || m.input.Value() != "db-prod" {
		t.Error("enter must keep the filter and results")
	}
	// esc clears the filter back to the tree
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.showResults() || m.input.Value() != "" {
		t.Error("esc should clear the filter back to the tree")
	}
}

// Navigating the tree into a folder's table and opening an entry's details.
func TestBrowseIntoDetails(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	// visible tree: personal, Net, work, Infra (roots expanded, leaves collapsed)
	// walk down to a folder that holds entries, then into its table
	var folder *node
	for i := 0; i < len(m.visible()); i++ {
		if f := m.currentFolder(); f != nil && len(f.entries) > 0 {
			folder = f
			break
		}
		m = up(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if folder == nil {
		t.Fatal("no folder with entries found in the tree")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // into the entry table
	if m.focus != 1 {
		t.Fatalf("tab should move focus to the table, focus=%d", m.focus)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // ↵ copies the password, does not open details
	if m.detail {
		t.Error("↵ on an entry should copy the password, not open the details screen")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRight}) // → opens details
	if !m.detail {
		t.Fatal("→ on an entry should open the details screen")
	}
	e := m.selEntry()
	if e == nil {
		t.Fatal("details screen has no selected entry")
	}
	view := m.View()
	if !strings.Contains(view, "user") || !strings.Contains(view, "password") {
		t.Error("details should show the user and password fields")
	}
	if e.Username != "" && !strings.Contains(view, e.Username) {
		t.Errorf("details should show the username %q", e.Username)
	}
	// reveal toggles, esc leaves
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.reveal {
		t.Error("ctrl+r should reveal the password")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.detail {
		t.Error("esc should leave the details screen")
	}
}

// Copying several entries in a row must not stack overlapping countdown tick
// loops (which made the timer run 2×, 3×… too fast).
func TestCountdownDoesNotStack(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "db") // db-prod, db-staging
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.results) < 2 {
		t.Fatalf("need at least two results, got %d", len(m.results))
	}

	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // copy the first
	m = nm.(Model)
	if m.remaining == 0 {
		t.Skip("clipboard unavailable in this environment")
	}
	if cmd == nil {
		t.Fatal("the first copy should start the countdown tick loop")
	}

	m = up(m, tea.KeyMsg{Type: tea.KeyDown})           // select another result
	nm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // copy again while counting down
	m = nm.(Model)
	if cmd != nil {
		t.Error("a copy while the countdown is running must not start a second tick loop")
	}
	// one tick still decrements by exactly one second
	before := m.remaining
	nm, _ = m.Update(tickMsg(time.Now()))
	m = nm.(Model)
	if m.remaining != before-1 {
		t.Errorf("a single tick should drop the timer by 1, got %d → %d", before, m.remaining)
	}
}

// A TOTP entry shows a live code row in the detail split and starts a refresh
// tick; a non-TOTP entry does neither.
func TestDetailTOTP(t *testing.T) {
	ents := []vault.Entry{
		{Source: "s", Path: "p", Title: "hasotp", Password: secret.New("p"),
			TOTP: "otpauth://totp/s:x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&digits=6"},
		{Source: "s", Path: "p", Title: "plain", Password: secret.New("p")},
	}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 16})

	// open the TOTP entry via search
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "hasotp")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = nm.(Model)
	if !m.detail {
		t.Fatal("→ should open the detail split")
	}
	if cmd == nil {
		t.Error("opening a TOTP entry should start the refresh tick")
	}
	if !strings.Contains(m.View(), "totp") {
		t.Error("the detail should show a totp row")
	}

	// a plain entry: no tick
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc}) // back to results
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc}) // clear search → tree
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "plain")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	nm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = nm.(Model)
	if cmd != nil {
		t.Error("a non-TOTP entry should not start a refresh tick")
	}
}

// The detail split shows the Notes field, wrapped.
func TestDetailNotes(t *testing.T) {
	ents := []vault.Entry{{
		Source: "s", Path: "p", Title: "withnotes", Password: secret.New("p"),
		Notes: "line one here\nsecond line of the note",
	}}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 18})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "withnotes")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = up(m, tea.KeyMsg{Type: tea.KeyRight})
	view := m.View()
	if !strings.Contains(view, "notes") || !strings.Contains(view, "second line") {
		t.Error("the detail should show the notes field")
	}
}

// The detail split lists custom fields, masking protected ones until ctrl+r.
func TestDetailCustomFields(t *testing.T) {
	ents := []vault.Entry{{
		Source: "s", Path: "p", Title: "cf", Password: secret.New("p"),
		Custom: []vault.Field{
			{Name: "Environment", Value: "production"},
			{Name: "Recovery", Value: "8842", Protected: true},
		},
	}}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 18})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "cf")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = up(m, tea.KeyMsg{Type: tea.KeyRight})
	view := m.View()
	if !strings.Contains(view, "Environment") || !strings.Contains(view, "production") {
		t.Error("detail should list custom fields")
	}
	if strings.Contains(view, "8842") {
		t.Error("a protected custom field must be masked before reveal")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !strings.Contains(m.View(), "8842") {
		t.Error("ctrl+r should reveal protected custom fields")
	}
}

// A long detail (notes) scrolls with ↑↓, clamped at both ends, and resets on open.
func TestDetailScroll(t *testing.T) {
	long := strings.Repeat("a reasonably long line of the runbook notes here\n", 40)
	ents := []vault.Entry{{Source: "s", Path: "p", Title: "big", Password: secret.New("p"), Notes: long}}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 80, Height: 14})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "big")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = up(m, tea.KeyMsg{Type: tea.KeyRight}) // open detail
	if m.detailScroll != 0 {
		t.Fatal("scroll should start at the top")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.detailScroll != 0 {
		t.Error("↑ at the top must stay at 0")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.detailScroll != 1 {
		t.Errorf("↓ should scroll to 1, got %d", m.detailScroll)
	}
	for i := 0; i < 300; i++ {
		m = up(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	w, vis := m.detailViewport()
	if want := len(m.detailLines(m.selEntry(), w)) - vis; m.detailScroll != want {
		t.Errorf("scroll should clamp to %d at the bottom, got %d", want, m.detailScroll)
	}
	// re-opening the entry resets the scroll
	nm, _ := m.openDetail()
	if nm.(Model).detailScroll != 0 {
		t.Error("opening an entry should reset the scroll to the top")
	}
}

// A long list shows a scrollbar; the mouse wheel scrolls it like ↑↓.
func TestScrollbarAndWheel(t *testing.T) {
	var ents []vault.Entry
	for range 20 {
		ents = append(ents, vault.Entry{Source: "s", Path: "f", Title: "entry", Password: secret.New("p")})
	}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 80, Height: 12})
	m = up(m, tea.KeyMsg{Type: tea.KeyRight}) // into the entry table
	if m.focus != 1 {
		t.Fatalf("→ should focus the table, got focus=%d", m.focus)
	}
	if !strings.Contains(m.View(), "┃") {
		t.Error("an overflowing list should draw a scrollbar thumb")
	}

	before := m.esel
	m = up(m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.esel != before+3 {
		t.Errorf("wheel down should move the selection by 3, got %d→%d", before, m.esel)
	}
	m = up(m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.esel != before {
		t.Errorf("wheel up should move back to %d, got %d", before, m.esel)
	}
}

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y}
}

// Left-clicks select tree/entry rows, open the already-selected row, and the tab
// indicator switches tabs.
func TestMouseClick(t *testing.T) {
	ents := []vault.Entry{
		{Source: "s", Path: "f", Title: "a", Password: secret.New("p")},
		{Source: "s", Path: "f", Title: "b", Password: secret.New("p")},
		{Source: "s", Path: "f", Title: "c", Password: secret.New("p")},
	}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	// visible tree = [s (0), f (1)]; panel content starts at Y=2, left pane X<40

	m = up(m, click(5, 2)) // tree row 0 → source "s"
	if m.tsel != 0 || m.focus != 0 {
		t.Errorf("clicking tree row 0 → tsel=0/focus=0, got %d/%d", m.tsel, m.focus)
	}
	m = up(m, click(5, 3)) // tree row 1 → folder "f"
	if m.tsel != 1 {
		t.Errorf("clicking tree row 1 → tsel=1, got %d", m.tsel)
	}

	// right pane entries: header at Y=2, entries a/b/c at Y=3/4/5
	m = up(m, click(50, 4)) // entry "b"
	if m.esel != 1 || m.focus != 1 {
		t.Errorf("clicking entry row → esel=1/focus=1, got %d/%d", m.esel, m.focus)
	}
	m = up(m, click(50, 4)) // click the selected row again → open detail
	if !m.detail {
		t.Error("clicking the selected entry again should open the detail")
	}

	m = up(m, tea.KeyMsg{Type: tea.KeyEsc}) // leave detail
	// tab indicator on the last line (Y=29): "1 Vault · 2 Settings" right-aligned
	m = up(m, click(94, 29)) // in the "2 Settings" region
	if m.tab != 1 {
		t.Errorf("clicking the Settings tab should switch to it, got tab=%d", m.tab)
	}
	m = up(m, click(82, 29)) // "1 Vault"
	if m.tab != 0 {
		t.Errorf("clicking the Vault tab should switch back, got tab=%d", m.tab)
	}
}

func rclick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress, X: x, Y: y}
}

// Right-clicking an entry selects it and copies its password without opening it.
func TestRightClickCopies(t *testing.T) {
	ents := []vault.Entry{
		{Source: "s", Path: "f", Title: "a", Password: secret.New("p")},
		{Source: "s", Path: "f", Title: "b", Password: secret.New("p")},
		{Source: "s", Path: "f", Title: "c", Password: secret.New("p")},
	}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, rclick(50, 5)) // right-click entry "c" (row 2, Y=5)
	if m.esel != 2 || m.focus != 1 {
		t.Errorf("right-click should select the entry under it, got esel=%d focus=%d", m.esel, m.focus)
	}
	if m.detail {
		t.Error("right-click must not open the detail")
	}
}

// Right-click copies while viewing an entry's detail (without leaving it).
func TestRightClickInDetail(t *testing.T) {
	ents := []vault.Entry{{Source: "s", Path: "f", Title: "a", Password: secret.New("p")}}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, click(50, 3)) // select
	m = up(m, click(50, 3)) // open detail
	if !m.detail {
		t.Fatal("should be in the detail")
	}
	m = up(m, rclick(60, 6)) // right-click inside the detail
	if !m.detail {
		t.Error("right-click in the detail must not leave it")
	}
}

// In the detail view, clicking the tree on the left leaves the entry and jumps.
func TestDetailClickTreeExits(t *testing.T) {
	ents := []vault.Entry{
		{Source: "s", Path: "f", Title: "a", Password: secret.New("p")},
		{Source: "s", Path: "f", Title: "b", Password: secret.New("p")},
	}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, click(50, 3)) // select entry
	m = up(m, click(50, 3)) // double-click → open detail
	if !m.detail {
		t.Fatal("double-click should have opened the detail")
	}
	m = up(m, click(5, 2)) // click tree row 0 ("s") on the left
	if m.detail {
		t.Error("clicking the tree should leave the detail")
	}
	if m.tsel != 0 || m.focus != 0 {
		t.Errorf("should jump to the clicked folder, got tsel=%d focus=%d", m.tsel, m.focus)
	}
}

// Double-clicking a folder expands/collapses it (like Enter).
func TestFolderDoubleClick(t *testing.T) {
	ents := []vault.Entry{{Source: "s", Path: "parent/child", Title: "x", Password: secret.New("p")}}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	// visible tree = [s, parent]; "parent" is collapsed so "child" is hidden
	before := len(m.visible())

	m = up(m, click(5, 3)) // select "parent" (row 1)
	m = up(m, click(5, 3)) // double-click → expand it (reveals "child")
	if len(m.visible()) <= before {
		t.Error("double-clicking a folder should expand it")
	}
}

// An empty search box (opened, or deleted back to empty) shows the tree, not a
// dump of every entry.
func TestEmptySearchShowsTree(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}) // open search, empty
	if m.showResults() || len(m.results) != 0 {
		t.Errorf("empty search must not show results, got showResults=%v n=%d", m.showResults(), len(m.results))
	}
	m = typeStr(m, "db") // now there's a query
	if !m.showResults() || len(m.results) == 0 {
		t.Error("typing a query should show results")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = up(m, tea.KeyMsg{Type: tea.KeyBackspace}) // delete back to empty
	if m.showResults() || len(m.results) != 0 {
		t.Errorf("deleting to empty should return to the tree, got showResults=%v n=%d", m.showResults(), len(m.results))
	}
}

// matchCounts tags each folder with how many hits it (and its descendants) hold.
func TestMatchCounts(t *testing.T) {
	ents := []vault.Entry{
		{Source: "s", Path: "a", Title: "foo1", Password: secret.New("p")},
		{Source: "s", Path: "a", Title: "foo2", Password: secret.New("p")},
		{Source: "s", Path: "b", Title: "bar", Password: secret.New("p")},
	}
	m := up(New(ents, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.matchCounts() != nil {
		t.Error("no counts without an active query")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "foo")

	var root, a, b *node
	for _, r := range m.roots {
		if r.name == "s" {
			root = r
		}
	}
	for _, ch := range root.children {
		switch ch.name {
		case "a":
			a = ch
		case "b":
			b = ch
		}
	}
	c := m.matchCounts()
	if c == nil {
		t.Fatal("counts should be non-nil while searching")
	}
	if c[a] != 2 {
		t.Errorf("folder a should hold 2 hits, got %d", c[a])
	}
	if c[b] != 0 {
		t.Errorf("folder b should hold 0 hits, got %d", c[b])
	}
	if c[root] != 2 {
		t.Errorf("root should aggregate 2 hits, got %d", c[root])
	}
}

// highlight preserves the visible text while emphasizing matches (colors may be
// stripped in the test environment, so we check the text, not the styling).
func TestHighlightAllOccurrences(t *testing.T) {
	defer theme.Apply(theme.Charm)
	for _, s := range []string{"runbook here and runbook there", "no match here", ""} {
		if got := ansi.Strip(highlight(s, "runbook", theme.Faded)); got != s {
			t.Errorf("highlight changed the text: %q → %q", s, got)
		}
	}
	// an empty query is a no-op
	if ansi.Strip(highlight("anything", "", theme.Faded)) != "anything" {
		t.Error("empty query should not change the text")
	}
}

// g on a search result leaves the search and lands on that entry's folder.
func TestGotoFolderFromResults(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "router")                  // personal · Net · router
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // apply filter, leave the box
	if !m.showResults() || len(m.results) == 0 {
		t.Fatalf("expected results for 'router', got %d", len(m.results))
	}
	want := m.results[m.sel].Entry
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})

	if m.searching() {
		t.Error("g should clear the search")
	}
	f := m.currentFolder()
	if f == nil {
		t.Fatal("g should select a folder in the tree")
	}
	if got := m.folderCrumb(m.visible(), m.tsel); got != want.Source+" › "+strings.ReplaceAll(want.Path, "/", " › ") {
		t.Errorf("landed on %q, want the entry's folder (%s · %s)", got, want.Source, want.Path)
	}
	if e := m.selEntry(); e == nil || e.Title != want.Title {
		t.Errorf("should select the entry %q in the folder", want.Title)
	}
}

// q quits from browse but is an ordinary character while typing a search.
func TestQuitAndSearchQ(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})

	// q in browse returns a (quit) command
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd == nil {
		t.Error("q should quit from browse mode")
	}

	// q while searching is typed, not a quit
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.input.Value() != "q" {
		t.Errorf("q should be typed in search mode, got %q", m.input.Value())
	}
}

// 1/2 switch tabs, but digits type while searching.
func TestTabsSwitch(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.tab != 0 {
		t.Fatal("default tab should be Vault")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if m.tab != 1 || !strings.Contains(m.View(), "Sources") {
		t.Error("2 should switch to the Settings tab")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if m.tab != 0 {
		t.Error("1 should switch back to Vault")
	}
	// while searching, digits are typed, not tab switches
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if m.tab != 0 || m.input.Value() != "2" {
		t.Errorf("digits should type in search, not switch tabs: tab=%d value=%q", m.tab, m.input.Value())
	}
}

// Help overlay toggles and any key closes it.
func TestHelpToggles(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	out := m.View()
	if !m.help || !strings.Contains(out, "Search") || !strings.Contains(out, "help") {
		t.Fatal("? should open the help screen with the search guide")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.help {
		t.Error("a non-navigation key should close help")
	}
}
