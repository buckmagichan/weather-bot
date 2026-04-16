package services

import (
	"math"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// BuildBucketDistributionService converts a WeatherFeatureSummary into a
// TemperatureBucketDistribution using deterministic rule-based logic.
// It has no DB dependencies and never returns an error.
type BuildBucketDistributionService struct{}

// NewBuildBucketDistributionService creates a BuildBucketDistributionService.
func NewBuildBucketDistributionService() *BuildBucketDistributionService {
	return &BuildBucketDistributionService{}
}

// Build computes a TemperatureBucketDistribution from a WeatherFeatureSummary.
// All input pointer fields are optional — missing signals are simply skipped.
// summary must not be nil; passing nil is a programming error and will panic.
func (s *BuildBucketDistributionService) Build(
	summary *domain.WeatherFeatureSummary,
) *domain.TemperatureBucketDistribution {
	if summary == nil {
		panic("BuildBucketDistributionService.Build: summary must not be nil")
	}
	adjusted := adjustedHigh(summary)
	spread := computeSpread(summary)
	probs := bucketProbabilities(adjusted, spread)
	conf := computeConfidence(summary)

	return &domain.TemperatureBucketDistribution{
		StationCode:     summary.StationCode,
		TargetDateLocal: summary.TargetDateLocal,
		GeneratedAt:     time.Now().UTC(),
		ExpectedHighC:   adjusted,
		BucketProbs:     probs,
		Confidence:      conf,
	}
}

// adjustedHigh refines the forecast high using three independent signals.
// Each rule is applied additively and independently; they do not interact.
// The weights and caps are first-pass values — tune after backtesting.
func adjustedHigh(s *domain.WeatherFeatureSummary) float64 {
	adj := s.LatestForecastHighC

	// Rule 1: Forecast trend.
	// If the forecast has been drifting up or down between model runs, carry
	// 25% of that drift forward. This is capped at ±0.5 C so a single large
	// revision doesn't dominate the estimate.
	if s.ForecastTrendC != nil {
		delta := clamp(*s.ForecastTrendC*0.25, -0.5, 0.5)
		adj += delta
	}

	// Rule 2: Observed high vs forecast.
	// When reality has already met or exceeded the model, revise the expected
	// high upward (30% of the gap). When observations are significantly below
	// the forecast (> 2 C gap), apply a small downward revision (15% of gap)
	// because the model may be too optimistic.
	if s.ObservedHighSoFarC != nil {
		gap := *s.ObservedHighSoFarC - s.LatestForecastHighC
		if gap >= 0 {
			adj += gap * 0.30
		} else if gap < -2.0 {
			adj += gap * 0.15 // gap is negative, so this subtracts
		}
	}

	// Rule 3: Recent 3-hour momentum.
	// Only acts on strong signals (> 1 C change over the last 3 hours).
	// A gentle nudge of ±0.1 C avoids over-fitting to short-term noise.
	if s.TempChangeLast3hC != nil {
		switch {
		case *s.TempChangeLast3hC > 1.0:
			adj += 0.10
		case *s.TempChangeLast3hC < -1.0:
			adj -= 0.10
		}
	}

	return adj
}

// computeSpread returns the Gaussian σ (°C) used to spread the point estimate
// into a probability distribution. More data → narrower spread → sharper probs.
// These values are first-pass — tune after backtesting.
func computeSpread(s *domain.WeatherFeatureSummary) float64 {
	hasObs := s.ObservationPoints > 0
	has3hTrend := s.TempChangeLast3hC != nil

	switch {
	case hasObs && has3hTrend:
		return 0.75 // confident: ground truth + recent trend
	case hasObs:
		return 0.90 // moderate: ground truth, but no recent trend
	default:
		return 1.20 // uncertain: forecast only, no observations
	}
}

// bucketProbabilities computes the probability of four temperature buckets
// using a Gaussian CDF centred on adjustedC with standard deviation sigma.
//
// Bucket boundaries (half-integer midpoints between bucket labels):
//
//	"17C or below"  →  X ≤ 17.5
//	"18C"           →  17.5 < X ≤ 18.5
//	"19C"           →  18.5 < X ≤ 19.5
//	"20C or above"  →  X > 19.5
//
// Because the probabilities are derived from a CDF, they sum to exactly 1.0.
func bucketProbabilities(adjustedC, sigma float64) []domain.BucketProbability {
	cdf175 := normalCDF(17.5, adjustedC, sigma)
	cdf185 := normalCDF(18.5, adjustedC, sigma)
	cdf195 := normalCDF(19.5, adjustedC, sigma)

	return []domain.BucketProbability{
		{Label: "17C or below", Prob: cdf175},
		{Label: "18C", Prob: cdf185 - cdf175},
		{Label: "19C", Prob: cdf195 - cdf185},
		{Label: "20C or above", Prob: 1 - cdf195},
	}
}

// computeConfidence returns a score ∈ [0, 1] reflecting how much data backed
// the estimate. Base is 0.50 (we always have at least a forecast); each
// additional signal adds a fixed bonus.
// Maximum achievable: 0.90 — we never claim certainty.
func computeConfidence(s *domain.WeatherFeatureSummary) float64 {
	conf := 0.50

	if s.PreviousForecastHighC != nil {
		conf += 0.15 // forecast trend direction available
	}
	if s.ObservationPoints > 0 {
		conf += 0.15 // real-world observations available
	}
	if s.TempChangeLast3hC != nil {
		conf += 0.10 // recent warming/cooling trend available
	}

	return clamp(conf, 0, 1)
}

// normalCDF computes P(X ≤ x) for X ~ N(mean, sigma²) using Go's math.Erfc.
// This is the exact error function, not an approximation.
func normalCDF(x, mean, sigma float64) float64 {
	return 0.5 * math.Erfc(-(x-mean)/(sigma*math.Sqrt2))
}

// clamp restricts v to [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
