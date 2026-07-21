// Package clip will hold the per-platform concealed-clipboard implementation
// (spec §9): copying a password must set the platform's "do not record / do not
// sync" pasteboard hint, not merely clear it after a timeout.
//
// This is a scaffold. Only the build boundary exists so far — the macOS path is
// cgo (NSPasteboard), the others are pure Go, so Linux and Windows keep
// cross-compiling without a C toolchain.
package clip

// ConcealedType reports the platform's marker for "do not record / do not
// sync" clipboard contents, or "" if the platform has no standard one.
func ConcealedType() string {
	return concealedType()
}
