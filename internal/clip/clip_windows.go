//go:build windows

package clip

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pOpenClipboard    = user32.NewProc("OpenClipboard")
	pCloseClipboard   = user32.NewProc("CloseClipboard")
	pEmptyClipboard   = user32.NewProc("EmptyClipboard")
	pGetClipboardData = user32.NewProc("GetClipboardData")
	pSetClipboardData = user32.NewProc("SetClipboardData")
	pRegisterFormat   = user32.NewProc("RegisterClipboardFormatW")

	pGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	pGlobalFree   = kernel32.NewProc("GlobalFree")
	pGlobalLock   = kernel32.NewProc("GlobalLock")
	pGlobalUnlock = kernel32.NewProc("GlobalUnlock")

	excludeName, _ = windows.UTF16PtrFromString("ExcludeClipboardContentFromMonitorProcessing")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

func concealedType() string { return "ExcludeClipboardContentFromMonitorProcessing" }

func openClipboard() error {
	for i := 0; i < 12; i++ {
		if r, _, _ := pOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("could not open the clipboard")
}

func platformWrite(s []byte) error {
	if err := openClipboard(); err != nil {
		return err
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	if s == nil {
		return nil // cleared
	}

	u16, err := windows.UTF16FromString(string(s))
	if err != nil {
		return err
	}
	hMem, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(u16)*2))
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc failed")
	}
	ptr, _, _ := pGlobalLock.Call(hMem)
	if ptr == 0 {
		pGlobalFree.Call(hMem)
		return fmt.Errorf("GlobalLock failed")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(u16)), u16)
	pGlobalUnlock.Call(hMem)

	if r, _, _ := pSetClipboardData.Call(cfUnicodeText, hMem); r == 0 {
		pGlobalFree.Call(hMem)
		return fmt.Errorf("SetClipboardData failed")
	}
	// Mark the content excluded from clipboard history / monitoring (spec §9).
	if fmtID, _, _ := pRegisterFormat.Call(uintptr(unsafe.Pointer(excludeName))); fmtID != 0 {
		if em, _, _ := pGlobalAlloc.Call(gmemMoveable, 1); em != 0 {
			pSetClipboardData.Call(fmtID, em)
		}
	}
	return nil
}

func platformRead() ([]byte, error) {
	if err := openClipboard(); err != nil {
		return nil, err
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return []byte{}, nil
	}
	ptr, _, _ := pGlobalLock.Call(h)
	if ptr == 0 {
		return []byte{}, nil
	}
	defer pGlobalUnlock.Call(h)
	return []byte(windows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr)))), nil
}
