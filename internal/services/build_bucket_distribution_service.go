package services

import (
	"fmt"
	"math"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// BuildBucketDistributionService converts a WeatherFeatureSummary into a
// TemperatureBucketDistribution using deterministic rule-based logic.
// It has no DB dependencies and never returns an error.
type BuildBucketDistributionService struct{}

const (
	bucketFloorC   = 17
	bucketCeilingC = 25
)

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
	if summary.ObservedHighSoFarC != nil && adjusted < *summary.ObservedHighSoFarC {
		// The final daily high cannot be below a temperature we have already
		// observed, so clamp the point estimate to that hard lower bound.
		adjusted = *summary.ObservedHighSoFarC
	}
	spread := computeSpread(summary)
	probs := bucketProbabilities(adjusted, spread, summary.ObservedHighSoFarC)
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

// bucketProbabilities computes a fine-grained probability distribution over
// integer temperature buckets using a Gaussian CDF centred on adjustedC with
// standard deviation sigma.
//
// Bucket boundaries use half-integer midpoints between neighbouring integer
// labels. With the current fixed range:
//
//	"17C or below" →  X ≤ 17.5
//	"18C"          →  17.5 < X ≤ 18.5
//	...
//	"24C"          →  23.5 < X ≤ 24.5
//	"25C or above" →  X > 24.5
//
// Because the probabilities are derived from a CDF, they sum to exactly 1.0.
func bucketProbabilities(adjustedC, sigma float64, observedHigh *float64) []domain.BucketProbability {
	buckets := temperatureBuckets()

	if observedHigh == nil {
		probs := make([]domain.BucketProbability, 0, len(buckets))
		for _, bucket := range buckets {
			probs = append(probs, domain.BucketProbability{
				Label: bucket.Label,
				Prob:  intervalProbability(bucket.Lo, bucket.Hi, adjustedC, sigma),
			})
		}
		return probs
	}

	lower := *observedHigh
	survival := 1 - normalCDF(lower, adjustedC, sigma)
	if survival <= 0 {
		probs := make([]domain.BucketProbability, 0, len(buckets))
		for _, bucket := range buckets {
			prob := 0.0
			if bucket.Hi == nil {
				prob = 1.0
			}
			probs = append(probs, domain.BucketProbability{
				Label: bucket.Label,
				Prob:  prob,
			})
		}
		return probs
	}

	probs := make([]domain.BucketProbability, 0, len(buckets))
	for _, bucket := range buckets {
		probs = append(probs, domain.BucketProbability{
			Label: bucket.Label,
			Prob:  conditionalIntervalProb(bucket.Lo, bucket.Hi, lower, adjustedC, sigma, survival),
		})
	}
	return probs
}

// conditionalIntervalProb returns P(X in interval | X >= lowerBound) for
// X ~ N(mean, sigma^2). A nil lo means -Inf; a nil hi means +Inf.
func conditionalIntervalProb(lo, hi *float64, lowerBound, mean, sigma, survival float64) float64 {
	effectiveLo := lowerBound
	if lo != nil && *lo > effectiveLo {
		effectiveLo = *lo
	}
	if hi != nil && effectiveLo >= *hi {
		return 0
	}

	upperCDF := 1.0
	if hi != nil {
		upperCDF = normalCDF(*hi, mean, sigma)
	}
	lowerCDF := normalCDF(effectiveLo, mean, sigma)

	prob := (upperCDF - lowerCDF) / survival
	return clamp(prob, 0, 1)
}

// intervalProbability returns P(X in interval) for X ~ N(mean, sigma^2).
// A nil lo means -Inf; a nil hi means +Inf.
func intervalProbability(lo, hi *float64, mean, sigma float64) float64 {
	upperCDF := 1.0
	if hi != nil {
		upperCDF = normalCDF(*hi, mean, sigma)
	}
	lowerCDF := 0.0
	if lo != nil {
		lowerCDF = normalCDF(*lo, mean, sigma)
	}
	return clamp(upperCDF-lowerCDF, 0, 1)
}

type temperatureBucket struct {
	Label string
	Lo    *float64
	Hi    *float64
}

func temperatureBuckets() []temperatureBucket {
	buckets := make([]temperatureBucket, 0, bucketCeilingC-bucketFloorC+1)
	for tempC := bucketFloorC; tempC <= bucketCeilingC; tempC++ {
		switch {
		case tempC == bucketFloorC:
			buckets = append(buckets, temperatureBucket{
				Label: bucketLabel(tempC),
				Lo:    nil,
				Hi:    floatPtr(float64(tempC) + 0.5),
			})
		case tempC == bucketCeilingC:
			buckets = append(buckets, temperatureBucket{
				Label: bucketLabel(tempC),
				Lo:    floatPtr(float64(tempC) - 0.5),
				Hi:    nil,
			})
		default:
			buckets = append(buckets, temperatureBucket{
				Label: bucketLabel(tempC),
				Lo:    floatPtr(float64(tempC) - 0.5),
				Hi:    floatPtr(float64(tempC) + 0.5),
			})
		}
	}
	return buckets
}

func bucketLabel(tempC int) string {
	switch tempC {
	case bucketFloorC:
		return fmt.Sprintf("%dC or below", bucketFloorC)
	case bucketCeilingC:
		return fmt.Sprintf("%dC or above", bucketCeilingC)
	default:
		return fmt.Sprintf("%dC", tempC)
	}
}

func floatPtr(v float64) *float64 { return &v }

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
