// Package openmeteo provides a client for the Open-Meteo weather API
// (https://api.open-meteo.com/v1).
package openmeteo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL = "https://api.open-meteo.com/v1"
	defaultTimeout = 10 * time.Second
	// maxErrBodyLen caps the body snippet included in non-200 error messages
	// to avoid flooding logs with large HTML/JSON error responses.
	maxErrBodyLen = 256
)

// Client is an HTTP client for the Open-Meteo API.
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

// WithHTTPClient replaces the underlying *http.Client, allowing callers to
// inject custom transports, middleware, or tracing. A nil value is ignored and
// the default client is kept. WithTimeout applied after this option will update
// the injected client's Timeout, so option order does not matter for timeouts.
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
// returns the raw response body. Non-200 status codes are returned as errors;
// the body snippet in the error is capped at maxErrBodyLen to keep logs clean.
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("openmeteo: %s: parse URL: %w", path, err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("openmeteo: %s: build request: %w", path, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openmeteo: %s: do request: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openmeteo: %s: read body: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > maxErrBodyLen {
			snippet = snippet[:maxErrBodyLen] + "..."
		}
		return nil, fmt.Errorf("openmeteo: %s: status %d: %s", path, resp.StatusCode, snippet)
	}

	return body, nil
}
