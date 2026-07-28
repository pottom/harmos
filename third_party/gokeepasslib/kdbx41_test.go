package gokeepasslib

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// The elements KDBX 4.1 added, plus Group>CustomData, which KDBX 4.0 added but
// this library never modelled. Unknown elements are discarded silently by
// encoding/xml, so before this support existed a 4.1 file could be decoded and
// re-encoded, still labelled 4.1, having quietly lost all of these.

func TestUnmarshalGroupKDBX41(t *testing.T) {
	cases := []struct {
		title         string
		xmlData       string
		expectedErr   error
		expectedGroup Group
	}{
		{
			title: "group with 4.1 fields",
			xmlData: `<Group>
				<UUID>rGjR5eICQEqPuNCbLDlPuA==</UUID>
				<Name>parent</Name>
				<Notes>notes</Notes>
				<Tags>work;archive</Tags>
				<IconID>48</IconID>
				<LastTopVisibleEntry>AAAAAAAAAAAAAAAAAAAAAA==</LastTopVisibleEntry>
				<CustomData>
					<Item>
						<Key>plugin.setting</Key>
						<Value>on</Value>
					</Item>
				</CustomData>
				<PreviousParentGroup>SnB29sd3a06jo6GR1BkGBQ==</PreviousParentGroup>
			</Group>`,
			expectedGroup: Group{
				UUID: UUID{
					0xac, 0x68, 0xd1, 0xe5,
					0xe2, 0x02, 0x40, 0x4a,
					0x8f, 0xb8, 0xd0, 0x9b,
					0x2c, 0x39, 0x4f, 0xb8,
				},
				Name:                "parent",
				Notes:               "notes",
				Tags:                "work;archive",
				IconID:              48,
				LastTopVisibleEntry: "AAAAAAAAAAAAAAAAAAAAAA==",
				CustomData: []CustomData{
					{XMLName: xml.Name{Local: "Item"}, Key: "plugin.setting", Value: "on"},
				},
				PreviousParentGroup: &UUID{
					0x4a, 0x70, 0x76, 0xf6,
					0xc7, 0x77, 0x6b, 0x4e,
					0xa3, 0xa3, 0xa1, 0x91,
					0xd4, 0x19, 0x06, 0x05,
				},
			},
		},
		{
			// A 4.0 file must decode to nil, not to a zero UUID: "the group was
			// never moved" and "the group was moved to nowhere" are different
			// statements, and only the pointer can tell them apart.
			title: "group without 4.1 fields leaves them unset",
			xmlData: `<Group>
				<UUID>rGjR5eICQEqPuNCbLDlPuA==</UUID>
				<Name>parent</Name>
			</Group>`,
			expectedGroup: Group{
				UUID: UUID{
					0xac, 0x68, 0xd1, 0xe5,
					0xe2, 0x02, 0x40, 0x4a,
					0x8f, 0xb8, 0xd0, 0x9b,
					0x2c, 0x39, 0x4f, 0xb8,
				},
				Name: "parent",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			decoder := xml.NewDecoder(bytes.NewBufferString(c.xmlData))

			var group Group

			err := decoder.Decode(&group)

			if !errors.Is(c.expectedErr, err) {
				t.Errorf("Expected %#v, received %#v", c.expectedErr, err)
			}

			assert.Equal(t, c.expectedGroup, group, "The groups should be identical")
		})
	}
}

func TestUnmarshalEntryKDBX41(t *testing.T) {
	cases := []struct {
		title         string
		xmlData       string
		expectedEntry Entry
	}{
		{
			title: "entry with 4.1 fields",
			xmlData: `<Entry>
				<UUID>rGjR5eICQEqPuNCbLDlPuA==</UUID>
				<IconID>2</IconID>
				<QualityCheck>False</QualityCheck>
				<PreviousParentGroup>SnB29sd3a06jo6GR1BkGBQ==</PreviousParentGroup>
			</Entry>`,
			expectedEntry: Entry{
				UUID: UUID{
					0xac, 0x68, 0xd1, 0xe5,
					0xe2, 0x02, 0x40, 0x4a,
					0x8f, 0xb8, 0xd0, 0x9b,
					0x2c, 0x39, 0x4f, 0xb8,
				},
				IconID:       2,
				QualityCheck: boolPtr(false),
				PreviousParentGroup: &UUID{
					0x4a, 0x70, 0x76, 0xf6,
					0xc7, 0x77, 0x6b, 0x4e,
					0xa3, 0xa3, 0xa1, 0x91,
					0xd4, 0x19, 0x06, 0x05,
				},
			},
		},
		{
			// QualityCheck defaults to true when absent, so a nil here is what
			// keeps a 4.0 file from acquiring an explicit setting it never had.
			title: "entry without 4.1 fields leaves them unset",
			xmlData: `<Entry>
				<UUID>rGjR5eICQEqPuNCbLDlPuA==</UUID>
				<IconID>2</IconID>
			</Entry>`,
			expectedEntry: Entry{
				UUID: UUID{
					0xac, 0x68, 0xd1, 0xe5,
					0xe2, 0x02, 0x40, 0x4a,
					0x8f, 0xb8, 0xd0, 0x9b,
					0x2c, 0x39, 0x4f, 0xb8,
				},
				IconID: 2,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			var entry Entry

			err := xml.NewDecoder(bytes.NewBufferString(c.xmlData)).Decode(&entry)
			if err != nil {
				t.Fatalf("Failed to decode entry: %s", err)
			}

			assert.Equal(t, c.expectedEntry, entry, "The entries should be identical")
		})
	}
}

// Marshalled child order must match the KDBX schema, which is what KeePass and
// KeePassXC write. Go emits struct fields in declaration order, so the struct
// order is the schema order and a reordering is a silent format change.
func TestMarshalOrderKDBX41(t *testing.T) {
	group := Group{
		Name:                "g",
		Tags:                "t",
		LastTopVisibleEntry: "x",
		CustomData:          []CustomData{{Key: "k", Value: "v"}},
		PreviousParentGroup: &UUID{},
		Entries:             []Entry{{}},
		Groups:              []Group{{}},
	}

	data, err := xml.Marshal(group)
	if err != nil {
		t.Fatalf("Failed to marshal group: %s", err)
	}

	assertOrder(t, string(data), []string{
		"UUID", "Name", "Notes", "Tags", "IconID", "CustomIconUUID", "Times",
		"IsExpanded", "DefaultAutoTypeSequence", "EnableAutoType",
		"EnableSearching", "LastTopVisibleEntry", "CustomData",
		"PreviousParentGroup", "Entry", "Group",
	})

	entry := Entry{
		Tags:                "t",
		QualityCheck:        boolPtr(false),
		PreviousParentGroup: &UUID{},
		Values:              []ValueData{{Key: "Title"}},
	}

	data, err = xml.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal entry: %s", err)
	}

	assertOrder(t, string(data), []string{
		"UUID", "IconID", "CustomIconUUID", "ForegroundColor", "BackgroundColor",
		"OverrideURL", "Tags", "Times", "QualityCheck", "PreviousParentGroup",
		"String",
	})
}

// assertOrder checks that the named elements appear in the given order in the
// marshalled output, ignoring anything not listed. It searches forward from the
// previous match rather than from the start, so a name that also labels an
// enclosing or repeated element does not match the wrong one.
func assertOrder(t *testing.T, data string, want []string) {
	t.Helper()

	at := 0
	for _, name := range want {
		rest := data[at:]

		i := strings.Index(rest, "<"+name+">")
		if j := strings.Index(rest, "<"+name+"/>"); j >= 0 && (i < 0 || j < i) {
			i = j
		}
		if i < 0 {
			t.Errorf("element %q is missing, or comes before an element that should precede it", name)
			return
		}

		at += i + 1
	}
}

// The point of the whole change: a 4.1 file must survive a decode/encode round
// trip with nothing dropped. Asserted at the XML level rather than field by
// field, because "the field we forgot to check" is exactly how the original data
// loss went unnoticed.
func TestRoundTripKeepsEveryElementKDBX41(t *testing.T) {
	db := NewDatabase(WithDatabaseKDBXVersion4())
	db.Credentials = NewPasswordCredentials(password)

	icon := CustomIcon{
		UUID:                 NewUUID(),
		Data:                 encodedIcon,
		Name:                 "an icon",
		LastModificationTime: &w.TimeWrapper{Time: refTime},
	}
	db.Content.Meta.CustomIcons = []CustomIcon{icon}
	db.Content.Meta.CustomData = []CustomData{
		{Key: "meta.key", Value: "meta.value", LastModificationTime: &w.TimeWrapper{Time: refTime}},
	}

	prev := NewUUID()

	entry := NewEntry()
	entry.Values = append(entry.Values, ValueData{Key: "Title", Value: V{Content: "an entry"}})
	entry.QualityCheck = boolPtr(false)
	entry.PreviousParentGroup = &prev
	entry.CustomData = []CustomData{
		{Key: "entry.key", Value: "entry.value", LastModificationTime: &w.TimeWrapper{Time: refTime}},
	}

	group := NewGroup()
	group.Name = "a group"
	group.Tags = "one;two"
	group.PreviousParentGroup = &prev
	group.CustomData = []CustomData{
		{Key: "group.key", Value: "group.value", LastModificationTime: &w.TimeWrapper{Time: refTime}},
	}
	group.Entries = []Entry{entry}

	root := NewGroup()
	root.Name = "root"
	root.Groups = []Group{group}
	db.Content.Root = &RootData{Groups: []Group{root}}

	if err := db.LockProtectedEntries(); err != nil {
		t.Fatalf("Failed to lock entries: %s", err)
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(db); err != nil {
		t.Fatalf("Failed to encode: %s", err)
	}

	decoded := NewDatabase()
	decoded.Credentials = NewPasswordCredentials(password)
	if err := NewDecoder(bytes.NewReader(buf.Bytes())).Decode(decoded); err != nil {
		t.Fatalf("Failed to decode: %s", err)
	}

	for _, want := range []string{
		"Group>Tags",
		"Group>CustomData",
		"Group>PreviousParentGroup",
		"Entry>QualityCheck",
		"Entry>PreviousParentGroup",
		"Entry>CustomData",
		"Icon>Name",
		"Icon>LastModificationTime",
		"Item>LastModificationTime",
	} {
		if n := census(t, decoded.Content.RawData)[want]; n == 0 {
			t.Errorf("%s did not survive the round trip", want)
		}
	}

	// And the values themselves, not merely the elements.
	gotGroup := decoded.Content.Root.Groups[0].Groups[0]
	if gotGroup.Tags != "one;two" {
		t.Errorf("group tags = %q, want one;two", gotGroup.Tags)
	}
	if gotGroup.PreviousParentGroup == nil || *gotGroup.PreviousParentGroup != prev {
		t.Errorf("group previous parent = %v, want %v", gotGroup.PreviousParentGroup, prev)
	}
	if len(gotGroup.CustomData) != 1 || gotGroup.CustomData[0].Key != "group.key" {
		t.Errorf("group custom data = %+v", gotGroup.CustomData)
	}
	if gotGroup.CustomData[0].LastModificationTime == nil {
		t.Error("group custom data lost its modification time")
	}

	gotEntry := gotGroup.Entries[0]
	if gotEntry.QualityCheck == nil || gotEntry.QualityCheck.Bool {
		t.Errorf("entry quality check = %v, want false", gotEntry.QualityCheck)
	}
	if gotEntry.PreviousParentGroup == nil || *gotEntry.PreviousParentGroup != prev {
		t.Errorf("entry previous parent = %v", gotEntry.PreviousParentGroup)
	}

	gotIcon := decoded.Content.Meta.CustomIcons[0]
	if gotIcon.Name != "an icon" {
		t.Errorf("icon name = %q", gotIcon.Name)
	}
	if gotIcon.LastModificationTime == nil {
		t.Error("icon lost its modification time")
	}
}

// A database that uses none of the 4.1 elements must not acquire them. This is
// what the pointer fields buy, and it would fail if they were plain values.
func TestRoundTripAddsNothingWhenUnusedKDBX41(t *testing.T) {
	db := NewDatabase(WithDatabaseKDBXVersion4())
	db.Credentials = NewPasswordCredentials(password)

	group := NewGroup()
	group.Name = "root"
	group.Entries = []Entry{NewEntry()}
	db.Content.Root = &RootData{Groups: []Group{group}}

	if err := db.LockProtectedEntries(); err != nil {
		t.Fatalf("Failed to lock entries: %s", err)
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(db); err != nil {
		t.Fatalf("Failed to encode: %s", err)
	}

	decoded := NewDatabase()
	decoded.Credentials = NewPasswordCredentials(password)
	if err := NewDecoder(bytes.NewReader(buf.Bytes())).Decode(decoded); err != nil {
		t.Fatalf("Failed to decode: %s", err)
	}

	counts := census(t, decoded.Content.RawData)
	for _, unwanted := range []string{
		"Group>Tags",
		"Group>PreviousParentGroup",
		"Entry>QualityCheck",
		"Entry>PreviousParentGroup",
	} {
		if n := counts[unwanted]; n != 0 {
			t.Errorf("%s appeared %d time(s) in a file that never used it", unwanted, n)
		}
	}
}

// census reduces the decrypted XML to a map of parent>child element paths and
// their counts. Names only — never values, so it is safe to run over real files.
func census(t *testing.T, raw []byte) map[string]int {
	t.Helper()

	i := bytes.Index(raw, []byte("<KeePassFile"))
	if i < 0 {
		t.Fatal("no KeePassFile element in the decrypted content")
	}

	counts := map[string]int{}
	stack := []string{}
	dec := xml.NewDecoder(bytes.NewReader(raw[i:]))

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Failed to walk the XML: %s", err)
		}

		switch e := tok.(type) {
		case xml.StartElement:
			path := e.Name.Local
			if len(stack) > 0 {
				path = stack[len(stack)-1] + ">" + e.Name.Local
			}
			counts[path]++
			stack = append(stack, e.Name.Local)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	return counts
}

// refTime is a fixed instant, so a failure points at the code rather than at the
// clock.
var refTime = time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)

func boolPtr(v bool) *w.BoolWrapper {
	b := w.NewBoolWrapper(v)
	return &b
}
