package tui

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest"
)

// TestInteractionContract drives the real Bubble Tea event loop through the
// state machine from docs/design/harmos-tui-interaction.md — browse → help,
// browse → search, and the too-small refuse — then asserts on the rendered
// frames and the final model. It tests the contract, not the colors.
//
// The frames are read after the program quits: teatest's output is flushed on
// shutdown, and the accumulated stream keeps every intermediate frame, so each
// visited state can be asserted as a positive presence. State that must be a
// specific value (the live query) is checked on the FinalModel.
func TestInteractionContract(t *testing.T) {
	// Keep it hermetic: no background release check reaching out to GitHub.
	t.Setenv("HARMOS_NO_UPDATE_CHECK", "1")

	tm := teatest.NewTestModel(t, testModel(), teatest.WithInitialTermSize(100, 24))

	// A short pause after each message lets that state render at least one frame,
	// so intermediate states (help, the refuse screen) survive in the stream.
	send := func(msg tea.Msg) {
		tm.Send(msg)
		time.Sleep(100 * time.Millisecond)
	}

	send(tea.WindowSizeMsg{Width: 100, Height: 24}) // seed the size, land in Browse
	send(key('?'))                                  // Browse → Help
	send(tea.KeyMsg{Type: tea.KeyEsc})              // Help → Browse (any key leaves)
	send(key('/'))                                  // Browse → Search
	for _, r := range "router" {                    // live-filter to one entry
		send(key(r))
	}
	send(tea.WindowSizeMsg{Width: 30, Height: 8})   // below the minimum → refuse
	send(tea.WindowSizeMsg{Width: 100, Height: 24}) // back to a usable size

	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	out := readAllStripped(t, tm)
	// Each visited state left a frame the contract requires.
	for _, want := range []string{
		"work",                        // Browse: the tree shows both sources
		"personal",                    //
		"hide / show the folder tree", // Help: a keymap line only the help screen renders
		"router",                      // Search: the typed query filtered the results
		"Terminal too small",          // Refuse: below the minimum size
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected a rendered frame to contain %q", want)
		}
	}

	fm, ok := tm.FinalModel(t).(Model)
	if !ok {
		t.Fatal("final model is not a tui.Model")
	}
	if !fm.searchMode {
		t.Error("expected the final model to be in search mode")
	}
	if got := fm.input.Value(); got != "router" {
		t.Errorf("final query = %q, want %q", got, "router")
	}
}

func readAllStripped(t *testing.T, tm *teatest.TestModel) string {
	t.Helper()
	b, err := io.ReadAll(tm.Output())
	if err != nil {
		t.Fatal(err)
	}
	return ansi.Strip(string(b))
}

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// The editing contract, driven through the real event loop.
//
// The unit tests check each state in isolation; this checks that the states join
// up — that unlocking, editing, staging and declining to save form one path a
// person can actually walk, and that walking it does not touch the file.
func TestEditInteractionContract(t *testing.T) {
	t.Setenv("HARMOS_NO_UPDATE_CHECK", "1")

	m := editModel(t)
	path := m.handles["own"].Path()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(110, 34))
	send := func(msg tea.Msg) {
		tm.Send(msg)
		time.Sleep(100 * time.Millisecond)
	}

	send(tea.WindowSizeMsg{Width: 110, Height: 34})
	send(key('z'))                       // open the source — the tree arrives closed
	send(tea.KeyMsg{Type: tea.KeyDown})  // onto the folder
	send(tea.KeyMsg{Type: tea.KeyTab})   // tree → entry table
	send(key('e'))                       // → the editor
	send(key('!'))                       // type into the title
	send(tea.KeyMsg{Type: tea.KeyEnter}) // stage it
	send(tabKey(tabChanges))             // → the Changes tab
	send(tea.KeyMsg{Type: tea.KeyCtrlS}) // → the save confirmation
	send(key('n'))                       // decline

	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

	out := readAllStripped(t, tm)
	for _, want := range []string{"-- EDIT --", "Write these changes?"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stream should have passed through %q", want)
		}
	}

	final := tm.FinalModel(t).(Model)
	if final.dirtyCount() != 1 {
		t.Errorf("declining the save should keep the change staged, got %d", final.dirtyCount())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("walking the whole editing path without saving still wrote to the file")
	}
}
