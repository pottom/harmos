// Package pleasant is the only package that knows Pleasant Password Server
// exists. It authenticates, fetches the OfflinePackage, and maps it to a kdbx
// cache. Everything downstream reads the kdbx and knows nothing about Pleasant
// (spec §2).
package pleasant

// TokenResponse is the /OAuth2/Token reply. No refresh token is issued (M1
// FINDINGS); the token TTL is ~3900s.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// The OfflinePackage is a ZIP (M1 FINDINGS, correcting spec §5): a single
// "<guid>.json" manifest of the shape below, plus one binary file per
// attachment named by its AttachmentId. FileData is never inlined in the JSON.

// Tag is a credential/folder tag: an object with a Name, not a bare string.
type Tag struct {
	Name string
}

// Attachment references a file. The bytes live in the zip entry named
// AttachmentId; FileData is always null in the manifest, so it is not modeled.
type Attachment struct {
	AttachmentId       string
	CredentialObjectId string
	FileName           string
	FileSize           int64
}

// Credential is one entry.
type Credential struct {
	Id       string
	Name     string
	Username string
	Password string
	Url      string
	Notes    string
	GroupId  string
	Created  string
	Modified string
	Expires  *string
	Tags     []Tag

	CustomUserFields        map[string]any
	CustomApplicationFields map[string]any

	// Native TOTP fields (M1 FINDINGS): present on every credential, rarely set.
	TOTPSecret string
	TOTPIssuer string
	TOTPDigits int
	TOTPPeriod int

	Attachments []Attachment

	HasViewEntryContentsAccess bool
}

// Folder is a group in the tree.
type Folder struct {
	Id       string
	Name     string
	Notes    string
	ParentId string
	Created  string
	Modified string
	Expires  *string
	Tags     []Tag

	CustomUserFields        map[string]any
	CustomApplicationFields map[string]any

	HasViewEntryContentsAccess bool

	Children    []Folder
	Credentials []Credential
}

// Package is the parsed manifest.
type Package struct {
	Root        Folder
	Attachments []Attachment
	Expiry      string
}
