package openmeteo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Variable name constants for use in ForecastParams.Current, .Hourly, and .Daily.
const (
	VarTemperature2m               = "temperature_2m"
	VarApparentTemperature         = "apparent_temperature"
	VarRelativeHumidity2m          = "relative_humidity_2m"
	VarDewPoint2m                  = "dew_point_2m"
	VarPrecipitation               = "precipitation"
	VarPrecipitationProbability    = "precipitation_probability"
	VarCloudCover                  = "cloud_cover"
	VarWeatherCode                 = "weather_code"
	VarWindSpeed10m                = "wind_speed_10m"
	VarWindDirection10m            = "wind_direction_10m"
	VarTemperature2mMax            = "temperature_2m_max"
	VarTemperature2mMin            = "temperature_2m_min"
	VarApparentTemperatureMax      = "apparent_temperature_max"
	VarApparentTemperatureMin      = "apparent_temperature_min"
	VarPrecipitationSum            = "precipitation_sum"
	VarPrecipitationProbabilityMax = "precipitation_probability_max"
	VarWindSpeed10mMax             = "wind_speed_10m_max"
	VarWindDirection10mDominant    = "wind_direction_10m_dominant"
)

// Unit constants for ForecastParams.TemperatureUnit.
const (
	TempUnitCelsius    = "celsius"
	TempUnitFahrenheit = "fahrenheit"
)

// Unit constants for ForecastParams.WindSpeedUnit.
const (
	WindSpeedUnitKMH   = "kmh"
	WindSpeedUnitMS    = "ms"
	WindSpeedUnitMPH   = "mph"
	WindSpeedUnitKnots = "kn"
)

// Unit constants for ForecastParams.PrecipitationUnit.
const (
	PrecipUnitMM   = "mm"
	PrecipUnitInch = "inch"
)

// TimezoneAuto instructs the API to infer the timezone from coordinates.
const TimezoneAuto = "auto"

var (
	validTempUnits   = map[string]bool{TempUnitCelsius: true, TempUnitFahrenheit: true}
	validWindUnits   = map[string]bool{WindSpeedUnitKMH: true, WindSpeedUnitMS: true, WindSpeedUnitMPH: true, WindSpeedUnitKnots: true}
	validPrecipUnits = map[string]bool{PrecipUnitMM: true, PrecipUnitInch: true}
)

// ForecastParams defines the query parameters for the /forecast endpoint.
// Latitude and Longitude are required; all other fields are optional and use
// the API's defaults when zero/empty.
type ForecastParams struct {
	Latitude  float64 // required: [-90, 90]
	Longitude float64 // required: [-180, 180]

	// Variable lists — use the Var* constants above.
	Current []string
	Hourly  []string
	Daily   []string

	Timezone          string // IANA timezone name or TimezoneAuto
	ForecastDays      int    // 1–16; API default is 7 (0 means use default)
	PastDays          int    // 0–92
	TemperatureUnit   string // TempUnit* constants; API default is celsius
	WindSpeedUnit     string // WindSpeedUnit* constants; API default is kmh
	PrecipitationUnit string // PrecipUnit* constants; API default is mm
}

func (p ForecastParams) validate() error {
	if p.Latitude < -90 || p.Latitude > 90 {
		return fmt.Errorf("openmeteo: latitude %g out of range [-90, 90]", p.Latitude)
	}
	if p.Longitude < -180 || p.Longitude > 180 {
		return fmt.Errorf("openmeteo: longitude %g out of range [-180, 180]", p.Longitude)
	}
	if p.ForecastDays != 0 && (p.ForecastDays < 1 || p.ForecastDays > 16) {
		return fmt.Errorf("openmeteo: ForecastDays %d out of range [1, 16]", p.ForecastDays)
	}
	if p.PastDays < 0 || p.PastDays > 92 {
		return fmt.Errorf("openmeteo: PastDays %d out of range [0, 92]", p.PastDays)
	}
	if p.TemperatureUnit != "" && !validTempUnits[p.TemperatureUnit] {
		return fmt.Errorf("openmeteo: unknown TemperatureUnit %q (want %q or %q)", p.TemperatureUnit, TempUnitCelsius, TempUnitFahrenheit)
	}
	if p.WindSpeedUnit != "" && !validWindUnits[p.WindSpeedUnit] {
		return fmt.Errorf("openmeteo: unknown WindSpeedUnit %q (want one of: kmh, ms, mph, kn)", p.WindSpeedUnit)
	}
	if p.PrecipitationUnit != "" && !validPrecipUnits[p.PrecipitationUnit] {
		return fmt.Errorf("openmeteo: unknown PrecipitationUnit %q (want %q or %q)", p.PrecipitationUnit, PrecipUnitMM, PrecipUnitInch)
	}
	return nil
}

// CurrentUnits holds the unit strings returned alongside current weather data.
type CurrentUnits struct {
	Time                string `json:"time"`
	Interval            string `json:"interval"`
	Temperature2m       string `json:"temperature_2m"`
	ApparentTemperature string `json:"apparent_temperature"`
	RelativeHumidity2m  string `json:"relative_humidity_2m"`
	Precipitation       string `json:"precipitation"`
	WeatherCode         string `json:"weather_code"`
	WindSpeed10m        string `json:"wind_speed_10m"`
	WindDirection10m    string `json:"wind_direction_10m"`
}

// CurrentWeather holds the current conditions snapshot.
type CurrentWeather struct {
	Time                string  `json:"time"`
	Interval            int     `json:"interval"`
	Temperature2m       float64 `json:"temperature_2m"`
	ApparentTemperature float64 `json:"apparent_temperature"`
	RelativeHumidity2m  int     `json:"relative_humidity_2m"`
	Precipitation       float64 `json:"precipitation"`
	WeatherCode         int     `json:"weather_code"`
	WindSpeed10m        float64 `json:"wind_speed_10m"`
	WindDirection10m    float64 `json:"wind_direction_10m"`
}

// HourlyUnits holds the unit strings for each hourly variable.
type HourlyUnits struct {
	Time                     string `json:"time"`
	Temperature2m            string `json:"temperature_2m"`
	ApparentTemperature      string `json:"apparent_temperature"`
	RelativeHumidity2m       string `json:"relative_humidity_2m"`
	DewPoint2m               string `json:"dew_point_2m"`
	PrecipitationProbability string `json:"precipitation_probability"`
	Precipitation            string `json:"precipitation"`
	CloudCover               string `json:"cloud_cover"`
	WeatherCode              string `json:"weather_code"`
	WindSpeed10m             string `json:"wind_speed_10m"`
	WindDirection10m         string `json:"wind_direction_10m"`
}

// HourlyData holds parallel slices of hourly forecasts indexed by Time.
// Only variables included in ForecastParams.Hourly are populated; unrequested
// fields decode as nil and are silently ignored by encoding/json.
type HourlyData struct {
	Time                     []string  `json:"time"`
	Temperature2m            []float64 `json:"temperature_2m"`
	ApparentTemperature      []float64 `json:"apparent_temperature"`
	RelativeHumidity2m       []int     `json:"relative_humidity_2m"`
	DewPoint2m               []float64 `json:"dew_point_2m"`
	PrecipitationProbability []int     `json:"precipitation_probability"`
	Precipitation            []float64 `json:"precipitation"`
	CloudCover               []int     `json:"cloud_cover"`
	WeatherCode              []int     `json:"weather_code"`
	WindSpeed10m             []float64 `json:"wind_speed_10m"`
	WindDirection10m         []float64 `json:"wind_direction_10m"`
}

// DailyUnits holds the unit strings for each daily variable.
type DailyUnits struct {
	Time                        string `json:"time"`
	WeatherCode                 string `json:"weather_code"`
	Temperature2mMax            string `json:"temperature_2m_max"`
	Temperature2mMin            string `json:"temperature_2m_min"`
	ApparentTemperatureMax      string `json:"apparent_temperature_max"`
	ApparentTemperatureMin      string `json:"apparent_temperature_min"`
	PrecipitationSum            string `json:"precipitation_sum"`
	PrecipitationProbabilityMax string `json:"precipitation_probability_max"`
	WindSpeed10mMax             string `json:"wind_speed_10m_max"`
	WindDirection10mDominant    string `json:"wind_direction_10m_dominant"`
}

// DailyData holds parallel slices of daily forecasts indexed by Time.
// Only variables included in ForecastParams.Daily are populated; unrequested
// fields decode as nil and are silently ignored by encoding/json.
type DailyData struct {
	Time                        []string  `json:"time"`
	WeatherCode                 []int     `json:"weather_code"`
	Temperature2mMax            []float64 `json:"temperature_2m_max"`
	Temperature2mMin            []float64 `json:"temperature_2m_min"`
	ApparentTemperatureMax      []float64 `json:"apparent_temperature_max"`
	ApparentTemperatureMin      []float64 `json:"apparent_temperature_min"`
	PrecipitationSum            []float64 `json:"precipitation_sum"`
	PrecipitationProbabilityMax []int     `json:"precipitation_probability_max"`
	WindSpeed10mMax             []float64 `json:"wind_speed_10m_max"`
	WindDirection10mDominant    []float64 `json:"wind_direction_10m_dominant"`
}

// ForecastResponse is the top-level response from the /forecast endpoint.
// Pointer fields are nil when the corresponding variable group was not
// requested. Variable fields not modelled in the structs above are silently
// ignored during JSON decoding; this is intentional for v1 of this package.
type ForecastResponse struct {
	Latitude             float64         `json:"latitude"`
	Longitude            float64         `json:"longitude"`
	GenerationtimeMs     float64         `json:"generationtime_ms"`
	UTCOffsetSeconds     int             `json:"utc_offset_seconds"`
	Timezone             string          `json:"timezone"`
	TimezoneAbbreviation string          `json:"timezone_abbreviation"`
	Elevation            float64         `json:"elevation"`
	CurrentUnits         *CurrentUnits   `json:"current_units"`
	Current              *CurrentWeather `json:"current"`
	HourlyUnits          *HourlyUnits    `json:"hourly_units"`
	Hourly               *HourlyData     `json:"hourly"`
	DailyUnits           *DailyUnits     `json:"daily_units"`
	Daily                *DailyData      `json:"daily"`
}

// checkSliceLengths verifies that every non-empty slice in a data block
// matches the expected length (typically len(Time)). A length of zero means
// the variable was not requested, so it is skipped.
func checkSliceLengths(block string, expected int, fields map[string]int) error {
	for name, got := range fields {
		if got > 0 && got != expected {
			return fmt.Errorf("openmeteo: %s.%s: got %d elements, expected %d", block, name, got, expected)
		}
	}
	return nil
}

// Forecast calls the Open-Meteo /forecast endpoint and returns the parsed
// response. p.Latitude and p.Longitude are required; invalid params are
// rejected before any network call is made.
func (c *Client) Forecast(ctx context.Context, p ForecastParams) (*ForecastResponse, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(p.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(p.Longitude, 'f', -1, 64))

	if len(p.Current) > 0 {
		params.Set("current", strings.Join(p.Current, ","))
	}
	if len(p.Hourly) > 0 {
		params.Set("hourly", strings.Join(p.Hourly, ","))
	}
	if len(p.Daily) > 0 {
		params.Set("daily", strings.Join(p.Daily, ","))
	}
	if p.Timezone != "" {
		params.Set("timezone", p.Timezone)
	}
	if p.ForecastDays > 0 {
		params.Set("forecast_days", strconv.Itoa(p.ForecastDays))
	}
	if p.PastDays > 0 {
		params.Set("past_days", strconv.Itoa(p.PastDays))
	}
	if p.TemperatureUnit != "" {
		params.Set("temperature_unit", p.TemperatureUnit)
	}
	if p.WindSpeedUnit != "" {
		params.Set("wind_speed_unit", p.WindSpeedUnit)
	}
	if p.PrecipitationUnit != "" {
		params.Set("precipitation_unit", p.PrecipitationUnit)
	}

	data, err := c.get(ctx, "/forecast", params)
	if err != nil {
		return nil, err
	}

	var result ForecastResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("openmeteo: decode response: %w", err)
	}

	if h := result.Hourly; h != nil {
		if err := checkSliceLengths("hourly", len(h.Time), map[string]int{
			VarTemperature2m:            len(h.Temperature2m),
			VarApparentTemperature:      len(h.ApparentTemperature),
			VarRelativeHumidity2m:       len(h.RelativeHumidity2m),
			VarDewPoint2m:               len(h.DewPoint2m),
			VarPrecipitationProbability: len(h.PrecipitationProbability),
			VarPrecipitation:            len(h.Precipitation),
			VarCloudCover:               len(h.CloudCover),
			VarWeatherCode:              len(h.WeatherCode),
			VarWindSpeed10m:             len(h.WindSpeed10m),
			VarWindDirection10m:         len(h.WindDirection10m),
		}); err != nil {
			return nil, err
		}
	}

	if d := result.Daily; d != nil {
		if err := checkSliceLengths("daily", len(d.Time), map[string]int{
			VarWeatherCode:                 len(d.WeatherCode),
			VarTemperature2mMax:            len(d.Temperature2mMax),
			VarTemperature2mMin:            len(d.Temperature2mMin),
			VarApparentTemperatureMax:      len(d.ApparentTemperatureMax),
			VarApparentTemperatureMin:      len(d.ApparentTemperatureMin),
			VarPrecipitationSum:            len(d.PrecipitationSum),
			VarPrecipitationProbabilityMax: len(d.PrecipitationProbabilityMax),
			VarWindSpeed10mMax:             len(d.WindSpeed10mMax),
			VarWindDirection10mDominant:    len(d.WindDirection10mDominant),
		}); err != nil {
			return nil, err
		}
	}

	return &result, nil
}
