package services

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/providers/wunderground"
)

// =============================================================================
// Helpers
// =============================================================================

// summaryOption is a functional option for makeSummary.
type summaryOption func(*domain.WeatherFeatureSummary)

func withForecastHigh(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.LatestForecastHighC = c }
}
func withPreviousForecastHigh(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.PreviousForecastHighC = fp(c) }
}
func withForecastTrend(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.ForecastTrendC = fp(c) }
}
func withObsHigh(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) {
		s.ObservedHighSoFarC = fp(c)
		s.ObservationPoints = 4 // implies obs data is present for spread calculation
	}
}
func withResolutionHigh(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.ResolutionObservedHighC = fp(c) }
}
func withResolutionSourceType(sourceType string) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.ResolutionSourceType = sourceType }
}
func withTempChange3h(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.TempChangeLast3hC = fp(c) }
}
func withLatestObserved(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.LatestObservedTempC = fp(c) }
}
func withRemainingForecastHigh(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.RemainingForecastHighC = fp(c) }
}
func withObsPoints(n int) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.ObservationPoints = n }
}
func withGeneratedAt(t time.Time) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.GeneratedAt = t }
}
func withTimezone(tz string) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.Timezone = tz }
}
func withTemperatureProfile(profile string) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.TemperatureProfile = profile }
}

// makeSummary builds a WeatherFeatureSummary with a default forecast high of
// 18.0 C. Options override individual fields.
func makeSummary(opts ...summaryOption) *domain.WeatherFeatureSummary {
	s := &domain.WeatherFeatureSummary{
		StationCode:         "ZSPD",
		TargetDateLocal:     "2026-04-14",
		LatestForecastHighC: 18.0,
		HourlyPoints:        24,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// fp returns a pointer to a float64.
func fp(v float64) *float64 { return &v }

// totalProb returns the sum of all bucket probabilities.
func totalProb(probs []domain.BucketProbability) float64 {
	var sum float64
	for _, p := range probs {
		sum += p.Prob
	}
	return sum
}

// findProb returns the probability of the bucket with the given label.
// Returns -1 if the label is not found.
func findProb(probs []domain.BucketProbability, label string) float64 {
	for _, p := range probs {
		if p.Label == label {
			return p.Prob
		}
	}
	return -1
}

// assertValidDistribution checks all general invariants that every valid
// TemperatureBucketDistribution must satisfy, regardless of inputs.
func assertValidDistribution(t *testing.T, d *domain.TemperatureBucketDistribution) {
	t.Helper()

	if d == nil {
		t.Fatal("distribution is nil")
		return
	}

	// Bucket count
	wantBuckets := bucketCeilingC - bucketFloorC + 1
	if len(d.BucketProbs) != wantBuckets {
		t.Errorf("BucketProbs: got %d buckets, want %d", len(d.BucketProbs), wantBuckets)
	}

	// Per-bucket validity
	for i, b := range d.BucketProbs {
		if b.Label == "" {
			t.Errorf("bucket[%d] has empty label", i)
		}
		if b.Prob < 0 || b.Prob > 1 {
			t.Errorf("bucket[%d] %q: prob %.6f outside [0,1]", i, b.Label, b.Prob)
		}
	}

	// Sum constraint
	sum := totalProb(d.BucketProbs)
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("bucket probs sum to %.12f, want 1.0 (delta %.2e)", sum, math.Abs(sum-1.0))
	}

	// Confidence range
	if d.Confidence < 0 || d.Confidence > 1 {
		t.Errorf("Confidence %.4f outside [0,1]", d.Confidence)
	}

	// Expected high is a normal floating-point number
	if math.IsNaN(d.ExpectedHighC) || math.IsInf(d.ExpectedHighC, 0) {
		t.Errorf("ExpectedHighC is not finite: %v", d.ExpectedHighC)
	}
}

// nearF reports whether |a-b| < tol.
func nearF(a, b, tol float64) bool { return math.Abs(a-b) < tol }

func sumProbUpTo(probs []domain.BucketProbability, maxTempC int) float64 {
	var sum float64
	for tempC := bucketFloorC; tempC <= maxTempC; tempC++ {
		sum += findProb(probs, bucketLabel(tempC))
	}
	return sum
}

func sumProbFrom(probs []domain.BucketProbability, minTempC int) float64 {
	var sum float64
	for tempC := minTempC; tempC <= bucketCeilingC; tempC++ {
		sum += findProb(probs, bucketLabel(tempC))
	}
	return sum
}

// =============================================================================
// Tests
// =============================================================================

// TestBuildBucketDistribution_Cases covers the six user-specified cases plus
// additional correctness cases, all table-driven.
func TestBuildBucketDistribution_Cases(t *testing.T) {
	svc := NewBuildBucketDistributionService()

	tests := []struct {
		name    string
		summary *domain.WeatherFeatureSummary
		check   func(t *testing.T, d *domain.TemperatureBucketDistribution)
	}{
		// ── Case A ────────────────────────────────────────────────────────────
		// Basic summary with no optional adjustments. Verifies every invariant.
		{
			name:    "A_basic_valid_distribution",
			summary: makeSummary(),
			check: func(t *testing.T, d *domain.TemperatureBucketDistribution) {
				t.Helper()
				assertValidDistribution(t, d)

				wantFirstLabels := []string{
					bucketLabel(bucketFloorC),
					bucketLabel(bucketFloorC + 1),
					bucketLabel(bucketFloorC + 2),
					bucketLabel(bucketFloorC + 3),
				}
				for i, want := range wantFirstLabels {
					if got := d.BucketProbs[i].Label; got != want {
						t.Errorf("bucket[%d] label: got %q, want %q", i, got, want)
					}
				}
				if got := d.BucketProbs[len(d.BucketProbs)-1].Label; got != bucketLabel(bucketCeilingC) {
					t.Errorf("last bucket label: got %q, want %q", got, bucketLabel(bucketCeilingC))
				}
				// Confidence: base only (0.50) since no optional fields are set.
				if !nearF(d.Confidence, 0.50, 1e-9) {
					t.Errorf("Confidence: got %.2f, want 0.50", d.Confidence)
				}
				// ExpectedHighC should equal the forecast when no signals adjust it.
				if !nearF(d.ExpectedHighC, 18.0, 1e-9) {
					t.Errorf("ExpectedHighC: got %.4f, want 18.0", d.ExpectedHighC)
				}
			},
		},

		// ── Case B ────────────────────────────────────────────────────────────
		// Positive forecast trend shifts the expected high upward and pushes
		// probability into warmer buckets.
		{
			name:    "B_positive_trend_shifts_up",
			summary: makeSummary(withForecastTrend(2.0)),
			check: func(t *testing.T, d *domain.TemperatureBucketDistribution) {
				t.Helper()
				assertValidDistribution(t, d)

				baseline := svc.Build(makeSummary())

				// Expected high must increase.
				if d.ExpectedHighC <= baseline.ExpectedHighC {
					t.Errorf("positive trend should raise ExpectedHighC: got %.3f, baseline %.3f",
						d.ExpectedHighC, baseline.ExpectedHighC)
				}
				// Warmer-tail probability should increase.
				gotWarm := sumProbFrom(d.BucketProbs, 19)
				baseWarm := sumProbFrom(baseline.BucketProbs, 19)
				if gotWarm <= baseWarm {
					t.Errorf("positive trend: warm-tail prob should increase: got %.4f, baseline %.4f",
						gotWarm, baseWarm)
				}
				// Cooler-tail probability should decrease.
				gotCool := sumProbUpTo(d.BucketProbs, 17)
				baseCool := sumProbUpTo(baseline.BucketProbs, 17)
				if gotCool >= baseCool {
					t.Errorf("positive trend: cool-tail prob should decrease: got %.4f, baseline %.4f",
						gotCool, baseCool)
				}
			},
		},

		// ── Case C ────────────────────────────────────────────────────────────
		// Negative forecast trend shifts the expected high downward and moves
		// probability into cooler buckets.
		{
			name:    "C_negative_trend_shifts_down",
			summary: makeSummary(withForecastTrend(-2.0)),
			check: func(t *testing.T, d *domain.TemperatureBucketDistribution) {
				t.Helper()
				assertValidDistribution(t, d)

				baseline := svc.Build(makeSummary())

				if d.ExpectedHighC >= baseline.ExpectedHighC {
					t.Errorf("negative trend should lower ExpectedHighC: got %.3f, baseline %.3f",
						d.ExpectedHighC, baseline.ExpectedHighC)
				}
				// Cooler-tail probability should gain.
				gotCool := sumProbUpTo(d.BucketProbs, 17)
				baseCool := sumProbUpTo(baseline.BucketProbs, 17)
				if gotCool <= baseCool {
					t.Errorf("negative trend: cool-tail prob should increase: got %.4f, baseline %.4f",
						gotCool, baseCool)
				}
				// Warmer-tail probability should lose.
				gotWarm := sumProbFrom(d.BucketProbs, 19)
				baseWarm := sumProbFrom(baseline.BucketProbs, 19)
				if gotWarm >= baseWarm {
					t.Errorf("negative trend: warm-tail prob should decrease: got %.4f, baseline %.4f",
						gotWarm, baseWarm)
				}
			},
		},

		// ── Case D ────────────────────────────────────────────────────────────
		// Obs high so far is close to (but slightly below) forecast high.
		// Rule 2 does not apply (gap = -0.2, between -2 and 0).
		// However, having observation data narrows the spread (1.20 → 0.90),
		// which concentrates probability around the expected high and reduces
		// probability in the lower tail — i.e. downside risk is reduced.
		{
			name:    "D_obs_close_to_forecast_reduces_downside",
			summary: makeSummary(withForecastHigh(18.0), withObsHigh(17.8)),
			check: func(t *testing.T, d *domain.TemperatureBucketDistribution) {
				t.Helper()
				assertValidDistribution(t, d)

				// Baseline: forecast only, no obs, wide spread (1.20).
				baseline := svc.Build(makeSummary(withForecastHigh(18.0)))

				// Adjusted high should not change (gap = -0.2 is within the dead zone).
				// But the final daily high still cannot be below the observed high.
				if d.ExpectedHighC < 17.8 {
					t.Errorf("obs close to forecast should enforce observed high floor: got %.4f, want >= 17.8",
						d.ExpectedHighC)
				}
				// Once 17.8 C has already happened, all buckets up to 17C are impossible.
				gotLow := sumProbUpTo(d.BucketProbs, 17)
				if !nearF(gotLow, 0, 1e-9) {
					t.Errorf("obs close to forecast: buckets from %s through 17C should be impossible after 17.8C observed, got %.4f",
						bucketLabel(bucketFloorC), gotLow)
				}
				baseLow := sumProbUpTo(baseline.BucketProbs, 17)
				if baseLow <= gotLow {
					t.Errorf("baseline lower-tail probability should still exceed conditioned value: got %.4f, baseline %.4f",
						gotLow, baseLow)
				}
			},
		},

		// ── Case E ────────────────────────────────────────────────────────────
		// Strong recent warming (> 1 C over 3 h) increases the expected high
		// by 0.10 C and increases probability in warmer buckets.
		//
		// To isolate the warming nudge from the spread change, we compare
		// against a baseline that has the same spread (obs + 3h data = 0.75 σ)
		// but a sub-threshold 3h change (0.5 C, no nudge applied).
		{
			name:    "E_strong_3h_warming_increases_upside",
			summary: makeSummary(withObsPoints(4), withTempChange3h(2.0)),
			check: func(t *testing.T, d *domain.TemperatureBucketDistribution) {
				t.Helper()
				assertValidDistribution(t, d)

				// Baseline has same spread (0.75) but no momentum nudge.
				baseline := svc.Build(makeSummary(withObsPoints(4), withTempChange3h(0.5)))

				if d.ExpectedHighC <= baseline.ExpectedHighC {
					t.Errorf("3h warming should raise ExpectedHighC: got %.4f, baseline %.4f",
						d.ExpectedHighC, baseline.ExpectedHighC)
				}
				// Same spread, higher center → warm-tail probability gains.
				gotWarm := sumProbFrom(d.BucketProbs, 19)
				baseWarm := sumProbFrom(baseline.BucketProbs, 19)
				if gotWarm <= baseWarm {
					t.Errorf("3h warming: warm-tail prob should increase: got %.4f, baseline %.4f",
						gotWarm, baseWarm)
				}
			},
		},

		// ── Case F ────────────────────────────────────────────────────────────
		// Only the required field (LatestForecastHighC) is set. All optional
		// pointer fields are nil. The service must still return a valid output.
		{
			name:    "F_missing_optional_fields_still_valid",
			summary: makeSummary(), // only LatestForecastHighC = 18.0
			check: func(t *testing.T, d *domain.TemperatureBucketDistribution) {
				t.Helper()
				assertValidDistribution(t, d)
				// No optional fields → minimum confidence (base only).
				if !nearF(d.Confidence, 0.50, 1e-9) {
					t.Errorf("Confidence: got %.2f, want 0.50 (no optional signals)", d.Confidence)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dist := svc.Build(tc.summary)
			if dist == nil {
				t.Fatal("Build returned nil")
			}
			tc.check(t, dist)
		})
	}
}

// TestBuildBucketDistribution_Invariants runs assertValidDistribution across a
// broad set of inputs, ensuring no input combination breaks basic invariants.
func TestBuildBucketDistribution_Invariants(t *testing.T) {
	svc := NewBuildBucketDistributionService()

	cases := []*domain.WeatherFeatureSummary{
		makeSummary(),
		makeSummary(withForecastHigh(15.0)),
		makeSummary(withForecastHigh(25.0)),
		makeSummary(withForecastTrend(0.4)),
		makeSummary(withForecastTrend(-0.4)),
		makeSummary(withForecastTrend(10.0)),  // large positive — hits cap
		makeSummary(withForecastTrend(-10.0)), // large negative — hits cap
		makeSummary(withObsHigh(17.8)),
		makeSummary(withObsHigh(19.5)),
		makeSummary(withObsHigh(15.0), withForecastHigh(20.0)), // obs well below
		makeSummary(withTempChange3h(2.0), withObsPoints(4)),
		makeSummary(withTempChange3h(-2.0), withObsPoints(4)),
		makeSummary(withPreviousForecastHigh(17.5), withObsPoints(4), withTempChange3h(1.5)),
	}

	for i, s := range cases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			assertValidDistribution(t, svc.Build(s))
		})
	}
}

// TestBuildBucketDistribution_Confidence verifies that confidence increases
// monotonically as more data signals become available.
func TestBuildBucketDistribution_Confidence(t *testing.T) {
	svc := NewBuildBucketDistributionService()

	c0 := svc.Build(makeSummary()).Confidence                                                                        // 0.50
	c1 := svc.Build(makeSummary(withPreviousForecastHigh(17.5))).Confidence                                          // 0.65
	c2 := svc.Build(makeSummary(withPreviousForecastHigh(17.5), withObsPoints(4))).Confidence                        // 0.80
	c3 := svc.Build(makeSummary(withPreviousForecastHigh(17.5), withObsPoints(4), withTempChange3h(0.5))).Confidence // 0.90

	steps := []struct{ got, prev float64 }{
		{c1, c0},
		{c2, c1},
		{c3, c2},
	}
	for i, s := range steps {
		if s.got <= s.prev {
			t.Errorf("step %d: confidence should increase: %.2f → %.2f", i, s.prev, s.got)
		}
	}
	// Maximum is 0.90; we never claim certainty.
	if c3 > 0.90+1e-9 {
		t.Errorf("max confidence exceeded: got %.4f, want ≤ 0.90", c3)
	}
}

func TestBuildBucketDistribution_ConfidenceDiscountsLargeEarlyUpside(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	localNoonUTC := time.Date(2026, 5, 10, 4, 0, 0, 0, time.UTC)

	baseline := svc.Build(makeSummary(
		withGeneratedAt(localNoonUTC),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withObsPoints(12),
		withTempChange3h(1.0),
		withRemainingForecastHigh(27.5),
	)).Confidence
	largeUpside := svc.Build(makeSummary(
		withGeneratedAt(localNoonUTC),
		withForecastHigh(30.0),
		withObsHigh(25.0),
		withObsPoints(12),
		withTempChange3h(1.0),
		withRemainingForecastHigh(29.0),
	)).Confidence

	if largeUpside >= baseline {
		t.Fatalf("large early remaining upside should discount confidence: got %.2f baseline %.2f", largeUpside, baseline)
	}
}

// TestBuildBucketDistribution_TrendCap ensures the ±0.5 C cap on Rule 1
// prevents large trends from dominating the estimate.
func TestBuildBucketDistribution_TrendCap(t *testing.T) {
	svc := NewBuildBucketDistributionService()

	const base = 18.0
	dPos := svc.Build(makeSummary(withForecastHigh(base), withForecastTrend(10.0)))
	dNeg := svc.Build(makeSummary(withForecastHigh(base), withForecastTrend(-10.0)))

	if dPos.ExpectedHighC > base+0.5+1e-9 {
		t.Errorf("positive cap: ExpectedHighC %.4f > %.4f", dPos.ExpectedHighC, base+0.5)
	}
	if dNeg.ExpectedHighC < base-0.5-1e-9 {
		t.Errorf("negative cap: ExpectedHighC %.4f < %.4f", dNeg.ExpectedHighC, base-0.5)
	}
}

func TestComputeSpread_TimeSeriesStressWidensUnstableInputs(t *testing.T) {
	baseline := computeSpread(makeSummary(
		withForecastHigh(18.0),
		withObsHigh(18.0),
		withTempChange3h(0.5),
	))
	withDrift := computeSpread(makeSummary(
		withForecastHigh(18.0),
		withObsHigh(18.0),
		withTempChange3h(0.5),
		withForecastTrend(3.0),
	))
	withUnderforecast := computeSpread(makeSummary(
		withForecastHigh(18.0),
		withObsHigh(20.0),
		withTempChange3h(0.5),
	))

	if withDrift <= baseline {
		t.Fatalf("large run-to-run drift should widen spread: got %.4f baseline %.4f", withDrift, baseline)
	}
	if withUnderforecast <= baseline {
		t.Fatalf("observed high above forecast should widen spread: got %.4f baseline %.4f", withUnderforecast, baseline)
	}
}

func TestComputeSpread_StableOverforecastDoesNotInflateLockedDay(t *testing.T) {
	local15UTC := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)
	s := makeSummary(
		withGeneratedAt(local15UTC),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withLatestObserved(24.6),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(25.2),
	)

	got := computeSpread(s)
	want := temperatureProfileConfigFor(s.TemperatureProfile).hardLockSigma
	if !nearF(got, want, 1e-9) {
		t.Fatalf("stable late overforecast should remain hard-locked: got %.4f want %.4f", got, want)
	}
}

func TestBuildBucketDistribution_ObservedHighSetsHardFloor(t *testing.T) {
	svc := NewBuildBucketDistributionService()

	d := svc.Build(makeSummary(
		withForecastHigh(18.3),
		withObsHigh(23.0),
		withTempChange3h(-4.4),
	))

	assertValidDistribution(t, d)

	if !nearF(d.ExpectedHighC, 23.0, 1e-9) {
		t.Errorf("ExpectedHighC: got %.4f, want 23.0", d.ExpectedHighC)
	}
	if !nearF(sumProbUpTo(d.BucketProbs, 22), 0, 1e-9) {
		t.Errorf("all buckets up to 22C should be impossible once 23.0C is observed, got %.6f",
			sumProbUpTo(d.BucketProbs, 22))
	}
	if !nearF(sumProbFrom(d.BucketProbs, 23), 1.0, 1e-9) {
		t.Errorf("all probability mass should remain on 23C and above, got %.6f",
			sumProbFrom(d.BucketProbs, 23))
	}
	if got := findProb(d.BucketProbs, "23C"); got <= 0 {
		t.Errorf("\"23C\" bucket should retain some probability once 23.0C is observed, got %.6f", got)
	}
}

func TestBuildBucketDistribution_LateDayStableHighCollapsesMass(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local15UTC := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)

	d := svc.Build(makeSummary(
		withGeneratedAt(local15UTC),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withLatestObserved(24.6),
		withTempChange3h(0.2),
		withObsPoints(16),
		withRemainingForecastHigh(25.2),
	))

	assertValidDistribution(t, d)

	if d.ExpectedHighC > 25.2 {
		t.Errorf("late stable high should keep ExpectedHighC near observed high: got %.4f", d.ExpectedHighC)
	}
	if got := findProb(d.BucketProbs, "25C"); got < 0.80 {
		t.Errorf("25C bucket should dominate late stable day, got %.4f", got)
	}
	if got := sumProbFrom(d.BucketProbs, 27); got > 0.01 {
		t.Errorf("late stable upside >=27C should be tiny, got %.4f", got)
	}
}

func TestBuildBucketDistribution_StrongRecentWarmingKeepsUpsideMeaningful(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local15UTC := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)

	d := svc.Build(makeSummary(
		withGeneratedAt(local15UTC),
		withForecastHigh(30.0),
		withObsHigh(30.0),
		withLatestObserved(30.0),
		withTempChange3h(3.0),
		withObsPoints(14),
		withRemainingForecastHigh(31.2),
	))

	assertValidDistribution(t, d)

	if d.ExpectedHighC < 30.6 {
		t.Errorf("strong warming should keep adjacent upside in the center estimate, got %.4f", d.ExpectedHighC)
	}
	if got := findProb(d.BucketProbs, "31C"); got < 0.35 {
		t.Errorf("31C should remain meaningful during strong warming, got %.4f", got)
	}
}

func TestBuildBucketDistribution_AfternoonObservedHighFarBelowForecastLowersExpectedHigh(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local15UTC := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)

	d := svc.Build(makeSummary(
		withGeneratedAt(local15UTC),
		withForecastHigh(24.0),
		withObsHigh(21.0),
		withLatestObserved(20.7),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(21.4),
	))

	assertValidDistribution(t, d)

	if d.ExpectedHighC > 21.5 {
		t.Errorf("afternoon forecast overshoot should be pulled toward observed high: got %.4f", d.ExpectedHighC)
	}
	if got := findProb(d.BucketProbs, "24C"); got > 0.01 {
		t.Errorf("stale 24C forecast bucket should be heavily discounted, got %.4f", got)
	}
	if got := sumProbUpTo(d.BucketProbs, 20); !nearF(got, 0, 1e-9) {
		t.Errorf("observed high remains a hard lower bound; <=20C got %.6f", got)
	}
}

func TestBuildBucketDistribution_UsesSummaryTimezoneForLateDayRules(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	utcMorningInNewYork := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC) // 03:00 New York, 15:00 Shanghai

	d := svc.Build(makeSummary(
		withGeneratedAt(utcMorningInNewYork),
		withTimezone("America/New_York"),
		withForecastHigh(24.0),
		withObsHigh(21.0),
		withLatestObserved(20.8),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(21.2),
	))

	assertValidDistribution(t, d)

	if d.ExpectedHighC <= 21.5 {
		t.Errorf("midday New York summary should not trigger late-day collapse, got %.4f", d.ExpectedHighC)
	}
}

func TestBuildBucketDistribution_PrefersResolutionHighOverMetarHigh(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local15UTC := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)

	d := svc.Build(makeSummary(
		withGeneratedAt(local15UTC),
		withForecastHigh(26.0),
		withObsHigh(30.0),
		withResolutionHigh(26.0),
		withLatestObserved(30.0),
		withTempChange3h(0.0),
		withObsPoints(32),
		withRemainingForecastHigh(26.2),
	))

	assertValidDistribution(t, d)

	if got := sumProbUpTo(d.BucketProbs, 25); !nearF(got, 0, 1e-9) {
		t.Errorf("resolution high should set the settlement floor at 26C, <=25C got %.6f", got)
	}
	if got := findProb(d.BucketProbs, "26C"); got < 0.80 {
		t.Errorf("26C should dominate when Wunderground resolution high is 26C, got %.4f", got)
	}
	if got := sumProbFrom(d.BucketProbs, 30); got > 0.01 {
		t.Errorf("METAR-only 30C should not remain the settlement floor when Wunderground is present, got %.4f", got)
	}
}

func TestBuildBucketDistribution_CoastalProfileNarrowsAt1330(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local1330UTC := time.Date(2026, 5, 5, 5, 30, 0, 0, time.UTC)

	coastal := svc.Build(makeSummary(
		withGeneratedAt(local1330UTC),
		withTemperatureProfile(TemperatureProfileCoastalFastLock),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withLatestObserved(24.7),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(25.2),
	))
	basin := svc.Build(makeSummary(
		withGeneratedAt(local1330UTC),
		withTemperatureProfile(TemperatureProfileBasinInland),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withLatestObserved(24.7),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(25.2),
	))

	assertValidDistribution(t, coastal)
	assertValidDistribution(t, basin)

	coastalObservedProb := findProb(coastal.BucketProbs, "25C")
	basinObservedProb := findProb(basin.BucketProbs, "25C")
	if coastalObservedProb < 0.80 {
		t.Errorf("coastal 13:30 should strongly concentrate on observed bucket, got %.4f", coastalObservedProb)
	}
	if coastalObservedProb <= basinObservedProb {
		t.Errorf("coastal 13:30 should be narrower than basin 13:30: coastal %.4f basin %.4f",
			coastalObservedProb, basinObservedProb)
	}
}

func TestBuildBucketDistribution_CoastalProfileHardLocksAfter1415(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local1415UTC := time.Date(2026, 5, 5, 6, 15, 0, 0, time.UTC)

	d := svc.Build(makeSummary(
		withGeneratedAt(local1415UTC),
		withTemperatureProfile(TemperatureProfileCoastalFastLock),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withLatestObserved(24.7),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(25.2),
	))

	assertValidDistribution(t, d)

	if got := findProb(d.BucketProbs, "25C"); got < 0.90 {
		t.Errorf("coastal 14:15 stable high should hard-lock near observed bucket, got %.4f", got)
	}
	if got := sumProbFrom(d.BucketProbs, 27); got > 0.01 {
		t.Errorf("coastal 14:15 upside >=27C should be strongly reduced, got %.4f", got)
	}
}

func TestBuildBucketDistribution_BasinProfileKeepsWarmingUpsideAt1415(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local1415UTC := time.Date(2026, 5, 5, 6, 15, 0, 0, time.UTC)

	d := svc.Build(makeSummary(
		withGeneratedAt(local1415UTC),
		withTemperatureProfile(TemperatureProfileBasinInland),
		withForecastHigh(30.0),
		withObsHigh(30.0),
		withLatestObserved(30.0),
		withTempChange3h(3.0),
		withObsPoints(16),
		withRemainingForecastHigh(31.4),
	))

	assertValidDistribution(t, d)

	if d.ExpectedHighC < 30.8 {
		t.Errorf("basin 14:15 strong warming should retain upside center, got %.4f", d.ExpectedHighC)
	}
	if got := findProb(d.BucketProbs, "31C"); got < 0.45 {
		t.Errorf("basin 14:15 strong warming should keep 31C live, got %.4f", got)
	}
}

func TestBuildBucketDistribution_NorthProfileSoftLocksAt1400ButKeepsWarmingUpside(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local1400UTC := time.Date(2026, 5, 5, 6, 0, 0, 0, time.UTC)

	stable := svc.Build(makeSummary(
		withGeneratedAt(local1400UTC),
		withTemperatureProfile(TemperatureProfileNorthInland),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withLatestObserved(24.6),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(25.2),
	))
	warming := svc.Build(makeSummary(
		withGeneratedAt(local1400UTC),
		withTemperatureProfile(TemperatureProfileNorthInland),
		withForecastHigh(30.0),
		withObsHigh(30.0),
		withLatestObserved(30.0),
		withTempChange3h(3.0),
		withObsPoints(16),
		withRemainingForecastHigh(31.2),
	))

	assertValidDistribution(t, stable)
	assertValidDistribution(t, warming)

	if got := findProb(stable.BucketProbs, "25C"); got < 0.75 {
		t.Errorf("north inland 14:00 stable high should start narrowing, got %.4f", got)
	}
	if got := findProb(warming.BucketProbs, "31C"); got < 0.35 {
		t.Errorf("north inland 14:00 strong warming should keep adjacent upside, got %.4f", got)
	}
}

func TestBuildBucketDistribution_HumidSouthBetweenCoastalAndBasin(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	local1330UTC := time.Date(2026, 5, 5, 5, 30, 0, 0, time.UTC)

	baseOpts := []summaryOption{
		withGeneratedAt(local1330UTC),
		withForecastHigh(28.0),
		withObsHigh(25.0),
		withLatestObserved(24.6),
		withTempChange3h(0.0),
		withObsPoints(16),
		withRemainingForecastHigh(25.2),
	}
	coastalOpts := append([]summaryOption{}, baseOpts...)
	coastalOpts = append(coastalOpts, withTemperatureProfile(TemperatureProfileCoastalFastLock))
	humidOpts := append([]summaryOption{}, baseOpts...)
	humidOpts = append(humidOpts, withTemperatureProfile(TemperatureProfileHumidSouth))
	basinOpts := append([]summaryOption{}, baseOpts...)
	basinOpts = append(basinOpts, withTemperatureProfile(TemperatureProfileBasinInland))

	coastal := svc.Build(makeSummary(coastalOpts...))
	humid := svc.Build(makeSummary(humidOpts...))
	basin := svc.Build(makeSummary(basinOpts...))

	assertValidDistribution(t, coastal)
	assertValidDistribution(t, humid)
	assertValidDistribution(t, basin)

	coastalObserved := findProb(coastal.BucketProbs, "25C")
	humidObserved := findProb(humid.BucketProbs, "25C")
	basinObserved := findProb(basin.BucketProbs, "25C")
	if !(coastalObserved > humidObserved && humidObserved > basinObserved) {
		t.Errorf("want coastal > humid_south > basin observed-bucket concentration, got %.4f %.4f %.4f",
			coastalObserved, humidObserved, basinObserved)
	}
}

func TestBuildBucketDistribution_LateHistoricalResolutionLocksObservedBucket(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	generatedAt := time.Date(2026, 5, 5, 13, 40, 0, 0, time.UTC) // 21:40 Asia/Shanghai

	d := svc.Build(makeSummary(
		withForecastHigh(24.2),
		withResolutionHigh(23.0),
		withResolutionSourceType(wunderground.SourceTypeHistoricalObservations),
		withTempChange3h(0.0),
		withLatestObserved(22.0),
		withRemainingForecastHigh(21.9),
		withObsPoints(47),
		withTimezone(chinaTimezone),
		withTemperatureProfile(TemperatureProfileHumidSouth),
		withGeneratedAt(generatedAt),
	))

	assertValidDistribution(t, d)

	if !nearF(d.ExpectedHighC, 23.0, 1e-9) {
		t.Errorf("ExpectedHighC: got %.4f, want locked historical high 23.0", d.ExpectedHighC)
	}
	if got := findProb(d.BucketProbs, "23C"); got < 0.95 {
		t.Errorf("23C should dominate after late historical lock, got %.4f", got)
	}
	if got := findProb(d.BucketProbs, "24C"); got > 0.05 {
		t.Errorf("24C upside should be tiny after late historical lock, got %.4f", got)
	}
}

// TestBuildBucketDistribution_MetadataPropagation verifies that station code,
// target date, and generated-at are correctly propagated to the output.
func TestBuildBucketDistribution_MetadataPropagation(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	s := makeSummary()
	d := svc.Build(s)

	if d.StationCode != s.StationCode {
		t.Errorf("StationCode: got %q, want %q", d.StationCode, s.StationCode)
	}
	if d.TargetDateLocal != s.TargetDateLocal {
		t.Errorf("TargetDateLocal: got %q, want %q", d.TargetDateLocal, s.TargetDateLocal)
	}
	if d.GeneratedAt.IsZero() {
		t.Error("GeneratedAt is zero")
	}
}

// TestNormalCDF verifies the Gaussian CDF helper against known standard-normal
// values and the exact midpoint symmetry.
func TestNormalCDF(t *testing.T) {
	tests := []struct {
		desc       string
		x, mean, σ float64
		want       float64
		tol        float64
	}{
		{"midpoint symmetry", 0, 0, 1, 0.5, 1e-15},
		{"+1σ", 1, 0, 1, 0.8413447, 1e-6},
		{"-1σ", -1, 0, 1, 0.1586553, 1e-6},
		{"+2σ", 2, 0, 1, 0.9772499, 1e-6},
		{"shifted mean", 18.5, 18.0, 1.0, 0.6914625, 1e-6},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := normalCDF(tc.x, tc.mean, tc.σ)
			if !nearF(got, tc.want, tc.tol) {
				t.Errorf("normalCDF(%.1f, %.1f, %.1f): got %.8f, want %.8f (tol %.0e)",
					tc.x, tc.mean, tc.σ, got, tc.want, tc.tol)
			}
		})
	}
}

// TestBuildBucketDistribution_NilSummaryPanics asserts that Build panics with a
// clear message when summary is nil, making the contract explicit and
// distinguishable from an accidental nil-dereference.
func TestBuildBucketDistribution_NilSummaryPanics(t *testing.T) {
	svc := NewBuildBucketDistributionService()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Build(nil) should panic, but it did not")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "must not be nil") {
			t.Errorf("unexpected panic value: %v", r)
		}
	}()
	svc.Build(nil)
}

// TestFindProb verifies the findProb helper returns -1 for missing labels.
func TestFindProb(t *testing.T) {
	probs := []domain.BucketProbability{
		{Label: bucketLabel(bucketFloorC), Prob: 0.10},
		{Label: "18C", Prob: 0.40},
		{Label: "19C", Prob: 0.35},
		{Label: bucketLabel(bucketCeilingC), Prob: 0.15},
	}

	if got := findProb(probs, "18C"); !nearF(got, 0.40, 1e-9) {
		t.Errorf("findProb(\"18C\"): got %.4f, want 0.40", got)
	}
	if got := findProb(probs, "99C"); got != -1 {
		t.Errorf("findProb missing label: got %.4f, want -1", got)
	}
	if got := totalProb(probs); !nearF(got, 1.0, 1e-9) {
		t.Errorf("totalProb: got %.10f, want 1.0", got)
	}
}
