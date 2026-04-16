// Package meteostat provides a client for the Meteostat weather API via RapidAPI
// (https://meteostat.p.rapidapi.com).
package meteostat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL = "https://meteostat.p.rapidapi.com"
	rapidAPIHost   = "meteostat.p.rapidapi.com"
	defaultTimeout = 10 * time.Second
	maxErrBodyLen  = 256
)

// Client is an HTTP client for the Meteostat API authenticated via RapidAPI.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout overrides the default HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func WithBaseURL(u string) Option {
	return func(c *Client) {
		c.baseURL = u
	}
}

// WithHTTPClient replaces the underlying *http.Client. A nil value is ignored.
// WithTimeout applied after this option updates the injected client's Timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// NewClient returns a Client authenticated with apiKey.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// get performs an authenticated GET request to path and returns the raw body.
// Non-200 status codes are returned as errors with the body capped at maxErrBodyLen.
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("meteostat: %s: parse URL: %w", path, err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("meteostat: %s: build request: %w", path, err)
	}
	req.Header.Set("x-rapidapi-key", c.apiKey)
	req.Header.Set("x-rapidapi-host", rapidAPIHost)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meteostat: %s: do request: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("meteostat: %s: read body: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > maxErrBodyLen {
			snippet = snippet[:maxErrBodyLen] + "..."
		}
		return nil, fmt.Errorf("meteostat: %s: status %d: %s", path, resp.StatusCode, snippet)
	}

	return body, nil
}
