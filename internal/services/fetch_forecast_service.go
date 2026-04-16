// Package services contains application-level use cases that orchestrate the
// provider and domain layers.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/providers/openmeteo"
)

const (
	// zspdLat and zspdLon target Shanghai Pudong International Airport (ICAO:
	// ZSPD), the settlement station for this market. These intentionally differ
	// from generic Shanghai city-centre coordinates (31.2304, 121.4737) so that
	// the forecast input is aligned with the station used in settlement.
	zspdLat = 31.1443
	zspdLon = 121.8083

	zspdTimezone = "Asia/Shanghai"
	zspdStation  = "ZSPD"

	// Open-Meteo returns hourly time strings as "2006-01-02T15:04" with no
	// seconds and no timezone suffix. We parse them in the station's local
	// timezone so the resulting time.Time values carry the correct offset.
	hourlyTimeLayout = "2006-01-02T15:04"
)

// FetchForecastService retrieves a daily forecast snapshot for ZSPD using the
// Open-Meteo API. The timezone location is loaded once at construction time.
type FetchForecastService struct {
	client *openmeteo.Client
	loc    *time.Location
}

// NewFetchForecastService creates a FetchForecastService backed by client.
// Returns an error if the IANA timezone database cannot be loaded.
func NewFetchForecastService(client *openmeteo.Client) (*FetchForecastService, error) {
	loc, err := time.LoadLocation(zspdTimezone)
	if err != nil {
		return nil, fmt.Errorf("fetch forecast service: load timezone %s: %w", zspdTimezone, err)
	}
	return &FetchForecastService{client: client, loc: loc}, nil
}

// FetchDailySnapshot retrieves today's forecast for ZSPD and returns a
// ForecastSnapshot covering the full day's hourly data.
func (s *FetchForecastService) FetchDailySnapshot(ctx context.Context) (*domain.ForecastSnapshot, error) {
	resp, err := s.client.Forecast(ctx, openmeteo.ForecastParams{
		Latitude:  zspdLat,
		Longitude: zspdLon,
		Hourly: []string{
			openmeteo.VarTemperature2m,
			openmeteo.VarDewPoint2m,
			openmeteo.VarPrecipitationProbability,
			openmeteo.VarCloudCover,
			openmeteo.VarWindSpeed10m,
		},
		Daily: []string{
			openmeteo.VarTemperature2mMax,
		},
		Timezone:     zspdTimezone,
		ForecastDays: 1,
		// Wind speed unit is left at the API default (km/h), which matches
		// the HourlyWindKMH field name in domain.ForecastSnapshot.
	})
	if err != nil {
		return nil, fmt.Errorf("fetch daily snapshot: %w", err)
	}

	if resp.Daily == nil || len(resp.Daily.Time) == 0 {
		return nil, fmt.Errorf("fetch daily snapshot: empty daily response")
	}
	if resp.Hourly == nil || len(resp.Hourly.Time) == 0 {
		return nil, fmt.Errorf("fetch daily snapshot: empty hourly response")
	}

	hourlyTimes, err := parseHourlyTimes(resp.Hourly.Time, s.loc)
	if err != nil {
		return nil, fmt.Errorf("fetch daily snapshot: parse hourly times: %w", err)
	}

	return &domain.ForecastSnapshot{
		StationCode:      zspdStation,
		TargetDateLocal:  resp.Daily.Time[0],
		FetchedAt:        time.Now().UTC(),
		Timezone:         resp.Timezone,
		ForecastHighC:    resp.Daily.Temperature2mMax[0],
		HourlyTime:       hourlyTimes,
		HourlyTempC:      resp.Hourly.Temperature2m,
		HourlyDewPointC:  resp.Hourly.DewPoint2m,
		HourlyCloudCover: resp.Hourly.CloudCover,
		HourlyPrecipProb: resp.Hourly.PrecipitationProbability,
		HourlyWindKMH:    resp.Hourly.WindSpeed10m,
	}, nil
}

// parseHourlyTimes converts a slice of "2006-01-02T15:04" strings into
// time.Time values anchored in loc.
func parseHourlyTimes(raw []string, loc *time.Location) ([]time.Time, error) {
	times := make([]time.Time, len(raw))
	for i, s := range raw {
		t, err := time.ParseInLocation(hourlyTimeLayout, s, loc)
		if err != nil {
			return nil, fmt.Errorf("index %d %q: %w", i, s, err)
		}
		times[i] = t
	}
	return times, nil
}
