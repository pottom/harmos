package tui

import (
	"math"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/pottom/harmos/internal/theme"
)

var fgCode = regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)
var bgCode = regexp.MustCompile(`48;2;(\d+);(\d+);(\d+)`)

// A status line is the outcome of an action, and half the outcomes are
// failures: "sync failed", "could not save", "remove failed". Every one of them
// rendered in the colour that means it worked.
func TestAFailedActionDoesNotReadAsSuccess(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := up(New(nil, nil, "", 0), tea.WindowSizeMsg{Width: 100, Height: 30}).switchTab(tabSettings)

	ok := m
	ok.setStatus, ok.setStatusBad = "saved", false
	bad := m
	bad.setStatus, bad.setStatusBad = "sync failed: connection refused", true

	good, wrong := ok.settingsContext(), bad.settingsContext()
	if fgOf(good) == fgOf(wrong) {
		t.Errorf("a failure renders in the same colour as a success:\n  ok:   %q\n  fail: %q", good, wrong)
	}
	if fgOf(wrong) != fgOf(theme.Bad.Render("x")) {
		t.Error("a failure should wear the danger colour")
	}
}

func fgOf(s string) string {
	if m := fgCode.FindString(s); m != "" {
		return m
	}
	return ""
}

// The faint tokens are for text the eye should skip, and against the selection
// background they were text the eye could not find at all — measured at 1.00 in
// dracula, where Faint and SelBg are the same colour, and 1.65 in the default.
// The cursor was erasing the information it was supposed to be highlighting.
func TestNothingGoesInvisibleUnderTheCursor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("HARMOS_NERDFONT", "0")

	for _, name := range theme.Names() {
		tm, ok := theme.Builtin(name)
		if !ok {
			continue
		}
		theme.Apply(tm)

		m, _ := walkModel(t)
		m = onRow(t, m.expandAll(true), "Infra")
		rows := m.treeLines(m.leftPaneW()-2, 10)
		if m.tsel >= len(rows) {
			t.Fatalf("%s: no row under the cursor", name)
		}
		for _, run := range styledRuns(rows[m.tsel]) {
			if run.fg == "" || run.bg == "" || strings.TrimSpace(run.text) == "" {
				continue
			}
			if c := contrast(run.fg, run.bg); c < 2.5 {
				t.Errorf("%s: %q renders at contrast %.2f under the cursor", name, run.text, c)
			}
		}
	}
	theme.Apply(theme.Charm)
}

type run struct{ text, fg, bg string }

// styledRuns splits a rendered line into its escape-delimited runs.
func styledRuns(s string) []run {
	var out []run
	parts := regexp.MustCompile("\x1b\\[[0-9;]*m").Split(s, -1)
	codes := regexp.MustCompile("\x1b\\[[0-9;]*m").FindAllString(s, -1)
	fg, bg := "", ""
	for i, p := range parts {
		if i > 0 && i-1 < len(codes) {
			c := codes[i-1]
			if f := fgCode.FindString(c); f != "" {
				fg = f
			}
			if b := bgCode.FindString(c); b != "" {
				bg = b
			}
			if c == "\x1b[0m" {
				fg, bg = "", ""
			}
		}
		if p != "" {
			out = append(out, run{ansi.Strip(p), fg, bg})
		}
	}
	return out
}

// contrast is the WCAG ratio between two "38;2;r;g;b" / "48;2;r;g;b" codes.
func contrast(fg, bg string) float64 {
	l1, l2 := luminance(fg), luminance(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func luminance(code string) float64 {
	m := regexp.MustCompile(`;(\d+);(\d+);(\d+)`).FindStringSubmatch(code)
	if len(m) != 4 {
		return 0
	}
	var c [3]float64
	for i := range c {
		v := float64(atoiSafe(m[i+1])) / 255
		if v <= 0.03928 {
			c[i] = v / 12.92
		} else {
			c[i] = pow((v+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func pow(x, y float64) float64 { return math.Pow(x, y) }
