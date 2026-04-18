package domain

import "time"

// ObservationSnapshot is a single weather observation for a station.
// Nullable fields use pointers because upstream providers may omit ancillary
// variables for some stations or time periods.
//
// CloudCover is optional provider-specific cloud metadata; when populated it is
// not necessarily a percentage.
type ObservationSnapshot struct {
	StationCode string
	ObservedAt  time.Time // wall-clock time in the station's local timezone
	Timezone    string    // IANA timezone name

	TempC      float64  // temperature in °C (always present)
	DewPointC  *float64 // dew point in °C
	WindKMH    *float64 // wind speed in km/h
	CloudCover *int     // optional provider-specific cloud metadata
	PrecipMM   *float64 // precipitation in mm
}
