package services

import (
	"context"
	"fmt"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/repository"
)

const summarySvcTimezone = "Asia/Shanghai"

// forecastStore is the minimal read interface for forecast snapshot data.
// Defined in the consumer (services) package so tests can supply fakes without
// importing the real repository.
type forecastStore interface {
	GetLatestForDate(ctx context.Context, stationCode, targetDate string) (repository.ForecastRow, bool, error)
	GetPreviousForDate(ctx context.Context, stationCode, targetDate string) (repository.ForecastRow, bool, error)
}

// observationStore is the minimal read interface for observation snapshot data.
type observationStore interface {
	ListForDate(ctx context.Context, stationCode, targetDate string, loc *time.Location) ([]domain.ObservationSnapshot, error)
}

// BuildFeatureSummaryService computes a WeatherFeatureSummary from the most
// recent forecast and observation data stored in PostgreSQL.
type BuildFeatureSummaryService struct {
	forecastRepo forecastStore
	obsRepo      observationStore
	loc          *time.Location
}

// NewBuildFeatureSummaryService constructs the service. The Asia/Shanghai
// location is loaded once and cached in the struct. The concrete
// *repository.ForecastSnapshotRepo and *repository.ObservationSnapshotRepo
// satisfy the interfaces, so callers in main.go need no changes.
func NewBuildFeatureSummaryService(
	forecastRepo forecastStore,
	obsRepo observationStore,
) (*BuildFeatureSummaryService, error) {
	loc, err := time.LoadLocation(summarySvcTimezone)
	if err != nil {
		return nil, fmt.Errorf("build feature summary service: load timezone: %w", err)
	}
	return &BuildFeatureSummaryService{
		forecastRepo: forecastRepo,
		obsRepo:      obsRepo,
		loc:          loc,
	}, nil
}

// Build assembles a WeatherFeatureSummary for the given station and local date.
// now is the wall-clock reference time for "observed so far" filtering: only
// observations with observed_at <= now are used for the observation-derived
// fields (LatestObservedTempC, ObservedHighSoFarC, TempChangeLast3hC, etc.).
// Pass time.Now() in production; pass a fixed time in tests.
// Missing data never causes an error — unavailable pointer fields are left nil.
func (s *BuildFeatureSummaryService) Build(
	ctx context.Context,
	stationCode, targetDate string,
	now time.Time,
) (*domain.WeatherFeatureSummary, error) {
	summary := &domain.WeatherFeatureSummary{
		StationCode:     stationCode,
		TargetDateLocal: targetDate,
		GeneratedAt:     now.UTC(),
	}

	// --- Latest forecast snapshot ---
	latest, found, err := s.forecastRepo.GetLatestForDate(ctx, stationCode, targetDate)
	if err != nil {
		return nil, fmt.Errorf("build feature summary: get latest forecast: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("build feature summary: no forecast snapshot found for %s on %s", stationCode, targetDate)
	}
	summary.LatestForecastHighC = latest.ForecastHighC
	summary.ForecastSnapshotFetchedAt = latest.FetchedAt
	summary.HourlyPoints = latest.HourlyPoints

	// --- Previous forecast snapshot (optional) ---
	prev, prevFound, err := s.forecastRepo.GetPreviousForDate(ctx, stationCode, targetDate)
	if err != nil {
		return nil, fmt.Errorf("build feature summary: get previous forecast: %w", err)
	}
	if prevFound {
		highC := prev.ForecastHighC
		summary.PreviousForecastHighC = &highC
		trend := latest.ForecastHighC - prev.ForecastHighC
		summary.ForecastTrendC = &trend
	}

	// --- Observations for the day ---
	// ListForDate returns all stored rows for the target local date. We then
	// restrict to "observed so far": rows whose observed_at is at or before now.
	// This prevents Meteostat data for future hours of the same calendar day
	// from contaminating derived fields like ObservedHighSoFarC.
	obs, err := s.obsRepo.ListForDate(ctx, stationCode, targetDate, s.loc)
	if err != nil {
		return nil, fmt.Errorf("build feature summary: list observations: %w", err)
	}

	var soFar []domain.ObservationSnapshot
	for _, o := range obs {
		if !o.ObservedAt.After(now) {
			soFar = append(soFar, o)
		}
	}
	// ObservationPoints counts only the "observed so far" subset.
	summary.ObservationPoints = len(soFar)

	if len(soFar) == 0 {
		return summary, nil
	}

	// soFar is sorted ASC by observed_at; the last element is the most recent.
	last := soFar[len(soFar)-1]
	latestTemp := last.TempC
	latestAt := last.ObservedAt
	summary.LatestObservedTempC = &latestTemp
	summary.LatestObservationAt = &latestAt

	// Observed high: max TempC across all "observed so far" rows.
	high := soFar[0].TempC
	for _, o := range soFar[1:] {
		if o.TempC > high {
			high = o.TempC
		}
	}
	summary.ObservedHighSoFarC = &high

	// 3-hour temperature change — restricted to the "observed so far" window.
	summary.TempChangeLast3hC = computeTempChangeLast3h(soFar)

	return summary, nil
}

// computeTempChangeLast3h returns the temperature change over the last 3 hours:
// latestTemp - oldestTempInWindow, where the window is [latestObservedAt-3h, latestObservedAt].
// Returns nil when fewer than 2 observations fall within the window — a single
// point has no meaningful trend.
//
// obs must be sorted by ObservedAt ASC (as returned by ListForDate).
func computeTempChangeLast3h(obs []domain.ObservationSnapshot) *float64 {
	if len(obs) < 2 {
		return nil
	}
	latest := obs[len(obs)-1]
	cutoff := latest.ObservedAt.Add(-3 * time.Hour)

	// Collect all observations at or after the cutoff (inclusive).
	var window []domain.ObservationSnapshot
	for _, o := range obs {
		if !o.ObservedAt.Before(cutoff) {
			window = append(window, o)
		}
	}
	if len(window) < 2 {
		return nil
	}
	delta := window[len(window)-1].TempC - window[0].TempC
	return &delta
}
