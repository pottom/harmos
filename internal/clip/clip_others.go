//go:build !darwin && !linux && !windows

package clip

import "fmt"

// No clipboard convention (and no implementation) on this platform.
func concealedType() string { return "" }

func platformWrite([]byte) error {
	return fmt.Errorf("clipboard not supported on this platform")
}

func platformRead() ([]byte, error) {
	return nil, fmt.Errorf("clipboard not supported on this platform")
}
