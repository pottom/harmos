//go:build darwin

package clip

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
void harmosClipWrite(const char* s);
char* harmosClipRead(void);
void harmosClipClear(void);
*/
import "C"

import "unsafe"

func platformWrite(s []byte) error {
	if s == nil {
		C.harmosClipClear()
		return nil
	}
	cs := C.CString(string(s))
	defer C.free(unsafe.Pointer(cs))
	C.harmosClipWrite(cs)
	return nil
}

func platformRead() ([]byte, error) {
	cs := C.harmosClipRead()
	defer C.free(unsafe.Pointer(cs))
	return []byte(C.GoString(cs)), nil
}

func concealedType() string { return "org.nspasteboard.ConcealedType" }
