package main

import (
	"math"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

func TestBuildHermesTimeout_default(t *testing.T) {
	t.Setenv("HERMES_TIMEOUT_SECONDS", "")

	if got, want := buildHermesTimeout(), 3*time.Minute; got != want {
		t.Fatalf("buildHermesTimeout default: got %v, want %v", got, want)
	}
}

func TestBuildHermesTimeout_fromEnv(t *testing.T) {
	t.Setenv("HERMES_TIMEOUT_SECONDS", "240")

	if got, want := buildHermesTimeout(), 240*time.Second; got != want {
		t.Fatalf("buildHermesTimeout from env: got %v, want %v", got, want)
	}
}

func TestBuildHermesTimeout_invalidFallsBack(t *testing.T) {
	t.Setenv("HERMES_TIMEOUT_SECONDS", "nope")

	if got, want := buildHermesTimeout(), 3*time.Minute; got != want {
		t.Fatalf("buildHermesTimeout invalid fallback: got %v, want %v", got, want)
	}
}

func TestDecorateMarketPriceSnapshotsAddsRunState(t *testing.T) {
	observedHigh := 24.0
	latestObserved := 23.0
	remainingHigh := 25.0
	trend := 2.0
	secondary := "25C"
	snapshots := []domain.MarketPriceSnapshot{
		{BucketLabel: "24C"},
		{BucketLabel: "25C"},
	}

	decorateMarketPriceSnapshots(
		snapshots,
		&domain.WeatherFeatureSummary{
			LatestForecastHighC:    24.7,
			ObservedHighSoFarC:     &observedHigh,
			LatestObservedTempC:    &latestObserved,
			RemainingForecastHighC: &remainingHigh,
			TempChangeLast3hC:      &trend,
			ObservationPoints:      18,
		},
		&domain.TemperatureBucketDistribution{
			ExpectedHighC: 24.4,
			Confidence:    0.82,
			BucketProbs: []domain.BucketProbability{
				{Label: "24C", Prob: 0.73},
				{Label: "25C", Prob: 0.27},
			},
		},
		&domain.AnalysisResult{
			PredictedBestBucket: "24C",
			SecondaryRiskBucket: &secondary,
			Confidence:          0.9,
		},
	)

	if snapshots[0].PredictedBestBucket != "24C" || snapshots[0].SecondaryRiskBucket == nil || *snapshots[0].SecondaryRiskBucket != "25C" {
		t.Fatalf("analysis buckets not copied: %#v", snapshots[0])
	}
	assertFloat(t, "analysis confidence", snapshots[0].AnalysisConfidence, 0.9)
	assertFloat(t, "model bucket probability", snapshots[0].ModelBucketProbability, 0.73)
	assertFloat(t, "expected high", snapshots[0].ExpectedHighC, 24.4)
	assertFloat(t, "observed high", snapshots[0].ObservedHighSoFarC, 24.0)
	assertFloat(t, "latest forecast high", snapshots[0].LatestForecastHighC, 24.7)
	if snapshots[0].ObservationPoints != 18 {
		t.Fatalf("ObservationPoints = %d, want 18", snapshots[0].ObservationPoints)
	}
	assertFloat(t, "second model bucket probability", snapshots[1].ModelBucketProbability, 0.27)
}

func assertFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s is nil", name)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, *got, want)
	}
}
