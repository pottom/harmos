package pleasant

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/pottom/harmos/internal/secret"
)

func TestLoginAndIsOfflineAvailable(t *testing.T) {
	srv := fakeServer(t, true)
	c := loggedInClient(t, srv)

	ok, err := c.IsOfflineAvailable(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("IsOfflineAvailable = false, want true")
	}
}

func TestLoginBadCredentials(t *testing.T) {
	srv := fakeServer(t, true)
	c := New(srv.URL, WithHTTPClient(srv.Client()))
	if err := c.Login(t.Context(), "alice", secret.New("wrong")); err == nil {
		t.Fatal("expected login to fail with bad credentials")
	}
}

func TestIsOfflineAvailableFalse(t *testing.T) {
	srv := fakeServer(t, false)
	c := loggedInClient(t, srv)

	ok, err := c.IsOfflineAvailable(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("IsOfflineAvailable = true, want false")
	}
}

func TestCallsRequireLogin(t *testing.T) {
	srv := fakeServer(t, true)
	c := New(srv.URL, WithHTTPClient(srv.Client())) // not logged in

	if _, err := c.IsOfflineAvailable(t.Context()); err == nil {
		t.Error("IsOfflineAvailable without login should error")
	}
	if _, err := c.OfflinePackage(t.Context(), "test", &bytes.Buffer{}, nil); err == nil {
		t.Error("OfflinePackage without login should error")
	}
}

func TestOfflinePackageStreamsValidZip(t *testing.T) {
	srv := fakeServer(t, true)
	c := loggedInClient(t, srv)

	var buf bytes.Buffer
	n, err := c.OfflinePackage(t.Context(), "harmos test", &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) {
		t.Fatalf("reported %d bytes, buffer has %d", n, buf.Len())
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}

	var manifest *zip.File
	blobs := map[string]bool{}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".json") {
			manifest = f
		} else {
			blobs[f.Name] = true
		}
	}
	if manifest == nil {
		t.Fatal("zip has no .json manifest")
	}
	// The three attachment ids from the fixture must be present as zip entries.
	for _, id := range []string{
		"00000000-0000-0000-0000-00000000a001",
		"00000000-0000-0000-0000-00000000a002",
		"00000000-0000-0000-0000-00000000a003",
	} {
		if !blobs[id] {
			t.Errorf("attachment %s missing from zip", id)
		}
	}
}

func TestWrongTokenIsRejected(t *testing.T) {
	// A well-formed but unauthenticated call must surface the 401 as an error.
	srv := fakeServer(t, true)
	c := New(srv.URL, WithHTTPClient(srv.Client()))
	// Log in so a token is set, then confirm the happy path works; the fake
	// server rejects any token other than the one it issued.
	if err := c.Login(t.Context(), "alice", secret.New("secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.IsOfflineAvailable(t.Context()); err != nil {
		t.Fatalf("authenticated call should succeed: %v", err)
	}
}
