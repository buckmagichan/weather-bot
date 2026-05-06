package wunderground

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 12 * time.Second

// Client fetches Wunderground history pages used by Polymarket resolution
// sources. It deliberately exposes only the daily whole-degree high needed by
// the market model.
type Client struct {
	httpClient *http.Client
}

const (
	SourceTypeHistoricalObservations  = "historical_observations"
	SourceTypeCurrentObservation      = "current_observation_fallback"
	SourceTypePageFallbackObservation = "page_fallback"
)

type DailyHighResult struct {
	HighC      int
	SourceType string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient injects an HTTP client, primarily for tests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// NewClient constructs a Wunderground client.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DailyHighC fetches sourceURL and extracts the current/final daily high in
// whole degrees Celsius. ok is false when the page is reachable but does not
// currently expose a usable high.
func (c *Client) DailyHighC(ctx context.Context, sourceURL string) (highC int, ok bool, err error) {
	return c.DailyHighCForStation(ctx, sourceURL, stationCodeFromURL(sourceURL))
}

// DailyHighCForStation fetches sourceURL and extracts the station-specific
// current/final daily high in whole degrees Celsius.
func (c *Client) DailyHighCForStation(
	ctx context.Context,
	sourceURL string,
	stationCode string,
) (highC int, ok bool, err error) {
	result, ok, err := c.DailyHighCForStationWithSource(ctx, sourceURL, stationCode)
	return result.HighC, ok, err
}

// DailyHighCForStationWithSource fetches the station-specific current/final
// daily high and reports which Wunderground payload supplied the value.
func (c *Client) DailyHighCForStationWithSource(
	ctx context.Context,
	sourceURL string,
	stationCode string,
) (DailyHighResult, bool, error) {
	if sourceURL == "" {
		return DailyHighResult{}, false, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return DailyHighResult{}, false, fmt.Errorf("wunderground daily high: build request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "weather-bot/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DailyHighResult{}, false, fmt.Errorf("wunderground daily high: get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DailyHighResult{}, false, fmt.Errorf("wunderground daily high: status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return DailyHighResult{}, false, fmt.Errorf("wunderground daily high: read body: %w", err)
	}
	text := html.UnescapeString(string(body))
	if historicalURL, ok := historicalObservationURL(text, sourceURL, stationCode); ok {
		if highC, ok, err := c.fetchHistoricalDailyHighC(ctx, historicalURL, stationCode); err == nil && ok {
			return DailyHighResult{HighC: highC, SourceType: SourceTypeHistoricalObservations}, true, nil
		}
	}
	if highC, ok, err := parseAppRootStateDailyHighC(text, stationCode); ok || err != nil {
		return DailyHighResult{HighC: highC, SourceType: SourceTypeCurrentObservation}, ok, err
	}
	if stationCode != "" && appRootStatePattern.MatchString(text) {
		return DailyHighResult{}, false, nil
	}
	highC, ok, err := parsePageFallbackDailyHighC(text)
	return DailyHighResult{HighC: highC, SourceType: SourceTypePageFallbackObservation}, ok, err
}

var dailyHighPatterns = []struct {
	re   *regexp.Regexp
	unit string
}{
	{regexp.MustCompile(`(?is)"(?:tempHighC|temperatureMaxC|maxTemperatureC|temperatureHighC)"\s*:\s*(-?\d+(?:\.\d+)?)`), "C"},
	{regexp.MustCompile(`(?is)"(?:tempHighF|temperatureMaxF|maxTemperatureF|temperatureHighF)"\s*:\s*(-?\d+(?:\.\d+)?)`), "F"},
	{regexp.MustCompile(`(?is)"metric"\s*:\s*\{[^{}]{0,800}"(?:tempHigh|temperatureMax|maxTemperature|temperatureHigh)"\s*:\s*(-?\d+(?:\.\d+)?)`), "C"},
	{regexp.MustCompile(`(?is)"imperial"\s*:\s*\{[^{}]{0,800}"(?:tempHigh|temperatureMax|maxTemperature|temperatureHigh)"\s*:\s*(-?\d+(?:\.\d+)?)`), "F"},
	{regexp.MustCompile(`(?is)(?:daily\s+)?(?:high|maximum)(?:\s+temperature)?[^-0-9]{0,160}(-?\d+(?:\.\d+)?)\s*°?\s*C\b`), "C"},
	{regexp.MustCompile(`(?is)(?:daily\s+)?(?:high|maximum)(?:\s+temperature)?[^-0-9]{0,160}(-?\d+(?:\.\d+)?)\s*°?\s*F\b`), "F"},
}

var genericMetricHighPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)"(?:units|unit)"\s*:\s*"(?:metric|m|c)"[^{}]{0,240}"(?:tempHigh|temperatureMax|maxTemperature|temperatureHigh)"\s*:\s*(-?\d+(?:\.\d+)?)`),
	regexp.MustCompile(`(?is)"(?:tempHigh|temperatureMax|maxTemperature|temperatureHigh)"\s*:\s*(-?\d+(?:\.\d+)?)[^{}]{0,240}"(?:units|unit)"\s*:\s*"(?:metric|m|c)"`),
}

var apiKeyPattern = regexp.MustCompile(`(?i)apiKey=([A-Za-z0-9]+)`)
var datePathPattern = regexp.MustCompile(`/date/(\d{4})-(\d{2})-(\d{2})`)

type historicalObservationPayload struct {
	Metadata struct {
		Units string `json:"units"`
	} `json:"metadata"`
	Observations []struct {
		ObsID string   `json:"obs_id"`
		Temp  *float64 `json:"temp"`
	} `json:"observations"`
}

func (c *Client) fetchHistoricalDailyHighC(
	ctx context.Context,
	sourceURL string,
	stationCode string,
) (int, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return 0, false, fmt.Errorf("wunderground historical high: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "weather-bot/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, false, fmt.Errorf("wunderground historical high: get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, false, fmt.Errorf("wunderground historical high: status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, false, fmt.Errorf("wunderground historical high: read body: %w", err)
	}
	return parseHistoricalObservationsDailyHighC(body, stationCode)
}

func parseHistoricalObservationsDailyHighC(body []byte, stationCode string) (int, bool, error) {
	var payload historicalObservationPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, false, fmt.Errorf("parse historical observations: %w", err)
	}

	unit := unitFromMetadata(payload.Metadata.Units)
	if unit == "" {
		unit = "C"
	}
	var maxTemp float64
	found := false
	for _, obs := range payload.Observations {
		if obs.Temp == nil {
			continue
		}
		if stationCode != "" && obs.ObsID != "" && !strings.EqualFold(obs.ObsID, stationCode) {
			continue
		}
		if !found || *obs.Temp > maxTemp {
			maxTemp = *obs.Temp
			found = true
		}
	}
	if !found {
		return 0, false, nil
	}
	return convertWholeC(maxTemp, unit), true, nil
}

func historicalObservationURL(pageText, sourceURL, stationCode string) (string, bool) {
	if stationCode == "" {
		stationCode = stationCodeFromURL(sourceURL)
	}
	if stationCode == "" {
		return "", false
	}

	dateMatch := datePathPattern.FindStringSubmatch(sourceURL)
	if len(dateMatch) < 4 {
		return "", false
	}
	apiKeyMatch := apiKeyPattern.FindStringSubmatch(pageText)
	if len(apiKeyMatch) < 2 {
		return "", false
	}

	countryCode := countryCodeFromHistoryURL(sourceURL)
	if countryCode == "" {
		return "", false
	}
	date := dateMatch[1] + dateMatch[2] + dateMatch[3]
	return fmt.Sprintf(
		"https://api.weather.com/v1/location/%s:9:%s/observations/historical.json?apiKey=%s&units=m&startDate=%s&endDate=%s",
		strings.ToUpper(stationCode),
		countryCode,
		apiKeyMatch[1],
		date,
		date,
	), true
}

func parseDailyHighC(body []byte) (int, bool, error) {
	return parseDailyHighCForStation(body, "")
}

func parseDailyHighCForStation(body []byte, stationCode string) (int, bool, error) {
	text := html.UnescapeString(string(body))
	if highC, ok, err := parseAppRootStateDailyHighC(text, stationCode); ok || err != nil {
		return highC, ok, err
	}
	if stationCode != "" && appRootStatePattern.MatchString(text) {
		return 0, false, nil
	}

	for _, pattern := range dailyHighPatterns {
		if highC, ok, err := parseFirstMatch(text, pattern.re, pattern.unit); ok || err != nil {
			return highC, ok, err
		}
	}

	if unit := inferredPageUnit(text); unit != "" {
		for _, re := range currentObservationHighPatterns {
			if highC, ok, err := parseFirstMatch(text, re, unit); ok || err != nil {
				return highC, ok, err
			}
		}
	}

	// Some Wunderground payloads carry generic high keys under a surrounding
	// metric page configuration instead of a per-field unit suffix.
	for _, re := range genericMetricHighPatterns {
		if highC, ok, err := parseFirstMatch(text, re, "C"); ok || err != nil {
			return highC, ok, err
		}
	}

	return 0, false, nil
}

func parsePageFallbackDailyHighC(text string) (int, bool, error) {
	for _, pattern := range dailyHighPatterns {
		if highC, ok, err := parseFirstMatch(text, pattern.re, pattern.unit); ok || err != nil {
			return highC, ok, err
		}
	}

	if unit := inferredPageUnit(text); unit != "" {
		for _, re := range currentObservationHighPatterns {
			if highC, ok, err := parseFirstMatch(text, re, unit); ok || err != nil {
				return highC, ok, err
			}
		}
	}

	// Some Wunderground payloads carry generic high keys under a surrounding
	// metric page configuration instead of a per-field unit suffix.
	for _, re := range genericMetricHighPatterns {
		if highC, ok, err := parseFirstMatch(text, re, "C"); ok || err != nil {
			return highC, ok, err
		}
	}

	return 0, false, nil
}

var appRootStatePattern = regexp.MustCompile(`(?is)<script[^>]+id=["']app-root-state["'][^>]*>(.*?)</script>`)

type appStateRecord struct {
	Body  json.RawMessage `json:"b"`
	URL   string          `json:"u"`
	Value json.RawMessage `json:"value"`
	VURL  string          `json:"url"`
}

type currentObservationPayload struct {
	TemperatureMaxSince7AM *float64 `json:"temperatureMaxSince7Am"`
	TemperatureMax24Hour   *float64 `json:"temperatureMax24Hour"`
}

func parseAppRootStateDailyHighC(text, stationCode string) (int, bool, error) {
	match := appRootStatePattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return 0, false, nil
	}

	var rawRecords map[string]json.RawMessage
	if err := json.Unmarshal([]byte(match[1]), &rawRecords); err != nil {
		return 0, false, fmt.Errorf("parse app-root-state: %w", err)
	}

	for _, rawRecord := range rawRecords {
		if len(rawRecord) == 0 || rawRecord[0] != '{' {
			continue
		}
		var record appStateRecord
		if err := json.Unmarshal(rawRecord, &record); err != nil {
			continue
		}
		recordURL := record.URL
		recordBody := record.Body
		if recordURL == "" {
			recordURL = record.VURL
		}
		if len(recordBody) == 0 {
			recordBody = record.Value
		}
		if !isCurrentObservationURL(recordURL) {
			continue
		}
		if stationCode != "" && !urlMatchesStation(recordURL, stationCode) {
			continue
		}
		unit := unitFromURL(recordURL)
		if unit == "" {
			unit = inferredPageUnit(recordURL)
		}
		if unit == "" {
			continue
		}
		highC, ok, err := parseCurrentObservationHigh(recordBody, unit)
		if ok || err != nil {
			return highC, ok, err
		}
	}

	return 0, false, nil
}

func parseCurrentObservationHigh(body json.RawMessage, unit string) (int, bool, error) {
	if len(body) == 0 {
		return 0, false, nil
	}
	var obs currentObservationPayload
	if err := json.Unmarshal(body, &obs); err != nil {
		return 0, false, fmt.Errorf("parse current observation: %w", err)
	}
	switch {
	case obs.TemperatureMaxSince7AM != nil:
		return convertWholeC(*obs.TemperatureMaxSince7AM, unit), true, nil
	case obs.TemperatureMax24Hour != nil:
		return convertWholeC(*obs.TemperatureMax24Hour, unit), true, nil
	default:
		return 0, false, nil
	}
}

var currentObservationHighPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)"temperatureMaxSince7Am"\s*:\s*(-?\d+(?:\.\d+)?)`),
	regexp.MustCompile(`(?is)"temperatureMax24Hour"\s*:\s*(-?\d+(?:\.\d+)?)`),
}

func parseFirstMatch(text string, re *regexp.Regexp, unit string) (int, bool, error) {
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return 0, false, nil
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse Wunderground high %q: %w", match[1], err)
	}
	return convertWholeC(value, unit), true, nil
}

func convertWholeC(value float64, unit string) int {
	if strings.EqualFold(unit, "F") {
		value = (value - 32) * 5 / 9
	}
	return int(math.Round(value))
}

func inferredPageUnit(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, `"units=e"`) ||
		strings.Contains(lower, `units=e`) ||
		strings.Contains(lower, `"units":"e"`) ||
		strings.Contains(lower, `"units":"imperial"`):
		return "F"
	case strings.Contains(lower, `"units=m"`) ||
		strings.Contains(lower, `units=m`) ||
		strings.Contains(lower, `"units":"m"`) ||
		strings.Contains(lower, `"units":"metric"`):
		return "C"
	default:
		return ""
	}
}

func stationCodeFromURL(sourceURL string) string {
	sourceURL = strings.TrimRight(sourceURL, "/")
	if idx := strings.LastIndex(sourceURL, "/date/"); idx >= 0 {
		sourceURL = strings.TrimRight(sourceURL[:idx], "/")
	}
	if idx := strings.LastIndex(sourceURL, "/"); idx >= 0 {
		return strings.ToUpper(sourceURL[idx+1:])
	}
	return ""
}

func isCurrentObservationURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "/wx/observations/current")
}

func urlMatchesStation(rawURL, stationCode string) bool {
	lowerURL := strings.ToLower(rawURL)
	lowerCode := strings.ToLower(stationCode)
	return strings.Contains(lowerURL, "icaocode="+lowerCode) ||
		strings.Contains(lowerURL, "icaocode%3d"+lowerCode)
}

func unitFromURL(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "units=e"):
		return "F"
	case strings.Contains(lower, "units=m"):
		return "C"
	default:
		return ""
	}
}

func unitFromMetadata(unit string) string {
	switch strings.ToLower(unit) {
	case "e", "imperial", "f":
		return "F"
	case "m", "metric", "c":
		return "C"
	default:
		return ""
	}
}

func countryCodeFromHistoryURL(sourceURL string) string {
	lower := strings.ToLower(sourceURL)
	const marker = "/history/daily/"
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	rest := sourceURL[idx+len(marker):]
	if slash := strings.Index(rest, "/"); slash >= 0 {
		return strings.ToUpper(rest[:slash])
	}
	return ""
}
