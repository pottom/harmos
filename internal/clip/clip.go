// Package clip copies a secret to the system clipboard and marks it "do not
// record / do not sync" where the platform has a convention (spec §9). A plain
// timeout is not enough: clipboard managers and macOS Universal Clipboard
// capture the value on write, before any timer fires.
//
// Conventions (spec §9):
//   - macOS  — org.nspasteboard.ConcealedType (cgo, NSPasteboard)
//   - KDE    — x-kde-passwordManagerHint: secret (wl-copy / xclip target)
//   - Windows— ExcludeClipboardContentFromMonitorProcessing clipboard format
//   - GNOME / plain Wayland — no convention exists; documented, not faked.
//
// The real per-platform writers live in the build-tagged files. This file holds
// the platform-independent policy: track what we wrote so a later clear never
// clobbers something the user has copied since.
package clip

import (
	"crypto/sha256"
	"sync"
)

// backends are indirected through variables so tests can substitute an in-memory
// clipboard; production wires them to the platform implementation in init.
var (
	writeFn = platformWrite
	readFn  = platformRead
)

var (
	mu       sync.Mutex
	lastHash [32]byte
	haveLast bool
)

// Copy writes secret to the clipboard, concealed where supported. The caller is
// responsible for scheduling a later [ClearIfUnchanged].
func Copy(secret []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if err := writeFn(secret); err != nil {
		return err
	}
	lastHash = sha256.Sum256(secret)
	haveLast = true
	return nil
}

// ClearIfUnchanged clears the clipboard only if it still holds the value we last
// wrote. If the user has copied something else since, that value is left alone
// (spec §9). Safe to call on exit and after the timeout.
func ClearIfUnchanged() error {
	mu.Lock()
	defer mu.Unlock()
	if !haveLast {
		return nil
	}
	haveLast = false
	cur, err := readFn()
	if err != nil {
		return err
	}
	if sha256.Sum256(cur) != lastHash {
		return nil // the user copied something else; don't touch it
	}
	return writeFn(nil) // nil clears
}

// ConcealedType reports the platform's "do not record / do not sync" marker, or
// "" if the platform has no standard one.
func ConcealedType() string { return concealedType() }
