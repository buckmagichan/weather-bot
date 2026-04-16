package domain

import "time"

// ObservationSnapshot is a single hourly weather observation for a station.
// Nullable fields use pointers because Meteostat frequently omits variables
// for sparse stations or time periods with no sensor data.
//
// CloudCover stores the WMO weather condition code (coco, 1–27) returned by
// Meteostat. This is a discrete condition code, not a cloud-cover percentage.
type ObservationSnapshot struct {
	StationCode string
	ObservedAt  time.Time // wall-clock time in the station's local timezone
	Timezone    string    // IANA timezone name

	TempC      float64  // temperature in °C (always present)
	DewPointC  *float64 // dew point in °C
	WindKMH    *float64 // wind speed in km/h
	CloudCover *int     // WMO condition code (1 = clear sky … 27 = heavy storm)
	PrecipMM   *float64 // precipitation in mm
}
