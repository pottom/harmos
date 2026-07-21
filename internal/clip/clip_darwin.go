//go:build darwin

package clip

/*
#cgo LDFLAGS: -framework Foundation

// Trivial cgo probe that links a system framework, proving the darwin cgo build
// path works on a real macOS runner. The actual NSPasteboard write lands with
// the clipboard milestone.
const char* harmosConcealedType(void) {
    return "org.nspasteboard.ConcealedType";
}
*/
import "C"

func concealedType() string {
	return C.GoString(C.harmosConcealedType())
}
