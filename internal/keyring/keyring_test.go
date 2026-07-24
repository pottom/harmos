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

func TestServerStoreFetchForget(t *testing.T) {
	gokeyring.MockInit()

	if _, ok, err := FetchServer("work"); err != nil || ok {
		t.Fatalf("empty server slot: ok=%v err=%v", ok, err)
	}
	if err := StoreServer("work", secret.New("srv")); err != nil {
		t.Fatalf("store server: %v", err)
	}
	pw, ok, err := FetchServer("work")
	if err != nil || !ok || pw.Reveal() != "srv" {
		t.Fatalf("fetch server: ok=%v err=%v pw=%q", ok, err, pw.Reveal())
	}
	// the server slot is separate from the per-name (kdbx) slot
	if _, ok, _ := Fetch("work"); ok {
		t.Error("a server password must not appear as a kdbx per-file password")
	}
	if err := ForgetServer("work"); err != nil {
		t.Fatalf("forget server: %v", err)
	}
	if _, ok, _ := FetchServer("work"); ok {
		t.Error("server password still present after forget")
	}
}

func TestMasterStoreFetchForget(t *testing.T) {
	gokeyring.MockInit()

	if _, ok, err := FetchMaster(); err != nil || ok {
		t.Fatalf("empty master: ok=%v err=%v", ok, err)
	}
	if err := StoreMaster(secret.New("m4ster")); err != nil {
		t.Fatalf("store master: %v", err)
	}
	pw, ok, err := FetchMaster()
	if err != nil || !ok || pw.Reveal() != "m4ster" {
		t.Fatalf("fetch master: ok=%v err=%v pw=%q", ok, err, pw.Reveal())
	}
	// the master is separate from per-source entries: a source named "work" is
	// untouched by StoreMaster
	if _, ok, _ := Fetch("work"); ok {
		t.Error("storing the master must not create a per-source entry")
	}
	if err := ForgetMaster(); err != nil {
		t.Fatalf("forget master: %v", err)
	}
	if _, ok, _ := FetchMaster(); ok {
		t.Error("master still present after forget")
	}
}
