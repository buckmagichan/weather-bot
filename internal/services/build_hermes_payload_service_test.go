package services

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// --- Fixtures -----------------------------------------------------------------

// testSummary returns a WeatherFeatureSummary with clean integer-like floats
// suitable for most builder tests. Note: ObservedHighSoFarC (17.5) is
// intentionally above LatestForecastHighC (17.0) and PreviousForecastHighC is
// nil, so the happy-path fixture carries two sanity flags by design.
func testSummary(stationCode, targetDate string) *domain.WeatherFeatureSummary {
	high := 17.5
	latestObsAt := time.Date(2026, 4, 16, 8, 0, 0, 0, time.UTC)
	return &domain.WeatherFeatureSummary{
		StationCode:         stationCode,
		TargetDateLocal:     targetDate,
		GeneratedAt:         time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		LatestForecastHighC: 17.0,
		ObservedHighSoFarC:  &high,
		ObservationPoints:   8,
		HourlyPoints:        24,
		LatestObservationAt: &latestObsAt,
	}
}

// testDist returns a minimal TemperatureBucketDistribution for the given station and date.
func testDist(stationCode, targetDate string) *domain.TemperatureBucketDistribution {
	return &domain.TemperatureBucketDistribution{
		StationCode:     stationCode,
		TargetDateLocal: targetDate,
		GeneratedAt:     time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		ExpectedHighC:   17.2,
		Confidence:      0.75,
		BucketProbs: []domain.BucketProbability{
			{Label: "14C or below", Prob: 0.30},
			{Label: "18C", Prob: 0.40},
			{Label: "19C or above", Prob: 0.30},
		},
	}
}

// cleanSummary returns a summary that triggers no sanity flags: observed high
// is below forecast, previous forecast is set, and observations are plentiful.
func cleanSummary(stationCode, targetDate string) *domain.WeatherFeatureSummary {
	obs := 16.5 // < LatestForecastHighC
	prev := 16.8
	latestObsAt := time.Date(2026, 4, 16, 8, 0, 0, 0, time.UTC)
	return &domain.WeatherFeatureSummary{
		StationCode:           stationCode,
		TargetDateLocal:       targetDate,
		GeneratedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		LatestForecastHighC:   17.0,
		PreviousForecastHighC: &prev,
		ObservedHighSoFarC:    &obs,
		ObservationPoints:     8,
		HourlyPoints:          24,
		LatestObservationAt:   &latestObsAt,
	}
}

// --- Tests --------------------------------------------------------------------

func TestBuildHermesPayload(t *testing.T) {
	svc := NewBuildHermesPayloadService()

	t.Run("happy_path", func(t *testing.T) {
		summary := testSummary("ZSPD", "2026-04-16")
		dist := testDist("ZSPD", "2026-04-16")

		payload, err := svc.Build(summary, dist)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Top-level identity fields.
		if payload.StationCode != "ZSPD" {
			t.Errorf("StationCode: got %q, want ZSPD", payload.StationCode)
		}
		if payload.TargetDateLocal != "2026-04-16" {
			t.Errorf("TargetDateLocal: got %q, want 2026-04-16", payload.TargetDateLocal)
		}
		if payload.GeneratedAt.IsZero() {
			t.Error("GeneratedAt is zero")
		}

		// Feature summary view spot-checks.
		if !near(payload.FeatureSummary.LatestForecastHighC, 17.0) {
			t.Errorf("FeatureSummary.LatestForecastHighC: got %.4f, want 17.0",
				payload.FeatureSummary.LatestForecastHighC)
		}
		if payload.FeatureSummary.ObservationPoints != 8 {
			t.Errorf("FeatureSummary.ObservationPoints: got %d, want 8",
				payload.FeatureSummary.ObservationPoints)
		}

		// Bucket distribution view spot-checks.
		if !near(payload.BucketDistribution.ExpectedHighC, 17.2) {
			t.Errorf("BucketDistribution.ExpectedHighC: got %.4f, want 17.2",
				payload.BucketDistribution.ExpectedHighC)
		}
		if len(payload.BucketDistribution.BucketProbs) != 3 {
			t.Errorf("BucketDistribution.BucketProbs: got %d buckets, want 3",
				len(payload.BucketDistribution.BucketProbs))
		}

		// testSummary has ObservedHighSoFarC (17.5) > LatestForecastHighC (17.0)
		// and PreviousForecastHighC is nil, so exactly two flags are expected.
		wantFlags := []string{"observed_high_exceeds_latest_forecast", "missing_previous_forecast"}
		if len(payload.SanityFlags) != len(wantFlags) {
			t.Errorf("SanityFlags: got %v, want %v", payload.SanityFlags, wantFlags)
		} else {
			for i, f := range wantFlags {
				if payload.SanityFlags[i] != f {
					t.Errorf("SanityFlags[%d]: got %q, want %q", i, payload.SanityFlags[i], f)
				}
			}
		}
	})

	t.Run("station_code_mismatch", func(t *testing.T) {
		summary := testSummary("ZSPD", "2026-04-16")
		dist := testDist("ZBAA", "2026-04-16")

		_, err := svc.Build(summary, dist)
		if err == nil {
			t.Fatal("expected error for station code mismatch, got nil")
		}
	})

	t.Run("target_date_mismatch", func(t *testing.T) {
		summary := testSummary("ZSPD", "2026-04-16")
		dist := testDist("ZSPD", "2026-04-17")

		_, err := svc.Build(summary, dist)
		if err == nil {
			t.Fatal("expected error for target date mismatch, got nil")
		}
	})

	t.Run("json_has_sanity_flags_and_no_nested_generated_at", func(t *testing.T) {
		summary := testSummary("ZSPD", "2026-04-16")
		dist := testDist("ZSPD", "2026-04-16")

		payload, err := svc.Build(summary, dist)
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if !json.Valid(b) {
			t.Fatalf("output is not valid JSON: %s", b)
		}

		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		// Top-level keys must exist.
		for _, key := range []string{"station_code", "target_date_local", "generated_at",
			"feature_summary", "bucket_distribution", "sanity_flags"} {
			if _, ok := m[key]; !ok {
				t.Errorf("JSON missing top-level key: %s", key)
			}
		}

		// feature_summary must NOT contain generated_at.
		fs, ok := m["feature_summary"].(map[string]any)
		if !ok {
			t.Fatal("feature_summary is not a JSON object")
		}
		if _, has := fs["generated_at"]; has {
			t.Error("feature_summary must not contain generated_at")
		}

		// bucket_distribution must NOT contain generated_at.
		bd, ok := m["bucket_distribution"].(map[string]any)
		if !ok {
			t.Fatal("bucket_distribution is not a JSON object")
		}
		if _, has := bd["generated_at"]; has {
			t.Error("bucket_distribution must not contain generated_at")
		}

		// sanity_flags must be a JSON array (never null).
		if _, ok := m["sanity_flags"].([]any); !ok {
			t.Errorf("sanity_flags is not a JSON array: %T %v", m["sanity_flags"], m["sanity_flags"])
		}
	})

	// --- Rounding tests -------------------------------------------------------

	t.Run("numeric_rounding_applied", func(t *testing.T) {
		// Construct a summary with deliberately noisy IEEE-754 values.
		noisyHigh := 0.9000000000000021
		noisyTrend := -1.0999999999999996
		prev := 16.1
		summary := &domain.WeatherFeatureSummary{
			StationCode:           "ZSPD",
			TargetDateLocal:       "2026-04-16",
			GeneratedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			LatestForecastHighC:   17.985,
			PreviousForecastHighC: &prev,
			ForecastTrendC:        &noisyTrend,
			ObservedHighSoFarC:    &noisyHigh,
			ObservationPoints:     8,
			HourlyPoints:          24,
		}
		dist := &domain.TemperatureBucketDistribution{
			StationCode:     "ZSPD",
			TargetDateLocal: "2026-04-16",
			GeneratedAt:     time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			ExpectedHighC:   17.985,
			Confidence:      0.9000000000000021,
			BucketProbs: []domain.BucketProbability{
				{Label: "below", Prob: 0.30000000000000004},
				{Label: "on",    Prob: 0.39999999999999997},
				{Label: "above", Prob: 0.30000000000000004},
			},
		}

		payload, err := svc.Build(summary, dist)
		if err != nil {
			t.Fatalf("build: %v", err)
		}

		// Temperatures rounded to 2 dp.
		// 17.985 * 100 = 1798.5 → Round → 1799 → /100 = 17.99
		if !near(payload.FeatureSummary.LatestForecastHighC, 17.99) {
			t.Errorf("LatestForecastHighC: got %.6f, want 17.99",
				payload.FeatureSummary.LatestForecastHighC)
		}
		// -1.0999999999999996 → round2 → -1.10
		if payload.FeatureSummary.ForecastTrendC == nil || !near(*payload.FeatureSummary.ForecastTrendC, -1.10) {
			t.Errorf("ForecastTrendC: got %v, want -1.10", payload.FeatureSummary.ForecastTrendC)
		}
		// 0.9000000000000021 → round2 → 0.90
		if payload.FeatureSummary.ObservedHighSoFarC == nil || !near(*payload.FeatureSummary.ObservedHighSoFarC, 0.90) {
			t.Errorf("ObservedHighSoFarC: got %v, want 0.90", payload.FeatureSummary.ObservedHighSoFarC)
		}
		// ExpectedHighC and Confidence rounded to 2 dp.
		if !near(payload.BucketDistribution.ExpectedHighC, 17.99) {
			t.Errorf("ExpectedHighC: got %.6f, want 17.99", payload.BucketDistribution.ExpectedHighC)
		}
		if !near(payload.BucketDistribution.Confidence, 0.90) {
			t.Errorf("Confidence: got %.6f, want 0.90", payload.BucketDistribution.Confidence)
		}
		// Probabilities rounded to 4 dp.
		// 0.30000000000000004 → round4 → 0.3
		if !near(payload.BucketDistribution.BucketProbs[0].Prob, 0.3) {
			t.Errorf("BucketProbs[0].Prob: got %.8f, want 0.3", payload.BucketDistribution.BucketProbs[0].Prob)
		}
	})

	// --- Sanity flag tests ----------------------------------------------------

	t.Run("sanity_flags_none_when_all_clean", func(t *testing.T) {
		payload, err := svc.Build(cleanSummary("ZSPD", "2026-04-16"), testDist("ZSPD", "2026-04-16"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if len(payload.SanityFlags) != 0 {
			t.Errorf("SanityFlags: want empty, got %v", payload.SanityFlags)
		}
	})

	t.Run("sanity_flags_observed_high_exceeds_forecast", func(t *testing.T) {
		s := cleanSummary("ZSPD", "2026-04-16")
		high := s.LatestForecastHighC + 1.0 // force observed > forecast
		s.ObservedHighSoFarC = &high

		payload, err := svc.Build(s, testDist("ZSPD", "2026-04-16"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if !containsFlag(payload.SanityFlags, "observed_high_exceeds_latest_forecast") {
			t.Errorf("SanityFlags: want observed_high_exceeds_latest_forecast, got %v", payload.SanityFlags)
		}
	})

	t.Run("sanity_flags_missing_previous_forecast", func(t *testing.T) {
		s := cleanSummary("ZSPD", "2026-04-16")
		s.PreviousForecastHighC = nil // remove previous forecast

		payload, err := svc.Build(s, testDist("ZSPD", "2026-04-16"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if !containsFlag(payload.SanityFlags, "missing_previous_forecast") {
			t.Errorf("SanityFlags: want missing_previous_forecast, got %v", payload.SanityFlags)
		}
	})

	t.Run("sanity_flags_no_observation_data", func(t *testing.T) {
		s := cleanSummary("ZSPD", "2026-04-16")
		s.ObservationPoints = 0
		s.ObservedHighSoFarC = nil
		s.LatestObservedTempC = nil
		s.LatestObservationAt = nil

		payload, err := svc.Build(s, testDist("ZSPD", "2026-04-16"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if !containsFlag(payload.SanityFlags, "no_observation_data") {
			t.Errorf("SanityFlags: want no_observation_data, got %v", payload.SanityFlags)
		}
		// limited_observation_coverage must NOT appear when count is 0.
		if containsFlag(payload.SanityFlags, "limited_observation_coverage") {
			t.Error("SanityFlags: limited_observation_coverage must not appear when ObservationPoints == 0")
		}
	})

	t.Run("sanity_flags_limited_observation_coverage", func(t *testing.T) {
		s := cleanSummary("ZSPD", "2026-04-16")
		s.ObservationPoints = 3 // 0 < 3 < 6 → limited

		payload, err := svc.Build(s, testDist("ZSPD", "2026-04-16"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if !containsFlag(payload.SanityFlags, "limited_observation_coverage") {
			t.Errorf("SanityFlags: want limited_observation_coverage, got %v", payload.SanityFlags)
		}
	})
}

// containsFlag reports whether flag appears in flags.
func containsFlag(flags []string, flag string) bool {
	return slices.Contains(flags, flag)
}
