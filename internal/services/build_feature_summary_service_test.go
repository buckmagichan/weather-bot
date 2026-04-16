package services

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/repository"
)

// --- Fakes ----------------------------------------------------------------

type fakeForecastStore struct {
	latestRow   repository.ForecastRow
	latestFound bool
	latestErr   error

	previousRow   repository.ForecastRow
	previousFound bool
	previousErr   error
}

func (f *fakeForecastStore) GetLatestForDate(_ context.Context, _, _ string) (repository.ForecastRow, bool, error) {
	return f.latestRow, f.latestFound, f.latestErr
}

func (f *fakeForecastStore) GetPreviousForDate(_ context.Context, _, _ string) (repository.ForecastRow, bool, error) {
	return f.previousRow, f.previousFound, f.previousErr
}

type fakeObservationStore struct {
	obs []domain.ObservationSnapshot
	err error
}

func (f *fakeObservationStore) ListForDate(_ context.Context, _, _ string, _ *time.Location) ([]domain.ObservationSnapshot, error) {
	return f.obs, f.err
}

// --- Helpers --------------------------------------------------------------

// near reports whether a and b are within 1e-6 of each other.
// Used for float64 comparisons where IEEE-754 rounding may cause tiny deltas.
func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

// pf returns a pointer to the given float64 literal.
func pf(v float64) *float64 { return &v }

// baseObs returns 4 hourly observations anchored at t0, covering a 3-hour
// window: t0, t0+1h, t0+2h, t0+3h with temps 14.0, 14.8, 15.5, 16.1.
// The user-requested fixture values that give TempChangeLast3hC = 2.1
// when all four observations are included.
func baseObs(t0 time.Time) []domain.ObservationSnapshot {
	return []domain.ObservationSnapshot{
		{StationCode: "ZSPD", ObservedAt: t0, Timezone: "Asia/Shanghai", TempC: 14.0},
		{StationCode: "ZSPD", ObservedAt: t0.Add(1 * time.Hour), Timezone: "Asia/Shanghai", TempC: 14.8},
		{StationCode: "ZSPD", ObservedAt: t0.Add(2 * time.Hour), Timezone: "Asia/Shanghai", TempC: 15.5},
		{StationCode: "ZSPD", ObservedAt: t0.Add(3 * time.Hour), Timezone: "Asia/Shanghai", TempC: 16.1},
	}
}

// newSvc is a test helper that builds a service with the given fakes.
func newSvc(t *testing.T, fc forecastStore, oc observationStore) *BuildFeatureSummaryService {
	t.Helper()
	svc, err := NewBuildFeatureSummaryService(fc, oc)
	if err != nil {
		t.Fatalf("NewBuildFeatureSummaryService: %v", err)
	}
	return svc
}

// --- Tests ----------------------------------------------------------------

func TestBuildFeatureSummary(t *testing.T) {
	// A fixed base time for observations: 2026-04-14 10:00 UTC
	t0 := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	fetchedAt := time.Date(2026, 4, 14, 8, 0, 0, 0, time.UTC)

	latestForecastRow := repository.ForecastRow{
		ForecastHighC: 17.2,
		FetchedAt:     fetchedAt,
		HourlyPoints:  24,
	}
	prevForecastRow := repository.ForecastRow{
		ForecastHighC: 17.0,
		FetchedAt:     fetchedAt.Add(-6 * time.Hour),
		HourlyPoints:  24,
	}

	tests := []struct {
		name    string
		fc      *fakeForecastStore
		oc      *fakeObservationStore
		now     time.Time // reference time passed to Build; controls "observed so far" cutoff
		check   func(t *testing.T, s *domain.WeatherFeatureSummary)
		wantErr bool
	}{
		{
			// Case A: full data — latest forecast + previous forecast + observations.
			// now is after all 4 observations, so none are filtered out.
			name: "A_all_data_present",
			fc: &fakeForecastStore{
				latestRow: latestForecastRow, latestFound: true,
				previousRow: prevForecastRow, previousFound: true,
			},
			oc:  &fakeObservationStore{obs: baseObs(t0)},
			now: t0.Add(4 * time.Hour), // 14:00 UTC — after the last obs at t0+3h (13:00)
			check: func(t *testing.T, s *domain.WeatherFeatureSummary) {
				t.Helper()
				if !near(s.LatestForecastHighC, 17.2) {
					t.Errorf("LatestForecastHighC: got %.4f, want 17.2", s.LatestForecastHighC)
				}
				if s.PreviousForecastHighC == nil || !near(*s.PreviousForecastHighC, 17.0) {
					t.Errorf("PreviousForecastHighC: got %v, want 17.0", s.PreviousForecastHighC)
				}
				// 17.2 - 17.0 ≈ 0.2
				if s.ForecastTrendC == nil || !near(*s.ForecastTrendC, 0.2) {
					t.Errorf("ForecastTrendC: got %v, want ~0.2", s.ForecastTrendC)
				}
				if s.LatestObservedTempC == nil || !near(*s.LatestObservedTempC, 16.1) {
					t.Errorf("LatestObservedTempC: got %v, want 16.1", s.LatestObservedTempC)
				}
				if s.ObservedHighSoFarC == nil || !near(*s.ObservedHighSoFarC, 16.1) {
					t.Errorf("ObservedHighSoFarC: got %v, want 16.1", s.ObservedHighSoFarC)
				}
				// All 4 obs (t0…t0+3h) fall in the [t0+3h-3h, t0+3h] = [t0, t0+3h] window.
				// delta = 16.1 - 14.0 = 2.1
				if s.TempChangeLast3hC == nil || !near(*s.TempChangeLast3hC, 2.1) {
					t.Errorf("TempChangeLast3hC: got %v, want ~2.1", s.TempChangeLast3hC)
				}
				if s.HourlyPoints != 24 {
					t.Errorf("HourlyPoints: got %d, want 24", s.HourlyPoints)
				}
				if s.ObservationPoints != 4 {
					t.Errorf("ObservationPoints: got %d, want 4", s.ObservationPoints)
				}
				if !s.ForecastSnapshotFetchedAt.Equal(fetchedAt) {
					t.Errorf("ForecastSnapshotFetchedAt: got %v, want %v", s.ForecastSnapshotFetchedAt, fetchedAt)
				}
				if s.LatestObservationAt == nil {
					t.Error("LatestObservationAt is nil")
				}
				if s.StationCode != "ZSPD" {
					t.Errorf("StationCode: got %q, want %q", s.StationCode, "ZSPD")
				}
			},
		},
		{
			// Case B: no previous forecast — trend fields must be nil.
			name: "B_no_previous_forecast",
			fc: &fakeForecastStore{
				latestRow: latestForecastRow, latestFound: true,
				previousFound: false,
			},
			oc:  &fakeObservationStore{obs: baseObs(t0)},
			now: t0.Add(4 * time.Hour), // after all observations
			check: func(t *testing.T, s *domain.WeatherFeatureSummary) {
				t.Helper()
				if !near(s.LatestForecastHighC, 17.2) {
					t.Errorf("LatestForecastHighC: got %.4f, want 17.2", s.LatestForecastHighC)
				}
				if s.PreviousForecastHighC != nil {
					t.Errorf("PreviousForecastHighC: want nil, got %v", *s.PreviousForecastHighC)
				}
				if s.ForecastTrendC != nil {
					t.Errorf("ForecastTrendC: want nil, got %v", *s.ForecastTrendC)
				}
				// Observation fields should still be populated.
				if s.LatestObservedTempC == nil {
					t.Error("LatestObservedTempC: want value, got nil")
				}
				if s.ObservationPoints != 4 {
					t.Errorf("ObservationPoints: got %d, want 4", s.ObservationPoints)
				}
			},
		},
		{
			// Case C: no observations — all obs-derived fields must be nil.
			name: "C_no_observations",
			fc: &fakeForecastStore{
				latestRow: latestForecastRow, latestFound: true,
				previousRow: prevForecastRow, previousFound: true,
			},
			oc:  &fakeObservationStore{obs: nil},
			now: t0.Add(4 * time.Hour),
			check: func(t *testing.T, s *domain.WeatherFeatureSummary) {
				t.Helper()
				if !near(s.LatestForecastHighC, 17.2) {
					t.Errorf("LatestForecastHighC: got %.4f, want 17.2", s.LatestForecastHighC)
				}
				if s.LatestObservedTempC != nil {
					t.Errorf("LatestObservedTempC: want nil, got %v", *s.LatestObservedTempC)
				}
				if s.ObservedHighSoFarC != nil {
					t.Errorf("ObservedHighSoFarC: want nil, got %v", *s.ObservedHighSoFarC)
				}
				if s.TempChangeLast3hC != nil {
					t.Errorf("TempChangeLast3hC: want nil, got %v", *s.TempChangeLast3hC)
				}
				if s.LatestObservationAt != nil {
					t.Errorf("LatestObservationAt: want nil, got %v", *s.LatestObservationAt)
				}
				if s.ObservationPoints != 0 {
					t.Errorf("ObservationPoints: got %d, want 0", s.ObservationPoints)
				}
			},
		},
		{
			// Case D: observations exist but only 1 falls within the last 3 hours.
			// The observation at t0 (10:00) is 4 hours before the latest (14:00),
			// so cutoff = 11:00 excludes it. Window has only [14:00] → trend is nil.
			name: "D_insufficient_3h_window",
			fc: &fakeForecastStore{
				latestRow: latestForecastRow, latestFound: true,
			},
			oc: &fakeObservationStore{obs: []domain.ObservationSnapshot{
				{StationCode: "ZSPD", ObservedAt: t0, Timezone: "Asia/Shanghai", TempC: 12.0},
				{StationCode: "ZSPD", ObservedAt: t0.Add(4 * time.Hour), Timezone: "Asia/Shanghai", TempC: 16.1},
			}},
			now: t0.Add(5 * time.Hour), // after both observations
			check: func(t *testing.T, s *domain.WeatherFeatureSummary) {
				t.Helper()
				if s.TempChangeLast3hC != nil {
					t.Errorf("TempChangeLast3hC: want nil (only 1 obs in window), got %v", *s.TempChangeLast3hC)
				}
				// Other observation fields must still be populated.
				if s.LatestObservedTempC == nil || !near(*s.LatestObservedTempC, 16.1) {
					t.Errorf("LatestObservedTempC: got %v, want 16.1", s.LatestObservedTempC)
				}
				// ObservedHighSoFarC is max of all obs (12.0 and 16.1) = 16.1
				if s.ObservedHighSoFarC == nil || !near(*s.ObservedHighSoFarC, 16.1) {
					t.Errorf("ObservedHighSoFarC: got %v, want 16.1", s.ObservedHighSoFarC)
				}
				if s.ObservationPoints != 2 {
					t.Errorf("ObservationPoints: got %d, want 2", s.ObservationPoints)
				}
			},
		},
		{
			// Case E: no forecast snapshot exists — Build must return an error.
			name:    "E_no_forecast",
			fc:      &fakeForecastStore{latestFound: false},
			oc:      &fakeObservationStore{},
			now:     t0,
			wantErr: true,
			check: func(t *testing.T, s *domain.WeatherFeatureSummary) {
				// summary is nil when an error is returned; nothing to check here.
			},
		},
		{
			// Case F: the DB contains observations for the full target date, but two
			// of them (t0+3h) are in the future relative to now (t0+2h30m).
			// Only the first three observations must be used for all derived fields.
			// This is the regression test for the "future observations included" bug.
			name: "F_future_observations_excluded",
			fc: &fakeForecastStore{
				latestRow: latestForecastRow, latestFound: true,
			},
			oc: &fakeObservationStore{obs: baseObs(t0)}, // [t0, t0+1h, t0+2h, t0+3h]
			// now is between the third and fourth observations (12:30 UTC).
			// t0+3h = 13:00 is strictly after now, so it must be excluded.
			now: t0.Add(2*time.Hour + 30*time.Minute),
			check: func(t *testing.T, s *domain.WeatherFeatureSummary) {
				t.Helper()

				// Only t0, t0+1h, t0+2h qualify.
				if s.ObservationPoints != 3 {
					t.Errorf("ObservationPoints: got %d, want 3", s.ObservationPoints)
				}

				// Latest observed temp is from t0+2h (15.5 C), not t0+3h (16.1 C).
				if s.LatestObservedTempC == nil || !near(*s.LatestObservedTempC, 15.5) {
					t.Errorf("LatestObservedTempC: got %v, want 15.5", s.LatestObservedTempC)
				}

				// High so far is max(14.0, 14.8, 15.5) = 15.5, not 16.1.
				if s.ObservedHighSoFarC == nil || !near(*s.ObservedHighSoFarC, 15.5) {
					t.Errorf("ObservedHighSoFarC: got %v, want 15.5", s.ObservedHighSoFarC)
				}

				// LatestObservationAt must equal t0+2h and must not exceed now.
				if s.LatestObservationAt == nil {
					t.Fatal("LatestObservationAt is nil")
				}
				wantLatest := t0.Add(2 * time.Hour)
				if !s.LatestObservationAt.Equal(wantLatest) {
					t.Errorf("LatestObservationAt: got %v, want %v", *s.LatestObservationAt, wantLatest)
				}
				if s.LatestObservationAt.After(t0.Add(2*time.Hour + 30*time.Minute)) {
					t.Errorf("LatestObservationAt %v is after now", *s.LatestObservationAt)
				}

				// 3h window: latest = t0+2h (12:00), cutoff = t0-1h (09:00).
				// All three soFar obs (10:00, 11:00, 12:00) fall in the window.
				// delta = 15.5 - 14.0 = 1.5
				if s.TempChangeLast3hC == nil || !near(*s.TempChangeLast3hC, 1.5) {
					t.Errorf("TempChangeLast3hC: got %v, want 1.5", s.TempChangeLast3hC)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newSvc(t, tc.fc, tc.oc)
			summary, err := svc.Build(context.Background(), "ZSPD", "2026-04-14", tc.now)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if summary == nil {
				t.Fatal("summary is nil")
			}
			tc.check(t, summary)
		})
	}
}

// TestComputeTempChangeLast3h tests the private helper directly, covering
// edge cases that are hard to exercise through Build.
func TestComputeTempChangeLast3h(t *testing.T) {
	t0 := time.Date(2026, 4, 14, 13, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		obs  []domain.ObservationSnapshot
		want *float64 // nil means expect nil
	}{
		{
			name: "empty",
			obs:  nil,
			want: nil,
		},
		{
			name: "single_observation",
			obs: []domain.ObservationSnapshot{
				{ObservedAt: t0, TempC: 16.0},
			},
			want: nil,
		},
		{
			name: "two_obs_in_window",
			obs: []domain.ObservationSnapshot{
				{ObservedAt: t0.Add(-2 * time.Hour), TempC: 14.0},
				{ObservedAt: t0, TempC: 16.0},
			},
			want: pf(2.0), // 16.0 - 14.0 = 2.0
		},
		{
			name: "older_obs_outside_window",
			obs: []domain.ObservationSnapshot{
				{ObservedAt: t0.Add(-4 * time.Hour), TempC: 12.0}, // excluded: 4h > 3h
				{ObservedAt: t0, TempC: 16.0},
			},
			want: nil, // only 1 in window
		},
		{
			name: "boundary_obs_at_exactly_minus_3h",
			obs: []domain.ObservationSnapshot{
				{ObservedAt: t0.Add(-3 * time.Hour), TempC: 13.0}, // at cutoff: inclusive
				{ObservedAt: t0, TempC: 16.0},
			},
			want: pf(3.0), // 16.0 - 13.0 = 3.0
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeTempChangeLast3h(tc.obs)
			if tc.want == nil {
				if got != nil {
					t.Errorf("want nil, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %v, got nil", *tc.want)
			}
			if !near(*got, *tc.want) {
				t.Errorf("got %.4f, want %.4f", *got, *tc.want)
			}
		})
	}
}
