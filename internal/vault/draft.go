package vault

import (
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"

	"github.com/pottom/harmos/internal/secret"
)

// Draft is an entry as an editor sees it: everything, exactly as it sits in the
// file.
//
// It exists because Entry cannot be edited and written back. Entry is a display
// projection — it collapses control characters, strips the pps.* prefixes off
// custom field names, and splits Tags on either separator — so round-tripping
// through it would quietly rewrite values nobody asked to change. A Draft holds
// the raw text instead.
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
// the "Env" the UI displays — because that prefix is part of the data and
// rewriting it would change what other tools see.
type DraftField struct {
	Key       string
	Value     string
	Protected bool
}

// EntryDraft reads an entry losslessly, for editing.
func (h *Handle) EntryDraft(id string) (Draft, error) {
	_, e, err := h.findEntry(id)
	if err != nil {
		return Draft{}, err
	}

	var groupOf string
	h.walkTree(nil, func(_ *gokeepasslib.Entry, eid, gid, _ string) {
		if eid == id {
			groupOf = gid
		}
	})

	d := Draft{
		ID:       id,
		GroupID:  groupOf,
		Title:    e.GetTitle(),
		Username: e.GetContent("UserName"),
		URL:      e.GetContent("URL"),
		Notes:    e.GetContent("Notes"),
		Tags:     e.Tags,
		Password: secret.New(e.GetPassword()),
		TOTP:     e.GetContent("otp"),
	}
	for _, v := range e.Values {
		if stdField(v.Key) {
			continue
		}
		d.Fields = append(d.Fields, DraftField{
			Key:       v.Key,
			Value:     v.Value.Content,
			Protected: v.Value.Protected.Bool,
		})
	}
	if e.Times.Expires.Bool && e.Times.ExpiryTime != nil {
		d.Expires, d.ExpiryTime = true, e.Times.ExpiryTime.Time
	}
	return d, nil
}

// applyDraft writes a draft onto a live entry, field by field.
//
// Field by field, never by replacing the entry: everything the library models
// but harmos does not project — auto-type, custom data, icons, colours,
// attachments, usage counts — stays exactly where it was. Building a fresh entry
// and splicing it in would discard all of it.
func applyDraft(e *gokeepasslib.Entry, d Draft) {
	setValue(e, "Title", d.Title, false)
	setValue(e, "UserName", d.Username, false)
	setValue(e, "URL", d.URL, false)
	setValue(e, "Notes", d.Notes, false)
	setValue(e, "Password", d.Password.Reveal(), true)
	if d.TOTP != "" || hasValue(e, "otp") {
		setValue(e, "otp", d.TOTP, true)
	}

	e.Tags = d.Tags

	// Custom fields are replaced wholesale, because the draft is the complete
	// statement of what they should be: a field the user deleted has to
	// disappear, and one they renamed must not linger under its old key.
	kept := e.Values[:0]
	for _, v := range e.Values {
		if stdField(v.Key) {
			kept = append(kept, v)
		}
	}
	e.Values = kept
	for _, f := range d.Fields {
		if f.Key == "" {
			continue
		}
		e.Values = append(e.Values, gokeepasslib.ValueData{
			Key:   f.Key,
			Value: gokeepasslib.V{Content: f.Value, Protected: w.NewBoolWrapper(f.Protected)},
		})
	}

	e.Times.Expires = w.NewBoolWrapper(d.Expires)
	if d.Expires {
		e.Times.ExpiryTime = timePtr(d.ExpiryTime)
	}
}

func setValue(e *gokeepasslib.Entry, key, content string, protected bool) {
	for i := range e.Values {
		if e.Values[i].Key == key {
			e.Values[i].Value.Content = content
			e.Values[i].Value.Protected = w.NewBoolWrapper(protected)
			return
		}
	}
	e.Values = append(e.Values, gokeepasslib.ValueData{
		Key:   key,
		Value: gokeepasslib.V{Content: content, Protected: w.NewBoolWrapper(protected)},
	})
}

func hasValue(e *gokeepasslib.Entry, key string) bool {
	for i := range e.Values {
		if e.Values[i].Key == key {
			return true
		}
	}
	return false
}
