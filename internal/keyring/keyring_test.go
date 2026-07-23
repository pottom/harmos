package keyring

import (
	"testing"

	gokeyring "github.com/zalando/go-keyring"

	"github.com/pottom/harmos/internal/secret"
)

func TestStoreFetchForget(t *testing.T) {
	gokeyring.MockInit() // in-memory backend, never touches the real OS keyring

	if _, ok, err := Fetch("work"); err != nil || ok {
		t.Fatalf("empty keyring: ok=%v err=%v", ok, err)
	}

	if err := Store("work", secret.New("hunter2")); err != nil {
		t.Fatalf("store: %v", err)
	}
	pw, ok, err := Fetch("work")
	if err != nil || !ok {
		t.Fatalf("fetch after store: ok=%v err=%v", ok, err)
	}
	if pw.Reveal() != "hunter2" {
		t.Errorf("got %q, want hunter2", pw.Reveal())
	}

	// storing again overwrites
	if err := Store("work", secret.New("new-pass")); err != nil {
		t.Fatal(err)
	}
	if pw, _, _ := Fetch("work"); pw.Reveal() != "new-pass" {
		t.Errorf("overwrite failed: %q", pw.Reveal())
	}

	if err := Forget("work"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if _, ok, _ := Fetch("work"); ok {
		t.Error("password still present after forget")
	}
	// forgetting a missing entry is not an error
	if err := Forget("work"); err != nil {
		t.Errorf("forget missing: %v", err)
	}
}
