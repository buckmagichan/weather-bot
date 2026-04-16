package openmeteo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// minimalResponse is a valid ForecastResponse JSON with current, hourly (2
// time steps), and daily (1 day) sections for use across decode tests.
const minimalResponse = `{
	"latitude": 52.52,
	"longitude": 13.42,
	"generationtime_ms": 0.5,
	"utc_offset_seconds": 3600,
	"timezone": "Europe/Berlin",
	"timezone_abbreviation": "CET",
	"elevation": 38.0,
	"current_units": {"time":"iso8601","interval":"seconds","temperature_2m":"°C","weather_code":"wmo code"},
	"current": {"time":"2026-04-13T12:00","interval":900,"temperature_2m":15.5,"apparent_temperature":12.1,"relative_humidity_2m":60,"precipitation":0.0,"weather_code":3,"wind_speed_10m":3.5,"wind_direction_10m":180},
	"hourly_units": {"time":"iso8601","temperature_2m":"°C","weather_code":"wmo code"},
	"hourly": {"time":["2026-04-13T00:00","2026-04-13T01:00"],"temperature_2m":[9.0,8.5],"weather_code":[3,3]},
	"daily_units": {"time":"iso8601","temperature_2m_max":"°C","temperature_2m_min":"°C"},
	"daily": {"time":["2026-04-13"],"temperature_2m_max":[15.5],"temperature_2m_min":[8.3]}
}`

// --- client.get tests ---

func TestClientGet_success(t *testing.T) {
	want := `{"latitude":52.52}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(want))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	got, err := c.get(context.Background(), "/forecast", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != want {
		t.Errorf("body: got %q, want %q", got, want)
	}
}

func TestClientGet_non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"reason":"bad input"}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.get(context.Background(), "/forecast", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should contain status 400: %v", err)
	}
	if !strings.Contains(err.Error(), "/forecast") {
		t.Errorf("error should contain path /forecast: %v", err)
	}
}

func TestClientGet_bodyTruncated(t *testing.T) {
	longBody := strings.Repeat("x", maxErrBodyLen*2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(longBody))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.get(context.Background(), "/forecast", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("truncated error should end with '...': %v", err)
	}
	if strings.Contains(err.Error(), longBody) {
		t.Error("full long body should not appear in error message")
	}
}

func TestWithHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	custom := &http.Client{}
	c := NewClient(WithHTTPClient(custom), WithBaseURL(srv.URL))
	if c.httpClient != custom {
		t.Error("WithHTTPClient: httpClient not replaced")
	}
	// Ensure the custom client is actually used for requests.
	if _, err := c.get(context.Background(), "/forecast", nil); err != nil {
		t.Fatalf("unexpected error with custom client: %v", err)
	}
}

func TestWithHTTPClient_nil(t *testing.T) {
	// A nil argument should be silently ignored; the default client is kept.
	c := NewClient(WithHTTPClient(nil))
	if c.httpClient == nil {
		t.Error("httpClient should not be nil after WithHTTPClient(nil)")
	}
}

func TestWithHTTPClient_timeoutOrderIndependent(t *testing.T) {
	// WithTimeout applied after WithHTTPClient must update the injected client's
	// Timeout, not be silently lost.
	custom := &http.Client{}
	want := 5 * time.Second
	c := NewClient(WithHTTPClient(custom), WithTimeout(want))
	if c.httpClient.Timeout != want {
		t.Errorf("Timeout: got %v, want %v", c.httpClient.Timeout, want)
	}
}

// --- Forecast() query param tests ---

func TestForecast_queryParams(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	_, _ = c.Forecast(context.Background(), ForecastParams{
		Latitude:          52.52,
		Longitude:         13.41,
		Current:           []string{VarTemperature2m, VarWeatherCode},
		Hourly:            []string{VarPrecipitation},
		Daily:             []string{VarTemperature2mMax},
		Timezone:          TimezoneAuto,
		ForecastDays:      3,
		PastDays:          1,
		TemperatureUnit:   TempUnitFahrenheit,
		WindSpeedUnit:     WindSpeedUnitMS,
		PrecipitationUnit: PrecipUnitInch,
	})

	checks := map[string]string{
		"latitude":           "52.52",
		"longitude":          "13.41",
		"current":            VarTemperature2m + "," + VarWeatherCode,
		"hourly":             VarPrecipitation,
		"daily":              VarTemperature2mMax,
		"timezone":           TimezoneAuto,
		"forecast_days":      "3",
		"past_days":          "1",
		"temperature_unit":   TempUnitFahrenheit,
		"wind_speed_unit":    WindSpeedUnitMS,
		"precipitation_unit": PrecipUnitInch,
	}
	for param, want := range checks {
		if got := captured.Get(param); got != want {
			t.Errorf("param %q: got %q, want %q", param, got, want)
		}
	}
}

func TestForecast_zeroOptionalParams(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	_, _ = c.Forecast(context.Background(), ForecastParams{Latitude: 1, Longitude: 1})

	for _, optional := range []string{"current", "hourly", "daily", "timezone", "forecast_days",
		"past_days", "temperature_unit", "wind_speed_unit", "precipitation_unit"} {
		if captured.Get(optional) != "" {
			t.Errorf("param %q should be absent when zero, got %q", optional, captured.Get(optional))
		}
	}
}

// --- Forecast() JSON decode test ---

func TestForecast_decodeResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalResponse))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	resp, err := c.Forecast(context.Background(), ForecastParams{Latitude: 52.52, Longitude: 13.42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Latitude != 52.52 {
		t.Errorf("Latitude: got %v, want 52.52", resp.Latitude)
	}
	if resp.Timezone != "Europe/Berlin" {
		t.Errorf("Timezone: got %q, want %q", resp.Timezone, "Europe/Berlin")
	}
	if resp.Current == nil {
		t.Fatal("Current is nil")
	}
	if resp.Current.Temperature2m != 15.5 {
		t.Errorf("Current.Temperature2m: got %v, want 15.5", resp.Current.Temperature2m)
	}
	if resp.Current.WeatherCode != 3 {
		t.Errorf("Current.WeatherCode: got %v, want 3", resp.Current.WeatherCode)
	}
	if resp.Hourly == nil {
		t.Fatal("Hourly is nil")
	}
	if len(resp.Hourly.Time) != 2 {
		t.Errorf("Hourly.Time length: got %d, want 2", len(resp.Hourly.Time))
	}
	if resp.Daily == nil {
		t.Fatal("Daily is nil")
	}
	if resp.Daily.Temperature2mMax[0] != 15.5 {
		t.Errorf("Daily.Temperature2mMax[0]: got %v, want 15.5", resp.Daily.Temperature2mMax[0])
	}
}

// --- Forecast() validation tests ---

func TestForecast_invalidLatitude(t *testing.T) {
	c := NewClient()
	_, err := c.Forecast(context.Background(), ForecastParams{Latitude: 91, Longitude: 0})
	if err == nil || !strings.Contains(err.Error(), "latitude") {
		t.Errorf("expected latitude error, got: %v", err)
	}
}

func TestForecast_invalidLongitude(t *testing.T) {
	c := NewClient()
	_, err := c.Forecast(context.Background(), ForecastParams{Latitude: 0, Longitude: -181})
	if err == nil || !strings.Contains(err.Error(), "longitude") {
		t.Errorf("expected longitude error, got: %v", err)
	}
}

func TestForecast_invalidForecastDays(t *testing.T) {
	c := NewClient()
	_, err := c.Forecast(context.Background(), ForecastParams{Latitude: 0, Longitude: 0, ForecastDays: 17})
	if err == nil || !strings.Contains(err.Error(), "ForecastDays") {
		t.Errorf("expected ForecastDays error, got: %v", err)
	}
}

func TestForecast_invalidUnits(t *testing.T) {
	cases := []struct {
		name   string
		params ForecastParams
		want   string
	}{
		{"temperature", ForecastParams{Latitude: 52.52, Longitude: 13.41, TemperatureUnit: "kelvin"}, "TemperatureUnit"},
		{"wind speed", ForecastParams{Latitude: 52.52, Longitude: 13.41, WindSpeedUnit: "furlongs"}, "WindSpeedUnit"},
		{"precipitation", ForecastParams{Latitude: 52.52, Longitude: 13.41, PrecipitationUnit: "cubits"}, "PrecipitationUnit"},
	}
	c := NewClient()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Forecast(context.Background(), tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got: %v", tc.want, err)
			}
		})
	}
}

// --- Forecast() slice length consistency test ---

func TestForecast_hourlySliceLengthMismatch(t *testing.T) {
	// temperature_2m has 1 element but time has 2 — the API should never do
	// this, but we guard against it.
	bad := `{
		"latitude":52.52,"longitude":13.42,
		"hourly": {
			"time":["2026-04-13T00:00","2026-04-13T01:00"],
			"temperature_2m":[9.0]
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bad))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.Forecast(context.Background(), ForecastParams{Latitude: 52.52, Longitude: 13.42})
	if err == nil {
		t.Fatal("expected slice length mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "hourly") {
		t.Errorf("error should mention 'hourly': %v", err)
	}
}

func TestForecast_dailySliceLengthMismatch(t *testing.T) {
	bad := `{
		"latitude":52.52,"longitude":13.42,
		"daily": {
			"time":["2026-04-13","2026-04-14"],
			"temperature_2m_max":[15.5]
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bad))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.Forecast(context.Background(), ForecastParams{Latitude: 52.52, Longitude: 13.42})
	if err == nil {
		t.Fatal("expected slice length mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "daily") {
		t.Errorf("error should mention 'daily': %v", err)
	}
}

// --- JSON round-trip sanity check ---

func TestForecastResponse_jsonRoundTrip(t *testing.T) {
	var original ForecastResponse
	if err := json.Unmarshal([]byte(minimalResponse), &original); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundtripped ForecastResponse
	if err := json.Unmarshal(b, &roundtripped); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}
	if roundtripped.Current.Temperature2m != original.Current.Temperature2m {
		t.Errorf("Temperature2m mismatch after round-trip: %v vs %v",
			roundtripped.Current.Temperature2m, original.Current.Temperature2m)
	}
}
