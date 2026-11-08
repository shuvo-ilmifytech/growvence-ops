// Package sheets backs project data up to a Google Sheet.
//
// It speaks the Sheets REST API directly with a service-account key: a signed JWT is
// exchanged for an access token, which is cached until it expires. That keeps the
// dependency tree small (standard library only) and makes the whole thing testable
// against a fake Google (see sheets_test.go).
package sheets

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const scope = "https://www.googleapis.com/auth/spreadsheets"

// Config is read from the environment; an empty SpreadsheetID disables the backup.
type Config struct {
	SpreadsheetID   string
	Tab             string
	CredentialsJSON []byte
	// Endpoints are overridden in tests.
	TokenURL string
	APIBase  string
}

type credentials struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// Client talks to one spreadsheet.
type Client struct {
	cfg   Config
	creds credentials
	key   *rsa.PrivateKey
	http  *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// LoadConfig reads the settings. It returns ok=false when no sheet is configured,
// which is not an error: the backup is simply switched off.
func LoadConfig(env func(string) string) (Config, bool, error) {
	c := Config{
		SpreadsheetID: strings.TrimSpace(env("SHEETS_SPREADSHEET_ID")),
		Tab:           strings.TrimSpace(env("SHEETS_TAB")),
	}
	if c.SpreadsheetID == "" {
		return c, false, nil
	}
	if c.Tab == "" {
		c.Tab = "Projects"
	}
	if raw := strings.TrimSpace(env("GOOGLE_SERVICE_ACCOUNT_JSON")); raw != "" {
		c.CredentialsJSON = []byte(raw)
	} else if file := strings.TrimSpace(env("GOOGLE_SERVICE_ACCOUNT_FILE")); file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return c, true, fmt.Errorf("service account file: %w", err)
		}
		c.CredentialsJSON = b
	} else {
		return c, true, fmt.Errorf("SHEETS_SPREADSHEET_ID is set but no service account key: set GOOGLE_SERVICE_ACCOUNT_FILE or GOOGLE_SERVICE_ACCOUNT_JSON")
	}
	return c, true, nil
}

func New(cfg Config) (*Client, error) {
	var creds credentials
	if err := json.Unmarshal(cfg.CredentialsJSON, &creds); err != nil {
		return nil, fmt.Errorf("service account key is not valid JSON: %w", err)
	}
	if creds.ClientEmail == "" || creds.PrivateKey == "" {
		return nil, fmt.Errorf("service account key needs client_email and private_key")
	}
	key, err := parseKey(creds.PrivateKey)
	if err != nil {
		return nil, err
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = creds.TokenURI
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://sheets.googleapis.com"
	}
	return &Client{cfg: cfg, creds: creds, key: key, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// ServiceAccountEmail is the address the sheet must be shared with.
func (c *Client) ServiceAccountEmail() string { return c.creds.ClientEmail }

func (c *Client) SpreadsheetID() string { return c.cfg.SpreadsheetID }
func (c *Client) Tab() string           { return c.cfg.Tab }

func parseKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("private_key is not PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private_key is not RSA")
		}
		return rsaKey, nil
	}
	k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("private_key: %w", err)
	}
	return k, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// accessToken returns a cached token, or trades a signed JWT for a fresh one.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expires) > time.Minute {
		return c.token, nil
	}
	now := time.Now()
	header := b64([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iss":   c.creds.ClientEmail,
		"scope": scope,
		"aud":   c.cfg.TokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + b64(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signing + "." + b64(sig)},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google refused the service account (%d): %s", res.StatusCode, snippet(body))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("google returned no access token: %s", snippet(body))
	}
	c.token = out.AccessToken
	c.expires = now.Add(time.Duration(max(out.ExpiresIn, 60)) * time.Second)
	return c.token, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(b)
	}
	u := c.cfg.APIBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("sheets %s (%d): %s", path, res.StatusCode, snippet(raw))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// rangeOf quotes the tab name, which may contain spaces.
func (c *Client) rangeOf(a1 string) string {
	return "'" + strings.ReplaceAll(c.cfg.Tab, "'", "''") + "'!" + a1
}

func (c *Client) valuesPath(a1 string) string {
	return "/v4/spreadsheets/" + url.PathEscape(c.cfg.SpreadsheetID) + "/values/" + url.PathEscape(c.rangeOf(a1))
}

// Column reads one column, top to bottom (used to find each project's row).
func (c *Client) Column(ctx context.Context, col string) ([]string, error) {
	var out struct {
		Values [][]string `json:"values"`
	}
	if err := c.do(ctx, "GET", c.valuesPath(col+":"+col), nil, nil, &out); err != nil {
		return nil, err
	}
	cells := make([]string, len(out.Values))
	for i, row := range out.Values {
		if len(row) > 0 {
			cells[i] = row[0]
		}
	}
	return cells, nil
}

// Update writes whole rows at fixed positions in one call. Rows are 1-based, as in the sheet.
func (c *Client) Update(ctx context.Context, rows map[int][]any, width int) error {
	if len(rows) == 0 {
		return nil
	}
	type valueRange struct {
		Range  string  `json:"range"`
		Values [][]any `json:"values"`
	}
	body := struct {
		ValueInputOption string       `json:"valueInputOption"`
		Data             []valueRange `json:"data"`
	}{ValueInputOption: "RAW"}
	last := columnName(width)
	for row, values := range rows {
		body.Data = append(body.Data, valueRange{
			Range:  c.rangeOf(fmt.Sprintf("A%d:%s%d", row, last, row)),
			Values: [][]any{values},
		})
	}
	path := "/v4/spreadsheets/" + url.PathEscape(c.cfg.SpreadsheetID) + "/values:batchUpdate"
	return c.do(ctx, "POST", path, nil, body, nil)
}

// Append adds rows at the bottom and reports the row number each one landed on.
func (c *Client) Append(ctx context.Context, rows [][]any) ([]int, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	body := struct {
		Values [][]any `json:"values"`
	}{Values: rows}
	var out struct {
		Updates struct {
			UpdatedRange string `json:"updatedRange"`
		} `json:"updates"`
	}
	q := url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}, "includeValuesInResponse": {"false"}}
	if err := c.do(ctx, "POST", c.valuesPath("A1")+":append", q, body, &out); err != nil {
		return nil, err
	}
	first, err := firstRowOf(out.Updates.UpdatedRange)
	if err != nil {
		return nil, err
	}
	numbers := make([]int, len(rows))
	for i := range rows {
		numbers[i] = first + i
	}
	return numbers, nil
}

// firstRowOf reads the row number out of a range like "Projects!A14:S15".
func firstRowOf(a1 string) (int, error) {
	if i := strings.LastIndex(a1, "!"); i >= 0 {
		a1 = a1[i+1:]
	}
	if i := strings.Index(a1, ":"); i >= 0 {
		a1 = a1[:i]
	}
	digits := strings.TrimLeft(a1, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	n := 0
	if digits == "" {
		return 0, fmt.Errorf("google returned an unreadable range %q", a1)
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("google returned an unreadable range %q", a1)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// columnName turns 1 into "A", 27 into "AA".
func columnName(n int) string {
	name := ""
	for n > 0 {
		n--
		name = string(rune('A'+n%26)) + name
		n /= 26
	}
	if name == "" {
		return "A"
	}
	return name
}
