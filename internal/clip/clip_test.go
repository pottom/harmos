package clip

import (
	"runtime"
	"testing"
)

// The clear-if-unchanged policy is platform-independent — test it against an
// in-memory clipboard so it runs everywhere.
func TestClearIfUnchanged(t *testing.T) {
	var board []byte
	writeFn = func(s []byte) error {
		if s == nil {
			board = nil
		} else {
			board = append([]byte(nil), s...)
		}
		return nil
	}
	readFn = func() ([]byte, error) { return board, nil }
	t.Cleanup(func() {
		writeFn, readFn = platformWrite, platformRead
		haveLast = false
	})

	// copy, then clear while unchanged → the clipboard is cleared
	if err := Copy([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	if string(board) != "secret" {
		t.Fatalf("after Copy board = %q", board)
	}
	if err := ClearIfUnchanged(); err != nil {
		t.Fatal(err)
	}
	if board != nil {
		t.Errorf("expected cleared clipboard, got %q", board)
	}

	// copy, the user copies something else, then clear → the user's value stays
	if err := Copy([]byte("secret2")); err != nil {
		t.Fatal(err)
	}
	board = []byte("the user copied this")
	if err := ClearIfUnchanged(); err != nil {
		t.Fatal(err)
	}
	if string(board) != "the user copied this" {
		t.Errorf("clear clobbered the user's clipboard: %q", board)
	}

	// a second clear does nothing (nothing tracked)
	if err := ClearIfUnchanged(); err != nil {
		t.Fatal(err)
	}
}

func TestConcealedType(t *testing.T) {
	got := ConcealedType()
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		if got == "" {
			t.Errorf("%s should report a concealed-clipboard marker", runtime.GOOS)
		}
	}
}
