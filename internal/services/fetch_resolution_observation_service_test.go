package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/providers/wunderground"
)

func TestResolutionSourceURLAddsDate(t *testing.T) {
	got := resolutionSourceURL("https://www.wunderground.com/history/daily/cn/beijing/ZBAA", "2026-05-05")
	want := "https://www.wunderground.com/history/daily/cn/beijing/ZBAA/date/2026-05-05"
	if got != want {
		t.Errorf("resolutionSourceURL: got %q, want %q", got, want)
	}
}

func TestResolutionSourceURLKeepsDatedURL(t *testing.T) {
	want := "https://www.wunderground.com/history/daily/cn/beijing/ZBAA/date/2026-05-05"
	got := resolutionSourceURL(want, "2026-05-06")
	if got != want {
		t.Errorf("resolutionSourceURL: got %q, want %q", got, want)
	}
}

func TestFetchDailyHighIgnoresCurrentObservationFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script id="app-root-state" type="application/json">{
			"current":{"b":{"temperatureMax24Hour":86},"u":"https://api.weather.com/v3/wx/observations/current?icaoCode=ZBAA&units=e&format=json"}
		}</script>`))
	}))
	defer server.Close()

	client := wunderground.NewClient(wunderground.WithHTTPClient(server.Client()))
	svc := NewFetchResolutionObservationService(client)

	_, ok, err := svc.FetchDailyHigh(context.Background(), WeatherStation{
		Code:                "ZBAA",
		ResolutionSourceURL: server.URL,
	}, "2026-05-10")
	if err != nil {
		t.Fatalf("FetchDailyHigh: %v", err)
	}
	if ok {
		t.Fatal("current observation fallback should not be treated as settlement-aligned daily high")
	}
}

func TestApplyResolutionObservedHighUsesResolutionHigh(t *testing.T) {
	summary := &domain.WeatherFeatureSummary{
		ObservedHighSoFarC: resolutionFloatPtr(28),
		GeneratedAt:        time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
		Timezone:           chinaTimezone,
	}
	ApplyResolutionObservedHigh(summary, ResolutionObservation{
		HighC:      30,
		SourceURL:  "https://example.test/history",
		SourceType: "historical_observations",
	})

	if summary.ResolutionObservedHighC == nil || *summary.ResolutionObservedHighC != 30 {
		t.Fatalf("ResolutionObservedHighC: got %v, want 30", summary.ResolutionObservedHighC)
	}
	if summary.ObservedHighSoFarC == nil || *summary.ObservedHighSoFarC != 30 {
		t.Fatalf("ObservedHighSoFarC: got %v, want 30", summary.ObservedHighSoFarC)
	}
	if summary.ResolutionSourceURL != "https://example.test/history" {
		t.Errorf("ResolutionSourceURL: got %q", summary.ResolutionSourceURL)
	}
	if summary.ResolutionSourceType != "historical_observations" {
		t.Errorf("ResolutionSourceType: got %q, want historical_observations", summary.ResolutionSourceType)
	}
}

func TestApplyResolutionObservedHighKeepsHigherMetarFloorIntraday(t *testing.T) {
	summary := &domain.WeatherFeatureSummary{
		ObservedHighSoFarC: resolutionFloatPtr(31),
		GeneratedAt:        time.Date(2026, 5, 10, 5, 10, 0, 0, time.UTC), // 13:10 local
		Timezone:           chinaTimezone,
	}
	ApplyResolutionObservedHigh(summary, ResolutionObservation{HighC: 30})

	if summary.ObservedHighSoFarC == nil || *summary.ObservedHighSoFarC != 31 {
		t.Fatalf("ObservedHighSoFarC: got %v, want 31", summary.ObservedHighSoFarC)
	}
	if summary.ResolutionObservedHighC == nil || *summary.ResolutionObservedHighC != 30 {
		t.Fatalf("ResolutionObservedHighC: got %v, want 30", summary.ResolutionObservedHighC)
	}
}

func TestApplyResolutionObservedHighCanLowerMetarFloorAfterEveningLock(t *testing.T) {
	summary := &domain.WeatherFeatureSummary{
		ObservedHighSoFarC: resolutionFloatPtr(31),
		GeneratedAt:        time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC), // 22:00 local
		Timezone:           chinaTimezone,
	}
	ApplyResolutionObservedHigh(summary, ResolutionObservation{HighC: 30})

	if summary.ObservedHighSoFarC == nil || *summary.ObservedHighSoFarC != 30 {
		t.Fatalf("ObservedHighSoFarC: got %v, want 30", summary.ObservedHighSoFarC)
	}
	if summary.ResolutionObservedHighC == nil || *summary.ResolutionObservedHighC != 30 {
		t.Fatalf("ResolutionObservedHighC: got %v, want 30", summary.ResolutionObservedHighC)
	}
}

func TestApplyResolutionObservedHighDefaultsBadTimezone(t *testing.T) {
	summary := &domain.WeatherFeatureSummary{
		ObservedHighSoFarC: resolutionFloatPtr(31),
		GeneratedAt:        time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC), // 22:00 China fallback
		Timezone:           "not/a-real-zone",
	}
	ApplyResolutionObservedHigh(summary, ResolutionObservation{HighC: 30})

	if summary.ObservedHighSoFarC == nil || *summary.ObservedHighSoFarC != 30 {
		t.Fatalf("ObservedHighSoFarC: got %v, want 30", summary.ObservedHighSoFarC)
	}
}

func resolutionFloatPtr(v float64) *float64 { return &v }
