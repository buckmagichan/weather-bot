// Package domain defines the core data types for the weather-bot service.
package domain

import "time"

// ForecastSnapshot is a point-in-time weather forecast captured for a single
// station day. The field layout is intentionally flat so it maps cleanly to a
// database row when persistence is added later.
type ForecastSnapshot struct {
	StationCode     string    // e.g. "ZSPD"
	TargetDateLocal string    // YYYY-MM-DD in the station's local timezone
	FetchedAt       time.Time // UTC timestamp of when the fetch was performed
	Timezone        string    // IANA timezone name returned by the API

	ForecastHighC float64 // daily maximum temperature in °C

	// Parallel hourly slices — all have the same length (one entry per hour).
	// Wind speed is in km/h (Open-Meteo default unit).
	HourlyTime       []time.Time
	HourlyTempC      []float64
	HourlyDewPointC  []float64
	HourlyCloudCover []int
	HourlyPrecipProb []int
	HourlyWindKMH    []float64
}
