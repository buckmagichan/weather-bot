package domain

import "time"

// WeatherFeatureSummary is a derived view over the most recent forecast and
// observation data for a single (station, date) pair.
// Pointer fields are nil when the underlying data is unavailable — callers
// must treat nil as "no data yet", never as an error.
type WeatherFeatureSummary struct {
	StationCode     string    `json:"station_code"`
	TargetDateLocal string    `json:"target_date_local"` // YYYY-MM-DD in local station time
	GeneratedAt     time.Time `json:"generated_at"`      // UTC timestamp of when Build was called

	// Forecast fields — sourced from forecast_snapshots.
	LatestForecastHighC   float64  `json:"latest_forecast_high_c"`
	PreviousForecastHighC *float64 `json:"previous_forecast_high_c"` // nil if only one snapshot exists
	ForecastTrendC        *float64 `json:"forecast_trend_c"`          // nil if Previous is nil

	// Observation fields — sourced from observation_snapshots.
	LatestObservedTempC *float64 `json:"latest_observed_temp_c"`  // nil if no observations
	ObservedHighSoFarC  *float64 `json:"observed_high_so_far_c"`  // nil if no observations
	TempChangeLast3hC   *float64 `json:"temp_change_last_3h_c"`   // nil if <2 observations in window

	// Metadata / diagnostics.
	ForecastSnapshotFetchedAt time.Time  `json:"forecast_snapshot_fetched_at"`
	LatestObservationAt       *time.Time `json:"latest_observation_at"`  // nil if no observations
	HourlyPoints              int        `json:"hourly_points"`
	ObservationPoints         int        `json:"observation_points"`
}
