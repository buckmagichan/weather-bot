package services

import (
	"fmt"
	"math"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/providers/wunderground"
)

// BuildBucketDistributionService converts a WeatherFeatureSummary into a
// TemperatureBucketDistribution using deterministic rule-based logic.
// It has no DB dependencies and never returns an error.
type BuildBucketDistributionService struct{}

const (
	bucketFloorC          = -20
	bucketCeilingC        = 50
	peakWarmingCutoffHour = 13.5
)

type temperatureProfileConfig struct {
	softLockHour              float64
	hardLockHour              float64
	stableSigma               float64
	hardLockSigma             float64
	overshootAdjustmentWeight float64
	strongWarmingUpsideC      float64
	strongWarmingUntilHour    float64
}

var defaultTemperatureProfileConfig = temperatureProfileConfig{
	softLockHour:              14.0,
	hardLockHour:              15.0,
	stableSigma:               0.35,
	hardLockSigma:             0.25,
	overshootAdjustmentWeight: 0.45,
	strongWarmingUpsideC:      0.65,
	strongWarmingUntilHour:    17.0,
}

var temperatureProfileConfigs = map[string]temperatureProfileConfig{
	TemperatureProfileCoastalFastLock: {
		softLockHour:              13.0,
		hardLockHour:              14.0,
		stableSigma:               0.30,
		hardLockSigma:             0.22,
		overshootAdjustmentWeight: 0.55,
		strongWarmingUpsideC:      0.35,
		strongWarmingUntilHour:    14.0,
	},
	TemperatureProfileHumidSouth: {
		softLockHour:              13.5,
		hardLockHour:              14.5,
		stableSigma:               0.34,
		hardLockSigma:             0.25,
		overshootAdjustmentWeight: 0.50,
		strongWarmingUpsideC:      0.50,
		strongWarmingUntilHour:    15.0,
	},
	TemperatureProfileNorthInland: {
		softLockHour:              14.0,
		hardLockHour:              15.0,
		stableSigma:               0.35,
		hardLockSigma:             0.25,
		overshootAdjustmentWeight: 0.45,
		strongWarmingUpsideC:      0.65,
		strongWarmingUntilHour:    16.0,
	},
	TemperatureProfileBasinInland: {
		softLockHour:              14.5,
		hardLockHour:              15.0,
		stableSigma:               0.45,
		hardLockSigma:             0.30,
		overshootAdjustmentWeight: 0.38,
		strongWarmingUpsideC:      0.85,
		strongWarmingUntilHour:    16.5,
	},
}

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
	observedHigh := effectiveObservedHigh(summary)
	adjusted := adjustedHigh(summary)
	if observedHigh != nil && adjusted < *observedHigh {
		// The final daily high cannot be below a temperature we have already
		// observed, so clamp the point estimate to that hard lower bound.
		adjusted = *observedHigh
	}
	if observedHigh != nil && historicalResolutionLocked(summary, *observedHigh) {
		adjusted = *observedHigh
	}
	spread := computeSpread(summary)
	probs := bucketProbabilities(adjusted, spread, observedHigh)
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

// adjustedHigh refines the forecast high using time-series style residual
// corrections, recent momentum, station-local time, and remaining model upside.
// The residual pieces are a deterministic fallback for the tsEMOS idea: without
// ensemble spread or enough training history, we use run-to-run forecast drift
// and observed-vs-forecast error as conservative proxies.
func adjustedHigh(s *domain.WeatherFeatureSummary) float64 {
	adj := s.LatestForecastHighC
	observedHigh := effectiveObservedHigh(s)
	localHour := stationLocalHour(s)
	profile := temperatureProfileConfigFor(s.TemperatureProfile)

	adj += forecastDriftCorrection(s)
	adj += observedForecastResidualCorrection(s, observedHigh, localHour, profile)

	// Recent 3-hour momentum.
	// Only acts on strong signals (> 1 C change over the last 3 hours). Strong
	// late-day warming is allowed to keep adjacent warmer buckets live; cooling
	// modestly pulls back model optimism.
	if s.TempChangeLast3hC != nil {
		switch {
		case *s.TempChangeLast3hC >= 1.0:
			adj += clamp(0.15+(*s.TempChangeLast3hC-1.0)*0.20, 0.15, 0.65)
		case *s.TempChangeLast3hC < -1.0:
			adj -= 0.20
		}
	}

	if observedHigh != nil {
		obs := *observedHigh
		if strongRecentWarming(s) && localHour < profile.strongWarmingUntilHour {
			adj = math.Max(adj, obs+profile.strongWarmingUpsideC)
		}
		if isSoftLocked(localHour, profile) && !strongRecentWarming(s) {
			forecastGap := s.LatestForecastHighC - obs
			if forecastGap >= 2.0 {
				allowedUpside := 0.75
				if isHardLocked(localHour, profile) {
					allowedUpside = 0.35
				}
				if stableOrCooling(s, obs) {
					allowedUpside = 0.25
				}
				if s.RemainingForecastHighC != nil {
					remainingUpside := math.Max(0, *s.RemainingForecastHighC-obs)
					allowedUpside = math.Min(allowedUpside, remainingUpside)
				}
				adj = math.Min(adj, obs+allowedUpside)
			}
			if stableOrCooling(s, obs) && remainingForecastDoesNotExceedObserved(s, obs) {
				adj = math.Min(adj, obs+0.15)
			}
		}
	}

	return adj
}

func forecastDriftCorrection(s *domain.WeatherFeatureSummary) float64 {
	if s.ForecastTrendC == nil {
		return 0
	}
	return clamp(*s.ForecastTrendC*0.25, -0.5, 0.5)
}

func observedForecastResidualCorrection(
	s *domain.WeatherFeatureSummary,
	observedHigh *float64,
	localHour float64,
	profile temperatureProfileConfig,
) float64 {
	if observedHigh == nil {
		return 0
	}
	gap := *observedHigh - s.LatestForecastHighC
	if gap >= 0 {
		return gap * 0.30
	}
	if gap >= -2.0 {
		return 0
	}

	weight := 0.15
	if isSoftLocked(localHour, profile) {
		weight = 0.35
	}
	if isSoftLocked(localHour, profile) && stableOrCooling(s, *observedHigh) {
		weight = profile.overshootAdjustmentWeight
	}
	return gap * weight
}

// computeSpread returns the Gaussian σ (°C) used to spread the point estimate
// into a probability distribution. More data → narrower spread → sharper probs.
// Run-to-run forecast drift and same-day residual stress widen the spread, a
// deterministic analogue of tsEMOS' time-varying scale parameter.
func computeSpread(s *domain.WeatherFeatureSummary) float64 {
	observedHigh := effectiveObservedHigh(s)
	hasObs := observedHigh != nil
	has3hTrend := s.TempChangeLast3hC != nil
	localHour := stationLocalHour(s)
	profile := temperatureProfileConfigFor(s.TemperatureProfile)

	var base float64
	switch {
	case hasObs && historicalResolutionLocked(s, *observedHigh):
		return profile.hardLockSigma
	case hasObs && isHardLocked(localHour, profile) && stableOrCooling(s, *observedHigh) && remainingForecastDoesNotExceedObserved(s, *observedHigh):
		base = profile.hardLockSigma
	case hasObs && isSoftLocked(localHour, profile) && stableOrCooling(s, *observedHigh) && strongObservationCoverage(s):
		base = profile.stableSigma
	case hasObs && isSoftLocked(localHour, profile) && !strongRecentWarming(s) && s.LatestForecastHighC-*observedHigh >= 2.0:
		base = 0.45
	case hasObs && strongRecentWarming(s):
		base = 0.80
	case hasObs && has3hTrend:
		base = 0.75 // confident: ground truth + recent trend
	case hasObs:
		base = 0.90 // moderate: ground truth, but no recent trend
	default:
		base = 1.20 // uncertain: forecast only, no observations
	}
	return spreadWithTimeSeriesStress(s, observedHigh, base)
}

func spreadWithTimeSeriesStress(
	s *domain.WeatherFeatureSummary,
	observedHigh *float64,
	base float64,
) float64 {
	inflation := 0.0

	if s.ForecastTrendC != nil {
		absTrend := math.Abs(*s.ForecastTrendC)
		if absTrend > 0.75 {
			inflation += clamp((absTrend-0.75)*0.15, 0, 0.35)
		}
	}

	if observedHigh != nil {
		residual := *observedHigh - s.LatestForecastHighC
		shouldInflateResidual := residual > 0 || !stableOrCooling(s, *observedHigh)
		if shouldInflateResidual {
			absResidual := math.Abs(residual)
			if absResidual > 1.0 {
				inflation += clamp((absResidual-1.0)*0.12, 0, 0.40)
			}
		}
	}

	if s.TempChangeLast3hC != nil && *s.TempChangeLast3hC >= 1.5 {
		inflation += clamp((*s.TempChangeLast3hC-1.5)*0.10, 0.05, 0.25)
	}

	return clamp(base+inflation, 0.20, 1.60)
}

func effectiveObservedHigh(s *domain.WeatherFeatureSummary) *float64 {
	if s.ResolutionObservedHighC != nil {
		return s.ResolutionObservedHighC
	}
	return s.ObservedHighSoFarC
}

func temperatureProfileConfigFor(profile string) temperatureProfileConfig {
	if config, ok := temperatureProfileConfigs[profile]; ok {
		return config
	}
	return defaultTemperatureProfileConfig
}

func isSoftLocked(localHour float64, config temperatureProfileConfig) bool {
	return localHour >= config.softLockHour
}

func isHardLocked(localHour float64, config temperatureProfileConfig) bool {
	return localHour >= config.hardLockHour
}

func stationLocalHour(s *domain.WeatherFeatureSummary) float64 {
	timezone := s.Timezone
	if timezone == "" {
		timezone = summarySvcTimezone
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc, err = time.LoadLocation(summarySvcTimezone)
		if err != nil {
			panic(fmt.Sprintf("BuildBucketDistributionService: load fallback timezone %q: %v", summarySvcTimezone, err))
		}
	}
	local := s.GeneratedAt.In(loc)
	return float64(local.Hour()) + float64(local.Minute())/60.0
}

func strongRecentWarming(s *domain.WeatherFeatureSummary) bool {
	return s.TempChangeLast3hC != nil && *s.TempChangeLast3hC >= 2.0
}

func stableOrCooling(s *domain.WeatherFeatureSummary, observedHigh float64) bool {
	if s.TempChangeLast3hC != nil && *s.TempChangeLast3hC <= 0.75 {
		return true
	}
	return s.LatestObservedTempC != nil && *s.LatestObservedTempC <= observedHigh-0.4
}

func strongObservationCoverage(s *domain.WeatherFeatureSummary) bool {
	return s.ObservationPoints >= 12 || s.ResolutionObservedHighC != nil
}

func remainingForecastDoesNotExceedObserved(s *domain.WeatherFeatureSummary, observedHigh float64) bool {
	return s.RemainingForecastHighC != nil && *s.RemainingForecastHighC <= observedHigh+0.5
}

func largeRemainingUpsideBeforePeak(s *domain.WeatherFeatureSummary) bool {
	observedHigh := effectiveObservedHigh(s)
	if observedHigh == nil || s.RemainingForecastHighC == nil {
		return false
	}
	if stationLocalHour(s) >= peakWarmingCutoffHour {
		return false
	}
	return *s.RemainingForecastHighC-*observedHigh >= 3.0
}

func historicalResolutionLocked(s *domain.WeatherFeatureSummary, observedHigh float64) bool {
	if s.ResolutionSourceType != wunderground.SourceTypeHistoricalObservations {
		return false
	}
	if s.ResolutionObservedHighC == nil {
		return false
	}
	if stationLocalHour(s) < lateEveningLockHour {
		return false
	}
	if s.ObservationPoints < 12 {
		return false
	}
	return stableOrCooling(s, observedHigh)
}

// bucketProbabilities computes a fine-grained probability distribution over
// integer temperature buckets using a Gaussian CDF centred on adjustedC with
// standard deviation sigma.
//
// Bucket boundaries use half-integer midpoints between neighbouring integer
// labels. With the current fixed range:
//
//	"-20C or below" →  X ≤ -19.5
//	"-19C"          →  -19.5 < X ≤ -18.5
//	...
//	"49C"           →  48.5 < X ≤ 49.5
//	"50C or above"  →  X > 49.5
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
	if largeRemainingUpsideBeforePeak(s) {
		conf -= 0.10
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
