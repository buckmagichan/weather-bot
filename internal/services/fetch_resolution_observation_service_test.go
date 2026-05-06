package services

import (
	"testing"

	"github.com/buckmagichan/weather-bot/internal/domain"
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

func TestApplyResolutionObservedHighUsesResolutionHigh(t *testing.T) {
	summary := &domain.WeatherFeatureSummary{
		ObservedHighSoFarC: resolutionFloatPtr(28),
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

func TestApplyResolutionObservedHighCanLowerMetarFloor(t *testing.T) {
	summary := &domain.WeatherFeatureSummary{
		ObservedHighSoFarC: resolutionFloatPtr(31),
	}
	ApplyResolutionObservedHigh(summary, ResolutionObservation{HighC: 30})

	if summary.ObservedHighSoFarC == nil || *summary.ObservedHighSoFarC != 30 {
		t.Fatalf("ObservedHighSoFarC: got %v, want 30", summary.ObservedHighSoFarC)
	}
	if summary.ResolutionObservedHighC == nil || *summary.ResolutionObservedHighC != 30 {
		t.Fatalf("ResolutionObservedHighC: got %v, want 30", summary.ResolutionObservedHighC)
	}
}

func resolutionFloatPtr(v float64) *float64 { return &v }
