package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

const (
	defaultGammaBaseURL = "https://gamma-api.polymarket.com"
	defaultCLOBBaseURL  = "https://clob.polymarket.com"
)

var ErrEventNotFound = errors.New("polymarket event not found")

// Client fetches Polymarket temperature bucket metadata from Gamma and
// tradable order-book prices from the CLOB API.
type Client struct {
	httpClient   *http.Client
	gammaBaseURL string
	clobBaseURL  string
}

// Option customizes a Polymarket client.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithGammaBaseURL overrides the Gamma API base URL.
func WithGammaBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.gammaBaseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// WithCLOBBaseURL overrides the CLOB API base URL.
func WithCLOBBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.clobBaseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// NewClient creates a Polymarket API client.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
		gammaBaseURL: defaultGammaBaseURL,
		clobBaseURL:  defaultCLOBBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchEventPriceSnapshots returns one row-ready snapshot per YES temperature
// bucket for the event slug. CLOB enrichment is best-effort: if order-book
// endpoints fail or a market has no book, Gamma prices are still returned.
func (c *Client) FetchEventPriceSnapshots(
	ctx context.Context,
	stationCode string,
	targetDateLocal string,
	eventSlug string,
	capturedAt time.Time,
) ([]domain.MarketPriceSnapshot, error) {
	if eventSlug == "" {
		return nil, nil
	}

	events, err := c.fetchGammaEvents(ctx, eventSlug)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrEventNotFound
	}

	event, err := exactSlugEvent(events, eventSlug)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.MarketPriceSnapshot, 0, len(event.Markets))
	yesTokenIDs := make([]string, 0, len(event.Markets))

	for _, market := range event.Markets {
		tokens := parseStringArray(market.CLOBTokenIDs)
		if len(tokens) == 0 || strings.TrimSpace(tokens[0]) == "" {
			continue
		}

		outcomePrices := parseFloatArray(market.OutcomePrices)
		snap := domain.MarketPriceSnapshot{
			StationCode:     stationCode,
			TargetDateLocal: targetDateLocal,
			CapturedAt:      capturedAt.UTC(),
			EventSlug:       eventSlug,
			EventID:         event.ID.String(),
			MarketID:        market.ID.String(),
			MarketSlug:      market.Slug,
			BucketLabel:     normalizeBucketLabel(market.GroupItemTitle),
			YesTokenID:      tokens[0],
			Active:          market.Active,
			Closed:          market.Closed,
		}
		if snap.BucketLabel == "" {
			snap.BucketLabel = market.Question
		}
		if len(tokens) > 1 {
			snap.NoTokenID = tokens[1]
		}
		if len(outcomePrices) > 0 {
			snap.GammaYesPrice = floatPtr(outcomePrices[0])
		}
		if len(outcomePrices) > 1 {
			snap.GammaNoPrice = floatPtr(outcomePrices[1])
		}
		snap.BestBid = market.BestBid.Ptr()
		snap.BestAsk = market.BestAsk.Ptr()
		snap.LastTradePrice = market.LastTradePrice.Ptr()
		snap.Volume = market.Volume.Ptr()
		snap.Liquidity = market.Liquidity.Ptr()

		snapshots = append(snapshots, snap)
		yesTokenIDs = append(yesTokenIDs, snap.YesTokenID)
	}

	if len(snapshots) == 0 {
		return nil, nil
	}

	c.enrichWithCLOB(ctx, snapshots, yesTokenIDs)
	return snapshots, nil
}

func exactSlugEvent(events []gammaEvent, eventSlug string) (gammaEvent, error) {
	var matches []gammaEvent
	for _, event := range events {
		if event.Slug == eventSlug {
			matches = append(matches, event)
		}
	}
	if len(matches) == 0 {
		return gammaEvent{}, ErrEventNotFound
	}
	if len(matches) > 1 {
		return gammaEvent{}, fmt.Errorf("polymarket gamma returned %d exact matches for slug %q", len(matches), eventSlug)
	}
	return matches[0], nil
}

func (c *Client) fetchGammaEvents(ctx context.Context, eventSlug string) ([]gammaEvent, error) {
	u, err := url.Parse(c.gammaBaseURL + "/events")
	if err != nil {
		return nil, fmt.Errorf("polymarket gamma URL: %w", err)
	}
	q := u.Query()
	q.Set("slug", eventSlug)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("polymarket gamma request: %w", err)
	}
	req.Header.Set("accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("polymarket gamma fetch %s: %w", eventSlug, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("polymarket gamma fetch %s: status %d: %s", eventSlug, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var events []gammaEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, fmt.Errorf("polymarket gamma decode %s: %w", eventSlug, err)
	}
	return events, nil
}

func (c *Client) enrichWithCLOB(ctx context.Context, snapshots []domain.MarketPriceSnapshot, yesTokenIDs []string) {
	prices, err := c.fetchCLOBPrices(ctx, yesTokenIDs)
	if err == nil {
		for i := range snapshots {
			tokenPrices := prices[snapshots[i].YesTokenID]
			if buy, ok := tokenPrices["BUY"]; ok {
				snapshots[i].BestBid = floatPtr(buy)
			}
			if sell, ok := tokenPrices["SELL"]; ok {
				snapshots[i].BestAsk = floatPtr(sell)
			}
		}
	} else if ctx.Err() != nil {
		return
	} else {
		log.Printf("polymarket clob /prices enrichment failed: %v — using Gamma price fields", err)
	}
	if ctx.Err() != nil {
		return
	}

	midpoints, err := c.fetchCLOBScalarMap(ctx, "/midpoints", yesTokenIDs)
	if err == nil {
		for i := range snapshots {
			if v, ok := midpoints[snapshots[i].YesTokenID]; ok {
				snapshots[i].Midpoint = floatPtr(v)
			}
		}
	} else if ctx.Err() != nil {
		return
	} else {
		log.Printf("polymarket clob /midpoints enrichment failed: %v — leaving midpoint empty", err)
	}
	if ctx.Err() != nil {
		return
	}

	spreads, err := c.fetchCLOBScalarMap(ctx, "/spreads", yesTokenIDs)
	if err == nil {
		for i := range snapshots {
			if v, ok := spreads[snapshots[i].YesTokenID]; ok {
				snapshots[i].Spread = floatPtr(v)
			}
		}
	} else if ctx.Err() != nil {
		return
	} else {
		log.Printf("polymarket clob /spreads enrichment failed: %v — leaving spread empty", err)
	}
}

func (c *Client) fetchCLOBPrices(ctx context.Context, tokenIDs []string) (map[string]map[string]float64, error) {
	reqBody := make([]clobPriceRequest, 0, len(tokenIDs)*2)
	for _, tokenID := range tokenIDs {
		reqBody = append(reqBody,
			clobPriceRequest{TokenID: tokenID, Side: "BUY"},
			clobPriceRequest{TokenID: tokenID, Side: "SELL"},
		)
	}

	var raw map[string]map[string]json.RawMessage
	if err := c.postCLOBJSON(ctx, "/prices", reqBody, &raw); err != nil {
		return nil, err
	}

	out := make(map[string]map[string]float64, len(raw))
	for tokenID, sides := range raw {
		out[tokenID] = make(map[string]float64, len(sides))
		for side, rawPrice := range sides {
			if price, ok := parseFlexibleFloat(rawPrice); ok {
				out[tokenID][side] = price
			}
		}
	}
	return out, nil
}

func (c *Client) fetchCLOBScalarMap(ctx context.Context, path string, tokenIDs []string) (map[string]float64, error) {
	reqBody := make([]clobTokenRequest, 0, len(tokenIDs))
	for _, tokenID := range tokenIDs {
		reqBody = append(reqBody, clobTokenRequest{TokenID: tokenID})
	}

	var raw map[string]json.RawMessage
	if err := c.postCLOBJSON(ctx, path, reqBody, &raw); err != nil {
		return nil, err
	}

	out := make(map[string]float64, len(raw))
	for tokenID, rawValue := range raw {
		if v, ok := parseFlexibleFloat(rawValue); ok {
			out[tokenID] = v
		}
	}
	return out, nil
}

func (c *Client) postCLOBJSON(ctx context.Context, path string, requestBody any, responseBody any) error {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("polymarket clob marshal %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.clobBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("polymarket clob request %s: %w", path, err)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("polymarket clob fetch %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("polymarket clob fetch %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("polymarket clob decode %s: %w", path, err)
	}
	return nil
}

type gammaEvent struct {
	ID      flexibleString `json:"id"`
	Slug    string         `json:"slug"`
	Markets []gammaMarket  `json:"markets"`
}

type gammaMarket struct {
	ID             flexibleString  `json:"id"`
	Question       string          `json:"question"`
	Slug           string          `json:"slug"`
	GroupItemTitle string          `json:"groupItemTitle"`
	OutcomePrices  json.RawMessage `json:"outcomePrices"`
	CLOBTokenIDs   json.RawMessage `json:"clobTokenIds"`
	BestBid        nullableFloat   `json:"bestBid"`
	BestAsk        nullableFloat   `json:"bestAsk"`
	LastTradePrice nullableFloat   `json:"lastTradePrice"`
	Volume         nullableFloat   `json:"volume"`
	Liquidity      nullableFloat   `json:"liquidity"`
	Active         bool            `json:"active"`
	Closed         bool            `json:"closed"`
}

type clobPriceRequest struct {
	TokenID string `json:"token_id"`
	Side    string `json:"side"`
}

type clobTokenRequest struct {
	TokenID string `json:"token_id"`
}

type flexibleString string

func (s *flexibleString) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		*s = flexibleString(str)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(raw, &num); err == nil {
		*s = flexibleString(num.String())
		return nil
	}
	return fmt.Errorf("decode flexible string: %s", string(raw))
}

func (s flexibleString) String() string {
	return string(s)
}

type nullableFloat struct {
	Value float64
	Set   bool
}

func (f *nullableFloat) UnmarshalJSON(raw []byte) error {
	if v, ok := parseFlexibleFloat(raw); ok {
		f.Value = v
		f.Set = true
		return nil
	}
	f.Value = 0
	f.Set = false
	if isJSONNullOrEmptyString(raw) {
		return nil
	}
	return fmt.Errorf("decode nullable float: %s", string(raw))
}

func isJSONNullOrEmptyString(raw []byte) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == `""` {
		return true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && strings.TrimSpace(s) == "" {
		return true
	}
	return false
}

func (f nullableFloat) Ptr() *float64 {
	if !f.Set {
		return nil
	}
	return floatPtr(f.Value)
}

func parseStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		if err := json.Unmarshal([]byte(encoded), &arr); err != nil {
			return nil
		}
		return arr
	}
	return nil
}

func parseFloatArray(raw json.RawMessage) []float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []float64
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var rawArr []json.RawMessage
	if err := json.Unmarshal(raw, &rawArr); err == nil {
		out := make([]float64, 0, len(rawArr))
		for _, rawValue := range rawArr {
			if v, ok := parseFlexibleFloat(rawValue); ok {
				out = append(out, v)
			} else {
				return nil
			}
		}
		return out
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		var rawArr []json.RawMessage
		if err := json.Unmarshal([]byte(encoded), &rawArr); err != nil {
			return nil
		}
		out := make([]float64, 0, len(rawArr))
		for _, rawValue := range rawArr {
			if v, ok := parseFlexibleFloat(rawValue); ok {
				out = append(out, v)
			} else {
				return nil
			}
		}
		return out
	}
	return nil
}

func parseFlexibleFloat(raw json.RawMessage) (float64, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == `""` {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v, err == nil
	}
	return 0, false
}

func normalizeBucketLabel(title string) string {
	label := strings.TrimSpace(title)
	label = strings.ReplaceAll(label, "°C", "C")
	label = strings.ReplaceAll(label, "℃", "C")
	label = strings.ReplaceAll(label, " C", "C")
	return label
}

func floatPtr(v float64) *float64 {
	return &v
}
