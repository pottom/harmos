package vault

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
)

// A census is what a kdbx file actually contains: every XML element path
// (parent>child) and how many times it occurs. Names and counts only, never
// values — so it is safe to compute over a real vault, and safe to put in an
// error message.
//
// It exists because the library silently drops XML it does not model: Go's
// encoding/xml ignores unknown elements, and the format gains elements over
// time. Checking the *version label* only catches the losses somebody thought to
// enumerate. Checking the *contents* catches the rest, including elements that
// do not exist yet.
type census map[string]int

// censusOf walks the decrypted XML that Decode leaves behind in
// Content.RawData. That buffer holds the inner header followed by the plaintext
// XML, so the walk starts at the document element.
func censusOf(db *gokeepasslib.Database) (census, error) {
	if db.Content == nil || len(db.Content.RawData) == 0 {
		return nil, errors.New("no decoded content to inspect")
	}

	raw := db.Content.RawData
	start := bytes.Index(raw, []byte("<KeePassFile"))
	if start < 0 {
		return nil, errors.New("decoded content has no KeePassFile element")
	}

	c := census{}
	var stack []string
	dec := xml.NewDecoder(bytes.NewReader(raw[start:]))

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("walk kdbx xml: %w", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			path := e.Name.Local
			if len(stack) > 0 {
				path = stack[len(stack)-1] + ">" + e.Name.Local
			}
			c[path]++
			stack = append(stack, e.Name.Local)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return c, nil
}

// lostSince reports element paths that were in the original and are missing (or
// fewer) in the rewritten file, worst first. An empty result means the write
// kept everything.
func (before census) lostSince(after census) []string {
	var lost []string
	for path, was := range before {
		if now := after[path]; now < was {
			lost = append(lost, fmt.Sprintf("%s (%d → %d)", path, was, now))
		}
	}
	sort.Strings(lost)
	return lost
}

// roundTripLoss reports what this database would lose if it were written back
// out — by encoding it in memory, decoding that, and comparing censuses.
//
// This is the writability gate. It replaces the version check it grew out of,
// which refused every KDBX 4.1 file: real vaults are 4.1, and most of them use
// nothing the library cannot keep. Asking what the file *contains* is both
// kinder (a 4.1 file that uses no unsupported element writes fine) and stricter
// (it catches whatever the next format revision adds, with no code change here).
//
// The cost is one encode plus one decode at open time, dominated by the KDF.
func roundTripLoss(db *gokeepasslib.Database, creds *gokeepasslib.DBCredentials) ([]string, error) {
	before, err := censusOf(db)
	if err != nil {
		return nil, err
	}

	// Encode expects a locked database and leaves it locked; Open left this one
	// unlocked. Restore that on the way out so the caller's handle stays usable.
	if err := db.LockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("lock for probe: %w", err)
	}
	defer func() { _ = db.UnlockProtectedEntries() }()

	var buf bytes.Buffer
	if err := gokeepasslib.NewEncoder(&buf).Encode(db); err != nil {
		return nil, fmt.Errorf("probe encode: %w", err)
	}

	probe := gokeepasslib.NewDatabase()
	probe.Credentials = creds
	if err := gokeepasslib.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(probe); err != nil {
		return nil, fmt.Errorf("probe decode: %w", err)
	}

	after, err := censusOf(probe)
	if err != nil {
		return nil, err
	}
	return before.lostSince(after), nil
}
