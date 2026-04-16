package services

import (
	"fmt"
	"math"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/hermes"
)

// BuildHermesPayloadService assembles a HermesAnalysisPayload from a completed
// WeatherFeatureSummary and TemperatureBucketDistribution.
// It has no DB dependencies and never returns a partial payload on error.
type BuildHermesPayloadService struct{}

// NewBuildHermesPayloadService creates a BuildHermesPayloadService.
func NewBuildHermesPayloadService() *BuildHermesPayloadService {
	return &BuildHermesPayloadService{}
}

// Build validates that summary and dist are consistent, then assembles a clean,
// rounded HermesAnalysisPayload with sanity flags.
// An error is returned when either input is nil, or station code / target date
// do not match between the two inputs.
func (s *BuildHermesPayloadService) Build(
	summary *domain.WeatherFeatureSummary,
	dist *domain.TemperatureBucketDistribution,
) (hermes.HermesAnalysisPayload, error) {
	if summary == nil {
		return hermes.HermesAnalysisPayload{}, fmt.Errorf("build hermes payload: summary is nil")
	}
	if dist == nil {
		return hermes.HermesAnalysisPayload{}, fmt.Errorf("build hermes payload: distribution is nil")
	}
	if summary.StationCode != dist.StationCode {
		return hermes.HermesAnalysisPayload{}, fmt.Errorf(
			"build hermes payload: station code mismatch: summary=%q dist=%q",
			summary.StationCode, dist.StationCode,
		)
	}
	if summary.TargetDateLocal != dist.TargetDateLocal {
		return hermes.HermesAnalysisPayload{}, fmt.Errorf(
			"build hermes payload: target date mismatch: summary=%q dist=%q",
			summary.TargetDateLocal, dist.TargetDateLocal,
		)
	}

	buckets := make([]hermes.BucketProbView, len(dist.BucketProbs))
	for i, bp := range dist.BucketProbs {
		buckets[i] = hermes.BucketProbView{
			Label: bp.Label,
			Prob:  round4(bp.Prob),
		}
	}

	return hermes.HermesAnalysisPayload{
		StationCode:     summary.StationCode,
		TargetDateLocal: summary.TargetDateLocal,
		GeneratedAt:     time.Now().UTC(),
		FeatureSummary: hermes.FeatureSummaryView{
			LatestForecastHighC:   round2(summary.LatestForecastHighC),
			PreviousForecastHighC: roundPtr2(summary.PreviousForecastHighC),
			ForecastTrendC:        roundPtr2(summary.ForecastTrendC),
			LatestObservedTempC:   roundPtr2(summary.LatestObservedTempC),
			ObservedHighSoFarC:    roundPtr2(summary.ObservedHighSoFarC),
			TempChangeLast3hC:     roundPtr2(summary.TempChangeLast3hC),
			LatestObservationAt:   summary.LatestObservationAt,
			ObservationPoints:     summary.ObservationPoints,
			HourlyPoints:          summary.HourlyPoints,
		},
		BucketDistribution: hermes.BucketDistributionView{
			ExpectedHighC: round2(dist.ExpectedHighC),
			Confidence:    round2(dist.Confidence),
			BucketProbs:   buckets,
		},
		SanityFlags: computeSanityFlags(summary),
	}, nil
}

// computeSanityFlags returns a non-nil slice of human-readable flags for
// notable conditions in summary that Hermes should acknowledge.
// Rules are evaluated in a fixed order so the output is deterministic.
func computeSanityFlags(s *domain.WeatherFeatureSummary) []string {
	flags := make([]string, 0)

	// Observed high exceeds the current forecast — either a genuine forecast
	// miss or unusual data; Hermes should mention this explicitly.
	if s.ObservedHighSoFarC != nil && *s.ObservedHighSoFarC > s.LatestForecastHighC {
		flags = append(flags, "observed_high_exceeds_latest_forecast")
	}

	// No previous forecast snapshot means trend cannot be computed.
	if s.PreviousForecastHighC == nil {
		flags = append(flags, "missing_previous_forecast")
	}

	// Observation coverage.
	switch {
	case s.ObservationPoints == 0:
		// No intraday observations — all observation-derived fields are nil.
		flags = append(flags, "no_observation_data")
	case s.ObservationPoints < 6:
		// Fewer than 6 hourly rows; limited intraday trend coverage.
		flags = append(flags, "limited_observation_coverage")
	}

	return flags
}

// --- Rounding helpers ---------------------------------------------------------
//
// Rounding policy applied at Hermes payload assembly time:
//   - Temperatures (°C), trends, expected high, confidence: 2 decimal places
//   - Bucket probabilities: 4 decimal places
//
// These helpers do NOT change domain values; they are only used when
// constructing the Hermes payload view types.

// round2 rounds v to 2 decimal places (e.g. -1.0999999999999996 → -1.10).
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// round4 rounds v to 4 decimal places (e.g. 0.30000000000000004 → 0.3).
func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// roundPtr2 rounds *v to 2 decimal places. Returns nil when v is nil.
func roundPtr2(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := round2(*v)
	return &r
}
