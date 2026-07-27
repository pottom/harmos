package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{"darwin", "arm64", "harmos_1.2.3_darwin_arm64.tar.gz", false},
		{"linux", "amd64", "harmos_1.2.3_linux_amd64.tar.gz", false},
		{"windows", "amd64", "harmos_1.2.3_windows_amd64.zip", false},
		{"linux", "mips", "", true}, // an arch with no release build
	}
	for _, tt := range tests {
		got, err := assetName("1.2.3", tt.goos, tt.goarch)
		if tt.wantErr {
			if !errors.Is(err, ErrUnsupported) {
				t.Errorf("assetName(%s/%s) err = %v, want ErrUnsupported", tt.goos, tt.goarch, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("assetName(%s/%s) unexpected err: %v", tt.goos, tt.goarch, err)
		}
		if got != tt.want {
			t.Errorf("assetName(%s/%s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestBinaryName(t *testing.T) {
	if got := binaryName("windows"); got != "harmos.exe" {
		t.Errorf("binaryName(windows) = %q, want harmos.exe", got)
	}
	if got := binaryName("linux"); got != "harmos" {
		t.Errorf("binaryName(linux) = %q, want harmos", got)
	}
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("the release archive bytes")
	sum := sha256.Sum256(archive)
	line := hex.EncodeToString(sum[:]) + "  harmos_1.2.3_linux_amd64.tar.gz\n" +
		"deadbeef  some_other_file.tar.gz\n"

	if err := verifyChecksum(archive, line, "harmos_1.2.3_linux_amd64.tar.gz"); err != nil {
		t.Errorf("matching checksum should verify: %v", err)
	}
	if err := verifyChecksum([]byte("tampered"), line, "harmos_1.2.3_linux_amd64.tar.gz"); err == nil {
		t.Error("a tampered archive must fail verification")
	}
	if err := verifyChecksum(archive, line, "not_listed.tar.gz"); err == nil {
		t.Error("an asset absent from the checksums must fail")
	}
}
