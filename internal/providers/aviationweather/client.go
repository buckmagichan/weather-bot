// Package aviationweather provides a client for the Aviation Weather Center
// Data API (https://aviationweather.gov/data/api/).
package aviationweather

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL = "https://aviationweather.gov"
	defaultTimeout = 20 * time.Second
	maxErrBodyLen  = 256
)

// Client is an HTTP client for the Aviation Weather Center Data API.
type Client struct {
	httpClient *http.Client
	baseURL    string
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
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// NewClient returns a Client with sensible defaults.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// get performs a GET request to path with the given query parameters and
// returns the raw response body. HTTP 204 is treated as an empty result.
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("aviationweather: %s: parse URL: %w", path, err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("aviationweather: %s: build request: %w", path, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aviationweather: %s: do request: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("aviationweather: %s: read body: %w", path, err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return body, nil
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > maxErrBodyLen {
			snippet = snippet[:maxErrBodyLen] + "..."
		}
		return nil, fmt.Errorf("aviationweather: %s: status %d: %s", path, resp.StatusCode, snippet)
	}

	return body, nil
}
