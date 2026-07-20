package clip

import (
	"runtime"
	"testing"
)

func TestConcealedType(t *testing.T) {
	got := ConcealedType()
	if runtime.GOOS == "darwin" && got == "" {
		t.Fatalf("darwin must report a concealed pasteboard type, got empty")
	}
}
