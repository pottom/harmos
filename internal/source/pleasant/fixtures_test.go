package pleasant

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pottom/harmos/internal/secret"
)

const fakeToken = "tok"

// attachmentContent is the deterministic body for an attachment id, so mapper
// tests can assert the bytes round-trip through the zip and the kdbx.
func attachmentContent(id string) []byte {
	return []byte("attachment:" + id)
}

// buildOfflineZip assembles a real OfflinePackage-shaped zip from the manifest:
// the JSON manifest plus one blob per attachment id, named by AttachmentId.
func buildOfflineZip(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg Package
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mw, err := zw.Create("11111111-1111-1111-1111-111111111111.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mw.Write(raw); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, a := range pkg.Attachments {
		if seen[a.AttachmentId] {
			continue
		}
		seen[a.AttachmentId] = true
		w, err := zw.Create(a.AttachmentId)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(attachmentContent(a.AttachmentId)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeServer stands in for a Pleasant server: token, IsOfflineAvailable, and the
// OfflinePackage zip. Password "wrong" simulates bad credentials.
func fakeServer(t *testing.T, offlineAvailable bool) *httptest.Server {
	t.Helper()
	zipBytes := buildOfflineZip(t)

	requireAuth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/OAuth2/Token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "password" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("password") == "wrong" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"` + fakeToken + `","token_type":"bearer","expires_in":3900}`))
	})
	mux.HandleFunc("/api/v6/rest/IsOfflineAvailable", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		if offlineAvailable {
			_, _ = w.Write([]byte("true"))
		} else {
			_, _ = w.Write([]byte("false"))
		}
	})
	mux.HandleFunc("/api/v6/rest/OfflinePackage", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// loggedInClient returns a Client already authenticated against srv.
func loggedInClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := New(srv.URL, WithHTTPClient(srv.Client()))
	if err := c.Login(t.Context(), "alice", secret.New("secret")); err != nil {
		t.Fatalf("login: %v", err)
	}
	return c
}
