package services

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/buckmagichan/weather-bot/internal/domain"
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
func withTempChange3h(c float64) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.TempChangeLast3hC = fp(c) }
}
func withObsPoints(n int) summaryOption {
	return func(s *domain.WeatherFeatureSummary) { s.ObservationPoints = n }
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
	const wantBuckets = 4
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

				wantLabels := []string{"17C or below", "18C", "19C", "20C or above"}
				for i, want := range wantLabels {
					if got := d.BucketProbs[i].Label; got != want {
						t.Errorf("bucket[%d] label: got %q, want %q", i, got, want)
					}
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
				// Warmer buckets (19C, 20C+) should gain probability.
				for _, label := range []string{"19C", "20C or above"} {
					got := findProb(d.BucketProbs, label)
					base := findProb(baseline.BucketProbs, label)
					if got <= base {
						t.Errorf("positive trend: %q prob should increase: got %.4f, baseline %.4f",
							label, got, base)
					}
				}
				// Cooler bucket (17C or below) should lose probability.
				got := findProb(d.BucketProbs, "17C or below")
				base := findProb(baseline.BucketProbs, "17C or below")
				if got >= base {
					t.Errorf("positive trend: \"17C or below\" prob should decrease: got %.4f, baseline %.4f",
						got, base)
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
				// Cooler bucket (17C or below) should gain probability.
				got := findProb(d.BucketProbs, "17C or below")
				base := findProb(baseline.BucketProbs, "17C or below")
				if got <= base {
					t.Errorf("negative trend: \"17C or below\" prob should increase: got %.4f, baseline %.4f",
						got, base)
				}
				// Warmer buckets should lose probability.
				for _, label := range []string{"19C", "20C or above"} {
					got := findProb(d.BucketProbs, label)
					base := findProb(baseline.BucketProbs, label)
					if got >= base {
						t.Errorf("negative trend: %q prob should decrease: got %.4f, baseline %.4f",
							label, got, base)
					}
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
				if !nearF(d.ExpectedHighC, baseline.ExpectedHighC, 1e-6) {
					t.Errorf("obs close to forecast should not change ExpectedHighC: got %.4f, baseline %.4f",
						d.ExpectedHighC, baseline.ExpectedHighC)
				}
				// Narrower spread → lower-tail probability decreases (downside risk reduced).
				gotLow := findProb(d.BucketProbs, "17C or below")
				baseLow := findProb(baseline.BucketProbs, "17C or below")
				if gotLow >= baseLow {
					t.Errorf("obs close to forecast: \"17C or below\" should decrease (narrower spread): got %.4f, baseline %.4f",
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
				// Same spread, higher center → upper buckets gain probability.
				for _, label := range []string{"19C", "20C or above"} {
					got := findProb(d.BucketProbs, label)
					base := findProb(baseline.BucketProbs, label)
					if got <= base {
						t.Errorf("3h warming: %q prob should increase: got %.4f, baseline %.4f",
							label, got, base)
					}
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

	c0 := svc.Build(makeSummary()).Confidence                                                                                                       // 0.50
	c1 := svc.Build(makeSummary(withPreviousForecastHigh(17.5))).Confidence                                                                        // 0.65
	c2 := svc.Build(makeSummary(withPreviousForecastHigh(17.5), withObsPoints(4))).Confidence                                                      // 0.80
	c3 := svc.Build(makeSummary(withPreviousForecastHigh(17.5), withObsPoints(4), withTempChange3h(0.5))).Confidence                               // 0.90

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
		desc        string
		x, mean, σ float64
		want        float64
		tol         float64
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
		{Label: "17C or below", Prob: 0.10},
		{Label: "18C", Prob: 0.40},
		{Label: "19C", Prob: 0.35},
		{Label: "20C or above", Prob: 0.15},
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
