//go:build windows

package clip

import "fmt"

// concealedType is the marker Windows clipboard managers honor.
//
// NOTE: the concealed *write* via the Win32 clipboard API needs
// uintptr→unsafe.Pointer conversions on GlobalLock'd HGLOBAL memory, which trip
// `go vet`'s unsafeptr check (a false positive for non-Go process memory, but
// not suppressible per-line). A vet-clean implementation is a follow-up; until
// then the Windows clipboard is unimplemented. harmos still runs on Windows —
// only copy-to-clipboard is unavailable (ls/get/browse work).
func concealedType() string { return "ExcludeClipboardContentFromMonitorProcessing" }

func platformWrite([]byte) error {
	return fmt.Errorf("clipboard on Windows is not yet implemented")
}

func platformRead() ([]byte, error) {
	return nil, fmt.Errorf("clipboard on Windows is not yet implemented")
}
