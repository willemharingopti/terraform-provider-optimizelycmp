// Package client is a minimal Optimizely CMP REST API client.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultHost is the CMP REST API host (the legacy api.welcomesoftware.com/v3 still works but is being retired).
	DefaultHost = "https://api.cmp.optimizely.com"
	// DefaultAPIVersion is the path version of the documented endpoints (e.g. /v3/tasks/{id}).
	DefaultAPIVersion = "v3"
	// DefaultTokenURL is the OAuth 2.0 token endpoint from the CMP OpenAPI spec, which supports both the
	// authorizationCode and clientCredentials flows. Terraform uses clientCredentials.
	DefaultTokenURL = "https://accounts.cmp.optimizely.com/o/oauth2/v1/token"

	maxResponseBytes = 8 << 20
	maxRetries       = 3
	pageSize         = 100
	maxPages         = 1000 // a safety stop: an API that never ends its pagination must not loop forever
)

// APIError is returned for non-2xx responses other than 404.
type APIError struct {
	Method, Path string
	Status       int
	Body         string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: status %d: %s", e.Method, e.Path, e.Status, truncate(e.Body, 500))
}

// ErrNotFound is returned for HTTP 404 responses.
var ErrNotFound = errors.New("not found")

// Client talks to the CMP REST API using the OAuth client-credentials flow.
type Client struct {
	baseURL      string
	tokenURL     string
	clientID     string
	clientSecret string
	http         *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time

	// The settings export is large and every settings-based resource reads it, so reads share one short-lived copy.
	// Any write clears it (see invalidateSettings).
	exportMu      sync.Mutex
	exportData    map[string]json.RawMessage
	exportFetched time.Time
}

// Option customises a Client (used by tests).
type Option func(*Client)

// WithHTTPClient replaces the HTTP client. Redirects are never followed by the client's own Do.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New builds a client. host is e.g. https://api.cmp.optimizely.com (a trailing /v3 is accepted);
// tokenURL may be empty to use DefaultTokenURL.
func New(host, tokenURL, clientID, clientSecret string, opts ...Option) (*Client, error) {
	if host == "" {
		host = DefaultHost
	}
	host = strings.TrimRight(host, "/")
	if !strings.HasSuffix(host, "/"+DefaultAPIVersion) {
		host += "/" + DefaultAPIVersion
	}
	if tokenURL == "" {
		tokenURL = DefaultTokenURL
	}
	for _, u := range []string{host, tokenURL} {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, fmt.Errorf("URL %q must be a valid https URL", u)
		}
	}
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("client_id and client_secret are required: set them in the provider block or export OPTIMIZELY_CMP_CLIENT_ID and OPTIMIZELY_CMP_CLIENT_SECRET (Terraform does not read .env files)")
	}
	c := &Client{
		baseURL:      host,
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	// Redirects carry meaning in this API (GET /users?email= answers 302), so they are handled by hand.
	hc := *c.http
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.http = &hc
	return c, nil
}

// bearer returns a cached token, refreshing it shortly before expiry. Tokens are never persisted.
func (c *Client) bearer(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.token != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second)) {
		return c.token, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		// Deliberately omit the body: it may echo credentials.
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil || tr.AccessToken == "" {
		return "", errors.New("token response was invalid")
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 300
	}
	c.token = tr.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return c.token, nil
}

// Do sends a JSON request to path (relative to the versioned base URL) and decodes a non-empty
// response into out (if non-nil). It retries 429s honouring Retry-After and refreshes the token
// once on 401. 404 maps to ErrNotFound.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	_, err := c.do(ctx, method, path, in, out, false)
	return err
}

// do is Do that also returns the response headers. A 302 is only accepted (as success, for the caller to read
// Location) when allowRedirect is set: anywhere else it is an error, so a stray redirect can never look like success.
func (c *Client) do(ctx context.Context, method, path string, in, out any, allowRedirect bool) (http.Header, error) {
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return nil, err
		}
	}

	refreshed := false
	for attempt := 0; ; attempt++ {
		tok, err := c.bearer(ctx, false)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if int64(len(raw)) >= maxResponseBytes {
			// Cut off at the limit: decoding it would fail with a confusing JSON error, or worse, succeed on part of the data.
			return nil, fmt.Errorf("%s %s: response is larger than %d bytes, which this provider refuses to read", method, path, maxResponseBytes)
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized && !refreshed:
			refreshed = true
			if _, err := c.bearer(ctx, true); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries:
			wait := time.Duration(1<<attempt) * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 && s <= 30 {
				wait = time.Duration(s) * time.Second
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case resp.StatusCode == http.StatusFound && allowRedirect:
			return resp.Header, nil // GET /users?email= answers with a redirect; the caller reads Location
		case resp.StatusCode >= 300:
			return nil, &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: string(raw)}
		}
		if method != http.MethodGet && !strings.HasPrefix(path, "/settings") {
			c.invalidateSettings() // a write elsewhere (a field, say) shows up in the export
		}
		if out != nil && len(raw) > 0 {
			return resp.Header, json.Unmarshal(raw, out)
		}
		return resp.Header, nil
	}
}

// listAll walks an offset/page_size paginated collection and returns every item.
// path may already contain a query string.
func listAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var all []T
	for pages, offset := 0, 0; ; pages, offset = pages+1, offset+pageSize {
		if pages >= maxPages {
			return nil, fmt.Errorf("GET %s: pagination did not end after %d pages", path, maxPages)
		}
		var page struct {
			Data       []T `json:"data"`
			Pagination struct {
				Next *string `json:"next"`
			} `json:"pagination"`
		}
		p := fmt.Sprintf("%s%soffset=%d&page_size=%d", path, sep, offset, pageSize)
		if err := c.Do(ctx, http.MethodGet, p, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if page.Pagination.Next == nil || *page.Pagination.Next == "" || len(page.Data) == 0 {
			return all, nil
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
