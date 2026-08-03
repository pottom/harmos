// Package edit is the staged change set: what the user has asked for, before
// any of it has touched a file.
//
// It is pure data. No gokeepasslib, no terminal, no filesystem — so the whole
// model of "what would happen if we saved" is testable without a kdbx or a
// screen, and the same set drives both the in-place colouring and the diff.
//
// The dependency runs vault → edit, never back: this package must not import
// internal/vault, or applying a change set could not live next to the code that
// knows how to perform one.
package edit

import (
	"time"

	"github.com/pottom/harmos/internal/secret"
)

// Draft is an entry as an editor sees it: everything, exactly as it sits in the
// file.
//
// It is deliberately not vault.Entry. That type is a display projection — it
// collapses control characters, strips the pps.* prefixes off custom field names
// and splits tags on either separator — so round-tripping an edit through it
// would quietly rewrite values nobody asked to change.
type Draft struct {
	ID      string // "" for an entry that does not exist yet
	GroupID string

	Title    string
	Username string
	URL      string
	Notes    string
	Tags     string // raw, as stored: one string, whatever separator the file uses
	Password secret.Secret
	TOTP     string // the otpauth:// URI, if any

	Fields []DraftField // custom fields, raw keys, in file order

	Expires    bool
	ExpiryTime time.Time
}

// DraftField is one custom field. Key is the raw kdbx key — "pps.cuf.Env", not
// the "Env" the UI displays — because that prefix is part of the data, and
// rewriting it would change what other tools see.
type DraftField struct {
	Key       string
	Value     string
	Protected bool
}

// clone is a Draft the caller can no longer reach.
//
// The struct copies by value, but Fields is a slice header: without copying it
// too, two drafts that look independent share one backing array, and appending
// to one can overwrite the other's fields in place.
func (d *Draft) clone() *Draft {
	if d == nil {
		return nil
	}
	out := *d
	if d.Fields != nil {
		out.Fields = append([]DraftField(nil), d.Fields...)
	}
	return &out
}

// field returns the field with this key, if the draft has one.
func (d Draft) field(key string) (DraftField, bool) {
	for _, f := range d.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return DraftField{}, false
}
