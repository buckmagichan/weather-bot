// Package hermes contains the boundary types and helpers for communicating
// with the Hermes analysis skill. Nothing in this package calls Hermes; it
// only defines the stable payload contract that the skill will consume.
package hermes

import (
	"encoding/json"
	"fmt"
	"time"
)

// FeatureSummaryView is the Hermes-facing projection of domain.WeatherFeatureSummary.
//
// Intentionally excludes:
//   - StationCode / TargetDateLocal — already present at the payload top level
//   - GeneratedAt — de-duplicated; only one timestamp at the payload top level
//   - ForecastSnapshotFetchedAt — internal pipeline metadata not needed for analysis
//
// All numeric fields are pre-rounded by the builder (see build_hermes_payload_service.go
// for the rounding policy). Nil pointer fields serialise as JSON null.
type FeatureSummaryView struct {
	LatestForecastHighC   float64    `json:"latest_forecast_high_c"`
	PreviousForecastHighC *float64   `json:"previous_forecast_high_c"`
	ForecastTrendC        *float64   `json:"forecast_trend_c"`
	LatestObservedTempC   *float64   `json:"latest_observed_temp_c"`
	ObservedHighSoFarC    *float64   `json:"observed_high_so_far_c"`
	TempChangeLast3hC     *float64   `json:"temp_change_last_3h_c"`
	LatestObservationAt   *time.Time `json:"latest_observation_at"`
	ObservationPoints     int        `json:"observation_points"`
	HourlyPoints          int        `json:"hourly_points"`
}

// BucketProbView is a single rounded probability bucket for the Hermes payload.
type BucketProbView struct {
	Label string  `json:"label"`
	Prob  float64 `json:"prob"`
}

// BucketDistributionView is the Hermes-facing projection of domain.TemperatureBucketDistribution.
//
// Intentionally excludes:
//   - StationCode / TargetDateLocal — already present at the payload top level
//   - GeneratedAt — de-duplicated; only one timestamp at the payload top level
//
// All numeric fields are pre-rounded by the builder.
type BucketDistributionView struct {
	ExpectedHighC float64          `json:"expected_high_c"`
	Confidence    float64          `json:"confidence"`
	BucketProbs   []BucketProbView `json:"bucket_probs"`
}

// HermesAnalysisPayload is the structured input that the Hermes skill receives
// for daily temperature analysis. It is the clean boundary between deterministic
// Go pipeline logic and future Hermes explanation logic.
//
// Design invariants:
//   - GeneratedAt appears exactly once (top level); nested views omit it.
//   - All float64 values are pre-rounded (no IEEE-754 noise).
//   - SanityFlags is always a list (never null); empty when nothing is notable.
type HermesAnalysisPayload struct {
	StationCode        string                 `json:"station_code"`
	TargetDateLocal    string                 `json:"target_date_local"`
	GeneratedAt        time.Time              `json:"generated_at"`
	FeatureSummary     FeatureSummaryView     `json:"feature_summary"`
	BucketDistribution BucketDistributionView `json:"bucket_distribution"`
	SanityFlags        []string               `json:"sanity_flags"`
}

// MarshalPayload serialises p to compact JSON.
// Use this when writing to a file or passing to a CLI argument.
func MarshalPayload(p HermesAnalysisPayload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal hermes payload: %w", err)
	}
	return b, nil
}

// MustPrettyJSON returns a 2-space-indented JSON string for p.
// Intended for CLI output and debug logging. Panics on marshal failure,
// which can only happen if the standard library fails to encode well-formed
// Go values — effectively a programming error.
func MustPrettyJSON(p HermesAnalysisPayload) string {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("hermes: MustPrettyJSON: %v", err))
	}
	return string(b)
}
