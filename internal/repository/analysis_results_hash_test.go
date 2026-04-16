package repository

// Internal tests for computeAnalysisContentHash and stripGeneratedAt.
// These run without a live database so they execute in every CI environment.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// baseRecord returns a fully-populated AnalysisPersistenceRecord with stable
// values for use in hash tests.
func baseRecord() *domain.AnalysisPersistenceRecord {
	secondary := "19C"
	return &domain.AnalysisPersistenceRecord{
		StationCode:     "ZSPD",
		TargetDateLocal: "2099-01-01",
		GeneratedAt:     time.Date(2099, 1, 1, 10, 0, 0, 0, time.UTC),
		Analysis: &domain.AnalysisResult{
			PredictedBestBucket: "18C",
			SecondaryRiskBucket: &secondary,
			Confidence:          0.9,
			KeyReasons:          []string{"reason one", "reason two"},
			RiskFlags:           []string{"observed_high_exceeds_latest_forecast"},
			NextCheckInMinutes:  60,
		},
		Summary: &domain.WeatherFeatureSummary{
			StationCode:         "ZSPD",
			TargetDateLocal:     "2099-01-01",
			GeneratedAt:         time.Date(2099, 1, 1, 10, 0, 0, 0, time.UTC),
			LatestForecastHighC: 18.0,
			ObservationPoints:   10,
			HourlyPoints:        24,
		},
		Distribution: &domain.TemperatureBucketDistribution{
			StationCode:     "ZSPD",
			TargetDateLocal: "2099-01-01",
			GeneratedAt:     time.Date(2099, 1, 1, 10, 0, 0, 0, time.UTC),
			ExpectedHighC:   18.1,
			Confidence:      0.9,
			BucketProbs: []domain.BucketProbability{
				{Label: "17C or below", Prob: 0.20},
				{Label: "18C", Prob: 0.50},
				{Label: "19C or above", Prob: 0.30},
			},
		},
		// Realistic Hermes payload including a generated_at that will vary per run.
		HermesPayloadJSON: json.RawMessage(`{
			"station_code":"ZSPD",
			"target_date_local":"2099-01-01",
			"generated_at":"2099-01-01T10:00:00Z",
			"feature_summary":{"latest_forecast_high_c":18.0,"observation_points":10,"hourly_points":24},
			"bucket_distribution":{"expected_high_c":18.1,"confidence":0.9,"bucket_probs":[{"label":"18C","prob":0.5}]},
			"sanity_flags":[]
		}`),
	}
}

// hashOf serialises the sub-fields and returns the content hash for rec.
func hashOf(t *testing.T, rec *domain.AnalysisPersistenceRecord) string {
	t.Helper()
	riskFlagsJSON, err := toJSONB(rec.Analysis.RiskFlags)
	if err != nil {
		t.Fatalf("toJSONB risk_flags: %v", err)
	}
	h, err := computeAnalysisContentHash(rec, riskFlagsJSON)
	if err != nil {
		t.Fatalf("computeAnalysisContentHash: %v", err)
	}
	return h
}

// ---- stripGeneratedAt -------------------------------------------------------

func TestStripGeneratedAt_removes_key(t *testing.T) {
	raw := json.RawMessage(`{"station_code":"ZSPD","generated_at":"2099-01-01T10:00:00Z","other":"value"}`)
	got, err := stripGeneratedAt(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := m["generated_at"]; ok {
		t.Error("generated_at must be absent after stripping")
	}
	if _, ok := m["station_code"]; !ok {
		t.Error("station_code must be preserved after stripping")
	}
}

func TestStripGeneratedAt_idempotent(t *testing.T) {
	raw := json.RawMessage(`{"station_code":"ZSPD","other":"value"}`) // no generated_at
	got, err := stripGeneratedAt(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if len(m) != 2 {
		t.Errorf("want 2 keys, got %d: %v", len(m), m)
	}
}

// ---- computeAnalysisContentHash ---------------------------------------------

func TestAnalysisContentHash_stable(t *testing.T) {
	rec := baseRecord()
	h1 := hashOf(t, rec)
	h2 := hashOf(t, rec)
	if h1 != h2 {
		t.Errorf("hash is not stable: first=%q second=%q", h1, h2)
	}
}

func TestAnalysisContentHash_stable_across_run_timestamps(t *testing.T) {
	// Root-cause regression test: two runs with the same weather inputs but
	// different generated_at timestamps in all JSONB blobs must produce the
	// same hash so ON CONFLICT correctly deduplicates them.
	rec1 := baseRecord()
	rec2 := baseRecord()

	// Simulate a second run: advance all generated_at timestamps.
	rec2.GeneratedAt = rec2.GeneratedAt.Add(2 * time.Hour)
	rec2.Summary.GeneratedAt = rec2.Summary.GeneratedAt.Add(2 * time.Hour)
	rec2.Distribution.GeneratedAt = rec2.Distribution.GeneratedAt.Add(2 * time.Hour)
	rec2.HermesPayloadJSON = json.RawMessage(`{
		"station_code":"ZSPD",
		"target_date_local":"2099-01-01",
		"generated_at":"2099-01-01T12:00:00Z",
		"feature_summary":{"latest_forecast_high_c":18.0,"observation_points":10,"hourly_points":24},
		"bucket_distribution":{"expected_high_c":18.1,"confidence":0.9,"bucket_probs":[{"label":"18C","prob":0.5}]},
		"sanity_flags":[]
	}`)

	h1 := hashOf(t, rec1)
	h2 := hashOf(t, rec2)
	if h1 != h2 {
		t.Errorf("hash must not change between runs with the same weather data:\nrun1=%q\nrun2=%q", h1, h2)
	}
}

func TestAnalysisContentHash_generated_at_excluded(t *testing.T) {
	// Scalar generated_at must not affect the hash.
	rec := baseRecord()
	h1 := hashOf(t, rec)
	rec.GeneratedAt = rec.GeneratedAt.Add(48 * time.Hour)
	h2 := hashOf(t, rec)
	if h1 != h2 {
		t.Errorf("hash must not change when only rec.GeneratedAt changes: before=%q after=%q", h1, h2)
	}
}

func TestAnalysisContentHash_changes_on_predicted_bucket(t *testing.T) {
	rec := baseRecord()
	h1 := hashOf(t, rec)
	rec.Analysis.PredictedBestBucket = "19C"
	h2 := hashOf(t, rec)
	if h1 == h2 {
		t.Error("hash must change when PredictedBestBucket changes")
	}
}

func TestAnalysisContentHash_stable_across_different_confidence(t *testing.T) {
	// Regression test: LLM confidence drifts between runs (e.g. 0.75 vs 0.80)
	// for identical inputs. confidence is excluded from the hash so this does
	// not produce duplicate rows.
	rec := baseRecord()
	h1 := hashOf(t, rec)
	rec.Analysis.Confidence = 0.75 // was 0.9
	h2 := hashOf(t, rec)
	if h1 != h2 {
		t.Errorf("hash must not change when only Confidence changes: before=%q after=%q", h1, h2)
	}
}

func TestAnalysisContentHash_changes_on_next_check(t *testing.T) {
	rec := baseRecord()
	h1 := hashOf(t, rec)
	rec.Analysis.NextCheckInMinutes = 30
	h2 := hashOf(t, rec)
	if h1 == h2 {
		t.Error("hash must change when NextCheckInMinutes changes")
	}
}

func TestAnalysisContentHash_stable_across_different_key_reasons(t *testing.T) {
	// Regression test: key_reasons is LLM free text that varies in wording between
	// runs even for the same decision. Changing it must NOT change the hash.
	rec := baseRecord()
	h1 := hashOf(t, rec)

	rec.Analysis.KeyReasons = []string{
		"Highest bucket probability is 18C at 38%",       // was "at 37.7%"
		"19C is a close secondary at 36%",                // slightly different wording
		"Downward forecast trend of -0.6C from last run", // different phrasing
		"Only 1 observation available so far",
	}
	h2 := hashOf(t, rec)
	if h1 != h2 {
		t.Errorf("hash must not change when only key_reasons wording changes: before=%q after=%q", h1, h2)
	}
}

func TestAnalysisContentHash_changes_on_hermes_payload_weather_data(t *testing.T) {
	// Changing the weather inputs in the Hermes payload must change the hash
	// even if only generated_at differs in the new payload.
	rec := baseRecord()
	h1 := hashOf(t, rec)
	// Different forecast high: 18.0 → 19.5
	rec.HermesPayloadJSON = json.RawMessage(`{
		"station_code":"ZSPD",
		"target_date_local":"2099-01-01",
		"generated_at":"2099-01-01T10:00:00Z",
		"feature_summary":{"latest_forecast_high_c":19.5,"observation_points":10,"hourly_points":24},
		"bucket_distribution":{"expected_high_c":18.1,"confidence":0.9,"bucket_probs":[{"label":"18C","prob":0.5}]},
		"sanity_flags":[]
	}`)
	h2 := hashOf(t, rec)
	if h1 == h2 {
		t.Error("hash must change when weather data in HermesPayloadJSON changes")
	}
}

func TestAnalysisContentHash_null_secondary_risk(t *testing.T) {
	// nil SecondaryRiskBucket must hash deterministically.
	rec := baseRecord()
	rec.Analysis.SecondaryRiskBucket = nil
	h1 := hashOf(t, rec)
	h2 := hashOf(t, rec)
	if h1 != h2 {
		t.Errorf("hash not stable for nil SecondaryRiskBucket: %q vs %q", h1, h2)
	}
}
