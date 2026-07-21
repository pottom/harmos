//go:build !darwin

package clip

// Non-darwin platforms use MIME/format hints (KDE, wl-clipboard) or a Win32
// clipboard format instead of a pasteboard type; those land with the clipboard
// milestone. No cgo here, so Linux and Windows cross-compile normally.
func concealedType() string {
	return ""
}
