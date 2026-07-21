package pleasant

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/pottom/harmos/internal/secret"
)

// DefaultAPIVersion is the REST version harmos targets. M1 FINDINGS: v5/v6/v7
// all respond on a 9.1.x server; v6 is the documented shape we mapped against.
const DefaultAPIVersion = "v6"

// Client talks to one Pleasant server. It is not safe for concurrent use across
// a Login and a fetch; use one Client per source.
type Client struct {
	base   string
	apiVer string
	http   *http.Client
	token  secret.Secret
}

// Option configures a Client.
type Option func(*Client)

// WithAPIVersion overrides the REST version (default v6).
func WithAPIVersion(v string) Option {
	return func(c *Client) {
		if v != "" {
			c.apiVer = v
		}
	}
}

// WithHTTPClient supplies the underlying *http.Client (for a CA bundle via
// [NewHTTPClient], or a test server's client).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// New creates a Client for baseURL (e.g. https://pps.internal.example:10001).
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		base:   strings.TrimRight(baseURL, "/"),
		apiVer: DefaultAPIVersion,
		http:   &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// NewHTTPClient builds an *http.Client with TLS verification on. If caBundle is
// non-empty its PEM certs are added to the system roots (for an internal CA);
// there is deliberately no insecure mode (spec §9).
func NewHTTPClient(caBundle string) (*http.Client, error) {
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if caBundle != "" {
		pem, err := os.ReadFile(caBundle)
		if err != nil {
			return nil, fmt.Errorf("read CA bundle: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in %s", caBundle)
		}
		tr.TLSClientConfig.RootCAs = pool
	}
	return &http.Client{Transport: tr}, nil
}

// Login exchanges username+password for an access token (grant_type=password).
// No 2FA is expected on the target account (M1 FINDINGS); a missing token in the
// response is reported as such rather than a confusing nil.
func (c *Client) Login(ctx context.Context, username string, password secret.Secret) error {
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", username)
	form.Set("password", password.Reveal())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/OAuth2/Token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("auth request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth failed: %s", statusError(resp))
	}
	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return fmt.Errorf("no access_token in response (2FA required, or bad credentials?)")
	}
	c.token = secret.New(tr.AccessToken)
	return nil
}

// IsOfflineAvailable reports whether the admin has offline packages enabled. If
// false, the whole approach is unavailable and sync must fail with a clear
// message (spec §5) rather than a confusing 4xx on OfflinePackage.
func (c *Client) IsOfflineAvailable(ctx context.Context) (bool, error) {
	var ok bool
	if err := c.getJSON(ctx, "IsOfflineAvailable", &ok); err != nil {
		return false, err
	}
	return ok, nil
}

// OfflinePackage fetches the whole vault as a ZIP and streams it to w. comment
// is recorded server-side (every fetch is audited); send a truthful one. Returns
// the number of bytes written.
func (c *Client) OfflinePackage(ctx context.Context, comment string, w io.Writer) (int64, error) {
	if c.token.IsZero() {
		return 0, fmt.Errorf("not logged in")
	}
	body, err := json.Marshal(map[string]string{"Comment": comment})
	if err != nil {
		return 0, err
	}
	req, err := c.authRequest(ctx, http.MethodPost, "OfflinePackage", strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("offline package request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("offline package failed: %s", statusError(resp))
	}
	return io.Copy(w, resp.Body)
}

// getJSON does an authenticated GET on an API path and decodes JSON into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	if c.token.IsZero() {
		return fmt.Errorf("not logged in")
	}
	req, err := c.authRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s request: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s failed: %s", path, statusError(resp))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) authRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u := fmt.Sprintf("%s/api/%s/rest/%s", c.base, c.apiVer, path)
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	// Both bare and "Bearer" forms are accepted (M1 FINDINGS); use the
	// conventional Bearer.
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	return req, nil
}

// statusError summarizes a failed response without leaking much: the status plus
// a short, bounded snippet of the body.
func statusError(resp *http.Response) string {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	s := strings.TrimSpace(string(snippet))
	if s == "" {
		return resp.Status
	}
	return fmt.Sprintf("%s: %s", resp.Status, s)
}
