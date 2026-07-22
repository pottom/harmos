//go:build linux

package clip

import (
	"bytes"
	"fmt"
	"os/exec"
)

// concealedType is the KDE/Klipper hint. Note: applying it fully needs a
// clipboard owner that serves multiple targets at once; the shell-out below
// copies the text reliably but the concealment is best-effort. GNOME / plain
// Wayland have no convention at all (documented in the README).
func concealedType() string { return "x-kde-passwordManagerHint" }

func platformWrite(s []byte) error {
	if path, err := exec.LookPath("wl-copy"); err == nil {
		if s == nil {
			return exec.Command(path, "--clear").Run()
		}
		cmd := exec.Command(path)
		cmd.Stdin = bytes.NewReader(s)
		return cmd.Run()
	}
	if path, err := exec.LookPath("xclip"); err == nil {
		if s == nil {
			s = []byte{}
		}
		cmd := exec.Command(path, "-selection", "clipboard")
		cmd.Stdin = bytes.NewReader(s)
		return cmd.Run()
	}
	return fmt.Errorf("no clipboard tool found; install wl-clipboard or xclip")
}

func platformRead() ([]byte, error) {
	if path, err := exec.LookPath("wl-paste"); err == nil {
		return exec.Command(path, "-n").Output()
	}
	if path, err := exec.LookPath("xclip"); err == nil {
		return exec.Command(path, "-selection", "clipboard", "-o").Output()
	}
	return nil, fmt.Errorf("no clipboard tool found; install wl-clipboard or xclip")
}
