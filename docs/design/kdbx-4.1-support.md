# KDBX 4.1 support in gokeepasslib

> **Status:** specification, 2026-07-28. This document is the contract for the
> library change, and it doubles as the description for the upstream
> contribution. Written to be read cold.

## Why this exists

harmos writes local `.kdbx` files (see `harmos-write-model.md`). Its kdbx library,
[`github.com/tobischo/gokeepasslib/v3`](https://github.com/tobischo/gokeepasslib)
v3.6.2, models the KDBX 4.0 schema. It does **not** model the elements KDBX 4.1
added, and Go's `encoding/xml` discards unknown elements without complaint.

That combination is dangerous rather than merely limiting:

1. The file's `Signature.MinorVersion` is read from the file and **written back
   verbatim**, and `IsKdbx4()` inspects only the major version.
2. So a 4.1 file is parsed as if it were 4.0, its 4.1-only elements are dropped
   on the floor, and the result is written out **still labelled 4.1**.
3. Nothing warns anybody. The next reader sees a well-formed 4.1 file that has
   quietly lost data.

This is not hypothetical. The vault that motivated this work is a real 4.1 file
carrying 28 real `Group>PreviousParentGroup` references, 4 custom-data
timestamps and a custom-icon timestamp — all of which today's library would
discard on the first save.

## Scope

Add the elements KDBX 4.1 introduced, so that a 4.1 file survives a
decode/encode round trip unchanged. Nothing else: no reordering of existing
fields, no behaviour change for 4.0 files, no new API surface beyond the struct
fields themselves.

## The complete inventory

Every element KDBX 4.1 added, cross-checked between the
[KeePass 4.1 specification](https://keepass.info/help/kb/kdbx_4.1.html) and
KeePassXC's writer (`src/format/KdbxXmlWriter.cpp`), which is the closest thing
to executable ground truth.

| # | Element | Parent | Type | Modelled today |
|---|---|---|---|---|
| 1 | `PreviousParentGroup` | `Group` | UUID (base64) | no |
| 2 | `PreviousParentGroup` | `Entry` | UUID (base64) | no |
| 3 | `QualityCheck` | `Entry` | bool, default true | no |
| 4 | `Tags` | `Group` | string, same format as entry tags | no |
| 5 | `LastModificationTime` | `Meta > CustomData > Item` | time | no |
| 6 | `Name` | `Meta > CustomIcons > Icon` | string | no |
| 7 | `LastModificationTime` | `Meta > CustomIcons > Icon` | time | no |

One pre-existing gap surfaced alongside these and belongs in the same change,
because it is lost in exactly the same silent way:

| # | Element | Parent | Introduced | Modelled today |
|---|---|---|---|---|
| 8 | `CustomData` | `Group` | KDBX **4.0** | no |

`Entry` already has `CustomData`; `Group` never got it.

## Element order

Order matters on write. Go's `encoding/xml` emits struct fields in declaration
order, so the struct order *is* the schema order.

Taken from KeePassXC's `KdbxXmlWriter.cpp` (`writeGroup`, `writeEntry`). Elements
marked *(4.1)* are written only for 4.1 databases; *(4.0+)* only for 4.0 and
later; several are omitted entirely when null or empty.

**`Group`**

1. `UUID`
2. `Name`
3. `Notes`
4. `Tags` *(4.1)*
5. `IconID`
6. `CustomIconUUID` — omitted when null
7. `Times`
8. `IsExpanded`
9. `DefaultAutoTypeSequence`
10. `EnableAutoType`
11. `EnableSearching`
12. `LastTopVisibleEntry`
13. `CustomData` *(4.0+)*
14. `PreviousParentGroup` *(4.1)* — omitted when null
15. `Entry` *
16. `Group` *

**`Entry`**

1. `UUID`
2. `IconID`
3. `CustomIconUUID` — omitted when null
4. `ForegroundColor`
5. `BackgroundColor`
6. `OverrideURL`
7. `Tags`
8. `Times`
9. `QualityCheck` *(4.1)* — written only when the value is false
10. `PreviousParentGroup` *(4.1)* — omitted when null
11. `String` *
12. `Binary` *
13. `AutoType`
14. `CustomData` *(4.0+)*
15. `History` — only on non-history entries

### Cross-validation against a real file

The orders above were checked against a genuine KDBX 4.1 vault, by walking its
decrypted XML and recording child-element order (names only). It agreed exactly,
with the optional elements it does not use simply absent:

```
Group : UUID, Name, Notes, IconID, Times, IsExpanded, DefaultAutoTypeSequence,
        EnableAutoType, EnableSearching, LastTopVisibleEntry,
        PreviousParentGroup, Entry*, Group*
Entry : UUID, IconID, ForegroundColor, BackgroundColor, OverrideURL, Tags,
        Times, String*, AutoType, History
```

### Where gokeepasslib's existing order already differs

`Entry`'s struct declares `Values, AutoType, Histories, Binaries, CustomData`,
whereas the schema order is `String*, Binary*, AutoType, CustomData, History`.
This predates the 4.1 work, and files written that way are opened correctly by
KeePassXC and MacPass — readers dispatch on element name, not position.

**Leave it alone.** Reordering existing fields is a separate change with its own
risk, and it is not needed to stop data loss. Place the new fields at their
specified positions and no more.

## The change

### `entry.go`

```go
type Entry struct {
    UUID                UUID              `xml:"UUID"`
    IconID              int64             `xml:"IconID"`
    CustomIconUUID      UUID              `xml:"CustomIconUUID"`
    ForegroundColor     string            `xml:"ForegroundColor"`
    BackgroundColor     string            `xml:"BackgroundColor"`
    OverrideURL         string            `xml:"OverrideURL"`
    Tags                string            `xml:"Tags"`
    Times               TimeData          `xml:"Times"`
    QualityCheck        *w.BoolWrapper    `xml:"QualityCheck,omitempty"`        // KDBX 4.1
    PreviousParentGroup *UUID             `xml:"PreviousParentGroup,omitempty"` // KDBX 4.1
    Values              []ValueData       `xml:"String,omitempty"`
    AutoType            AutoTypeData      `xml:"AutoType"`
    Histories           []History         `xml:"History"`
    Binaries            []BinaryReference `xml:"Binary,omitempty"`
    CustomData          []CustomData      `xml:"CustomData>Item"`
}
```

Both new fields are **pointers**, so "absent" and "present with a zero value"
stay distinguishable. A 4.0 file must not gain a `PreviousParentGroup` element it
never had, and `QualityCheck` defaults to true — writing an explicit `false`
where the file said nothing would change meaning.

`CustomData` gains the 4.1 timestamp:

```go
type CustomData struct {
    XMLName              xml.Name       `xml:"Item"`
    Key                  string         `xml:"Key"`
    Value                string         `xml:"Value"`
    LastModificationTime *w.TimeWrapper `xml:"LastModificationTime,omitempty"` // KDBX 4.1
}
```

### `group.go`

```go
type Group struct {
    UUID                    UUID                  `xml:"UUID"`
    Name                    string                `xml:"Name"`
    Notes                   string                `xml:"Notes"`
    Tags                    string                `xml:"Tags,omitempty"`  // KDBX 4.1
    IconID                  int64                 `xml:"IconID"`
    CustomIconUUID          UUID                  `xml:"CustomIconUUID"`
    Times                   TimeData              `xml:"Times"`
    IsExpanded              w.BoolWrapper         `xml:"IsExpanded"`
    DefaultAutoTypeSequence string                `xml:"DefaultAutoTypeSequence"`
    EnableAutoType          w.NullableBoolWrapper `xml:"EnableAutoType"`
    EnableSearching         w.NullableBoolWrapper `xml:"EnableSearching"`
    LastTopVisibleEntry     string                `xml:"LastTopVisibleEntry"`
    CustomData              []CustomData          `xml:"CustomData>Item"`               // KDBX 4.0
    PreviousParentGroup     *UUID                 `xml:"PreviousParentGroup,omitempty"` // KDBX 4.1
    Entries                 []Entry               `xml:"Entry,omitempty"`
    Groups                  []Group               `xml:"Group,omitempty"`
    groupChildOrder         int                   `xml:"-"`
}
```

`Group` has a **hand-written `UnmarshalXML`** that dispatches on element name in
`unmarshalGroupToken`, so struct tags alone are not enough on the way in. Three
cases must be added there — `Tags`, `CustomData`, `PreviousParentGroup` — or the
elements are still silently dropped on read while appearing to be supported.

`Entry` uses the default unmarshaller, so its struct fields suffice.

### `meta_data.go`

```go
type CustomIcon struct {
    UUID                 UUID           `xml:"UUID"`
    Data                 string         `xml:"Data"`
    Name                 string         `xml:"Name,omitempty"`                 // KDBX 4.1
    LastModificationTime *w.TimeWrapper `xml:"LastModificationTime,omitempty"` // KDBX 4.1
}
```

### Time formatting

`TimeWrapper` serialises differently per format version — RFC3339 text for KDBX
3.1, base64 seconds for KDBX 4 — and `Database.ensureKdbxFormatVersion` walks the
tree setting each wrapper's `Formatted` flag before encoding. **Every new
`*TimeWrapper` must be reached by that walk**, or it will be written in the wrong
representation. The `setKdbxFormatVersion` methods on the affected types need to
descend into the new fields.

This is the single easiest thing to get wrong here, because it fails silently:
the file still encodes, and only a strict reader notices.

## Test plan

The bar is a **round trip that loses nothing**, not merely "it compiles".

1. **Golden-file round trip.** A committed 4.1 fixture exercising all eight
   elements: decode, encode, decode again, and assert the second decode equals
   the first field for field.
2. **XML-level proof.** Compare the element census (path → count) of the input
   and the output. Nothing may disappear. This catches elements nobody
   remembered to enumerate, which is exactly how the original bug hid.
3. **Absent stays absent.** A 4.0 file must round-trip without gaining any 4.1
   element. This is what the pointer fields buy, and it needs a test that would
   fail if they were values.
4. **Order.** Assert the written child order matches the table above, for both a
   group and an entry that populate every optional element.
5. **Time representation.** A 4.1 file's timestamps must come back byte-identical,
   proving the `Formatted` walk reaches the new wrappers.
6. **The oracle.** `keepassxc-cli` must open the written file and read the
   affected values back — our own assertions cannot certify a format we do not
   own.

## Measured outcome

Both real vaults that motivated this work — KDBX 4.1, one with 28 and one with 76
`Group>PreviousParentGroup` references, plus custom-data and custom-icon
timestamps — go from **refused** to **writable**, and the content gate proves it
by round-tripping them in memory and comparing element censuses. Nothing they
contain is lost.

The upstream test suite passes unchanged apart from four `CustomIcon` literals
that had to become keyed (see below). `golangci-lint` reports the same **50**
findings as pristine upstream: all pre-existing `goconst` noise, none introduced
here.

## Upstream contribution

`CONTRIBUTING.md` asks for an **issue first**, then a fork and a pull request
referencing it. Follow that order — do not open a PR cold.

The change matches the project's existing style: table-driven tests with
`title` + `t.Run`, `testify/assert` for struct comparison, raw-XML fixtures for
unmarshal cases, no gratuitous refactoring, no reordering of existing fields.

**One unavoidable break to declare in the PR.** Adding fields to `CustomIcon`
breaks unkeyed struct literals, and the project's own tests use four of them
(`encoder_test.go`, `meta_data_test.go`). They are updated to keyed form in the
same change. This is the normal Go consequence of extending a struct, but it is
the reviewer's call, so it belongs in the description rather than buried in a
diff.

harmos consumes it in the meantime through a `replace` directive pointing at our
branch. **When (if) upstream merges it, the `replace` is deleted and the
dependency returns to the published module** — that is the intended end state,
not a fork we mean to keep.

Until then, we own the security maintenance of the vendored copy: upstream
releases must be watched and rebased onto. That cost is accepted deliberately,
recorded here so it is not a surprise later.

## How the data loss was found

Recorded because the technique generalises, and because the production code now
uses it.

`Decoder.Decode` leaves the decrypted, decompressed content in the exported
`db.Content.RawData` — inner header followed by the plaintext XML. Slicing from
`<KeePassFile` gives the original XML, which can be walked with `encoding/xml`
and reduced to a census of element paths and counts.

Comparing that census against what the library's structs model tells you exactly
what would be lost, **without needing to enumerate the format's history in
advance**. harmos's writer uses the same census on both sides of a save: if any
element present in the original is missing from the freshly written file, the
save aborts before the rename.

Diagnosing a real vault this way needs no values, only names and counts — worth
keeping that way, since the files are people's passwords.
