package wunderground

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDailyHighC(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
		ok   bool
	}{
		{
			name: "metric_json_high",
			body: `{"history":{"daily":{"metric":{"tempHigh":30}}}}`,
			want: 30,
			ok:   true,
		},
		{
			name: "explicit_celsius_key_rounds_whole_degree",
			body: `{"temperatureMaxC":24.6}`,
			want: 25,
			ok:   true,
		},
		{
			name: "html_celsius_high",
			body: `<div>High Temperature</div><span>27 °C</span>`,
			want: 27,
			ok:   true,
		},
		{
			name: "html_fahrenheit_high_converts_to_celsius",
			body: `<div>Daily High</div><span>86 °F</span>`,
			want: 30,
			ok:   true,
		},
		{
			name: "missing_high_is_unavailable",
			body: `<html><body>No finalized history yet</body></html>`,
			ok:   false,
		},
		{
			name: "generic_metric_payload",
			body: `{"units":"metric","temperatureMax":23.3}`,
			want: 23,
			ok:   true,
		},
		{
			name: "distant_metric_signal_does_not_unlock_generic_high",
			body: `{"units":"metric","padding":"` + strings.Repeat("x", 260) + `","temperatureMax":99}`,
			ok:   false,
		},
		{
			name: "wunderground_current_observation_imperial_since_7am",
			body: `{"url":"https://api.weather.com/v3/wx/observations/current?units=e&format=json","temperatureMax24Hour":84,"temperatureMaxSince7Am":85}`,
			want: 29,
			ok:   true,
		},
		{
			name: "wunderground_current_observation_metric_since_7am",
			body: `{"url":"https://api.weather.com/v3/wx/observations/current?units=m&format=json","temperatureMaxSince7Am":31}`,
			want: 31,
			ok:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := parseDailyHighC([]byte(tc.body))
			if err != nil {
				t.Fatalf("parseDailyHighC: %v", err)
			}
			if ok != tc.ok {
				t.Fatalf("ok: got %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("highC: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseDailyHighCForStation_UsesMatchingAppRootCurrentObservation(t *testing.T) {
	body := `<script id="app-root-state" type="application/json">{
		"__nghData__":[{"not":"a data cache record"}],
		"wrong_forecast":{"b":{"temperatureMax":[80]},"u":"https://api.weather.com/v3/wx/forecast/daily/5day?icaoCode=ZBAA&units=e"},
		"wrong_station":{"b":{"temperatureMaxSince7Am":82},"u":"https://api.weather.com/v3/wx/observations/current?icaoCode=KJFK&units=e&format=json"},
		"right_station":{"b":{"temperatureMax24Hour":84,"temperatureMaxSince7Am":85},"u":"https://api.weather.com/v3/wx/observations/current?icaoCode=ZBAA&units=e&format=json"}
	}</script>`

	got, ok, err := parseDailyHighCForStation([]byte(body), "ZBAA")
	if err != nil {
		t.Fatalf("parseDailyHighCForStation: %v", err)
	}
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if got != 29 {
		t.Errorf("highC: got %d, want 29", got)
	}
}

func TestParseDailyHighCForStation_DoesNotUseOtherStationCurrentObservation(t *testing.T) {
	body := `<script id="app-root-state" type="application/json">{
		"wrong_station":{"b":{"temperatureMaxSince7Am":82},"u":"https://api.weather.com/v3/wx/observations/current?icaoCode=KJFK&units=e&format=json"}
	}</script>`

	_, ok, err := parseDailyHighCForStation([]byte(body), "ZBAA")
	if err != nil {
		t.Fatalf("parseDailyHighCForStation: %v", err)
	}
	if ok {
		t.Fatal("ok=true, want false")
	}
}

func TestParseHistoricalObservationsDailyHighC(t *testing.T) {
	body := `{
		"metadata":{"units":"m"},
		"observations":[
			{"obs_id":"ZSQD","temp":14},
			{"obs_id":"ZSQD","temp":21},
			{"obs_id":"ZSQD","temp":20},
			{"obs_id":"ZBAA","temp":35}
		]
	}`

	got, ok, err := parseHistoricalObservationsDailyHighC([]byte(body), "ZSQD")
	if err != nil {
		t.Fatalf("parseHistoricalObservationsDailyHighC: %v", err)
	}
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if got != 21 {
		t.Errorf("highC: got %d, want 21", got)
	}
}

func TestParseHistoricalObservationsDailyHighCConvertsImperial(t *testing.T) {
	body := `{
		"metadata":{"units":"e"},
		"observations":[
			{"obs_id":"ZSQD","temp":69},
			{"obs_id":"ZSQD","temp":70}
		]
	}`

	got, ok, err := parseHistoricalObservationsDailyHighC([]byte(body), "ZSQD")
	if err != nil {
		t.Fatalf("parseHistoricalObservationsDailyHighC: %v", err)
	}
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if got != 21 {
		t.Errorf("highC: got %d, want 21", got)
	}
}

func TestHistoricalObservationURL(t *testing.T) {
	body := `<script id="app-root-state" type="application/json">{"x":{"url":"https://api.weather.com/v3/location/point?apiKey=abc123&icaoCode=ZSQD"}}</script>`
	got, ok := historicalObservationURL(body, "https://www.wunderground.com/history/daily/cn/qingdao/ZSQD/date/2026-05-05", "ZSQD")
	if !ok {
		t.Fatal("ok=false, want true")
	}
	want := "https://api.weather.com/v1/location/ZSQD:9:CN/observations/historical.json?apiKey=abc123&units=m&startDate=20260505&endDate=20260505"
	if got != want {
		t.Errorf("url: got %q, want %q", got, want)
	}
}

func TestClientDailyHighCPrefersHistoricalObservations(t *testing.T) {
	client := NewClient(WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "www.wunderground.com":
			body := `<script id="app-root-state" type="application/json">{
				"current":{"b":{"temperatureMaxSince7Am":76},"u":"https://api.weather.com/v3/wx/observations/current?icaoCode=ZSQD&units=e&format=json"},
				"location":{"url":"https://api.weather.com/v3/location/point?apiKey=abc123&icaoCode=ZSQD"}
			}</script>`
			return stringResponse(r, http.StatusOK, body), nil
		case "api.weather.com":
			if r.URL.Path != "/v1/location/ZSQD:9:CN/observations/historical.json" {
				t.Errorf("historical path: got %q", r.URL.Path)
			}
			if got := r.URL.Query().Get("startDate"); got != "20260505" {
				t.Errorf("startDate: got %q, want 20260505", got)
			}
			body := `{"metadata":{"units":"m"},"observations":[{"obs_id":"ZSQD","temp":18},{"obs_id":"ZSQD","temp":21}]}`
			return stringResponse(r, http.StatusOK, body), nil
		default:
			t.Fatalf("unexpected host %q", r.URL.Host)
			return nil, nil
		}
	})}))

	result, ok, err := client.DailyHighCForStationWithSource(
		context.Background(),
		"https://www.wunderground.com/history/daily/cn/qingdao/ZSQD/date/2026-05-05",
		"ZSQD",
	)
	if err != nil {
		t.Fatalf("DailyHighCForStation: %v", err)
	}
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if result.HighC != 21 {
		t.Errorf("highC: got %d, want 21", result.HighC)
	}
	if result.SourceType != SourceTypeHistoricalObservations {
		t.Errorf("SourceType: got %q, want %q", result.SourceType, SourceTypeHistoricalObservations)
	}
}

func TestClientDailyHighCReportsCurrentFallbackSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("missing User-Agent")
		}
		fmt.Fprint(w, `<script id="app-root-state" type="application/json">{"station":{"b":{"temperatureMaxSince7Am":88},"u":"https://api.weather.com/v3/wx/observations/current?icaoCode=ZBAA&units=e&format=json"}}</script>`)
	}))
	defer srv.Close()

	client := NewClient(WithHTTPClient(srv.Client()))
	result, ok, err := client.DailyHighCForStationWithSource(context.Background(), srv.URL, "ZBAA")
	if err != nil {
		t.Fatalf("DailyHighCForStationWithSource: %v", err)
	}
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if result.HighC != 31 {
		t.Errorf("highC: got %d, want 31", result.HighC)
	}
	if result.SourceType != SourceTypeCurrentObservation {
		t.Errorf("SourceType: got %q, want %q", result.SourceType, SourceTypeCurrentObservation)
	}
}

func TestClientDailyHighC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("missing User-Agent")
		}
		fmt.Fprint(w, `<script id="app-root-state" type="application/json">{"station":{"b":{"temperatureMaxSince7Am":88},"u":"https://api.weather.com/v3/wx/observations/current?icaoCode=ZBAA&units=e&format=json"}}</script>`)
	}))
	defer srv.Close()

	client := NewClient(WithHTTPClient(srv.Client()))
	got, ok, err := client.DailyHighCForStation(context.Background(), srv.URL, "ZBAA")
	if err != nil {
		t.Fatalf("DailyHighC: %v", err)
	}
	if !ok {
		t.Fatal("DailyHighC ok=false, want true")
	}
	if got != 31 {
		t.Errorf("highC: got %d, want 31", got)
	}
}

func TestClientDailyHighCNotAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body>loading</body></html>`)
	}))
	defer srv.Close()

	client := NewClient(WithHTTPClient(srv.Client()))
	_, ok, err := client.DailyHighC(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DailyHighC: %v", err)
	}
	if ok {
		t.Fatal("DailyHighC ok=true, want false")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func stringResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}
