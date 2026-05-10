package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/providers/aviationweather"
)

type fakeMETARObservationClient struct {
	reports    []aviationweather.METARReport
	err        error
	called     bool
	lastParams aviationweather.METARParams
}

func (f *fakeMETARObservationClient) METAR(ctx context.Context, p aviationweather.METARParams) ([]aviationweather.METARReport, error) {
	f.called = true
	f.lastParams = p
	return f.reports, f.err
}

func TestFetchTodayObservations_FiltersToTodayAndSortsAscending(t *testing.T) {
	client := &fakeMETARObservationClient{
		reports: []aviationweather.METARReport{
			{ICAOID: "ZSPD", ReportTime: "2026-04-18T12:30:00.000Z", Temp: floatPtr(14)},
			{ICAOID: "ZSPD", ReportTime: "2026-04-17T15:30:00.000Z", Temp: floatPtr(13)},
			{ICAOID: "ZSPD", ReportTime: "2026-04-18T12:00:00.000Z", Temp: floatPtr(15)},
		},
	}
	now := func() time.Time {
		return time.Date(2026, 4, 18, 21, 0, 0, 0, time.FixedZone("CST", 8*3600))
	}
	svc, err := NewBuildableFetchObservationService(client, now)
	if err != nil {
		t.Fatalf("init service: %v", err)
	}

	rows, err := svc.FetchTodayObservations(context.Background())
	if err != nil {
		t.Fatalf("FetchTodayObservations: %v", err)
	}
	if !client.called {
		t.Fatal("METAR was not called")
	}
	if got := client.lastParams.IDs; len(got) != 1 || got[0] != "ZSPD" {
		t.Fatalf("METAR IDs: got %v, want [ZSPD]", got)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ObservedAt.After(rows[1].ObservedAt) {
		t.Fatal("rows are not sorted ascending by ObservedAt")
	}
	if rows[0].StationCode != "ZSPD" || rows[1].StationCode != "ZSPD" {
		t.Fatalf("StationCode: got %q/%q, want ZSPD", rows[0].StationCode, rows[1].StationCode)
	}
}

func TestFetchTodayObservations_ConvertsWindKnotsToKMH(t *testing.T) {
	client := &fakeMETARObservationClient{
		reports: []aviationweather.METARReport{
			{
				ICAOID:     "ZSPD",
				ReportTime: "2026-04-18T12:30:00.000Z",
				Temp:       floatPtr(14),
				Wspd:       floatPtr(8),
				Dewp:       floatPtr(11),
			},
		},
	}
	now := func() time.Time {
		return time.Date(2026, 4, 18, 21, 0, 0, 0, time.FixedZone("CST", 8*3600))
	}
	svc, err := NewBuildableFetchObservationService(client, now)
	if err != nil {
		t.Fatalf("init service: %v", err)
	}

	rows, err := svc.FetchTodayObservations(context.Background())
	if err != nil {
		t.Fatalf("FetchTodayObservations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].WindKMH == nil || *rows[0].WindKMH != 8*knotsToKMH {
		t.Fatalf("WindKMH: got %v, want %.3f", rows[0].WindKMH, 8*knotsToKMH)
	}
	if rows[0].DewPointC == nil || *rows[0].DewPointC != 11 {
		t.Fatalf("DewPointC: got %v, want 11", rows[0].DewPointC)
	}
}

func TestFetchTodayObservations_UsesObsTimeFallback(t *testing.T) {
	client := &fakeMETARObservationClient{
		reports: []aviationweather.METARReport{
			{
				ICAOID:  "ZSPD",
				ObsTime: 1776519000,
				Temp:    floatPtr(14),
			},
		},
	}
	now := func() time.Time {
		return time.Date(2026, 4, 18, 21, 0, 0, 0, time.FixedZone("CST", 8*3600))
	}
	svc, err := NewBuildableFetchObservationService(client, now)
	if err != nil {
		t.Fatalf("init service: %v", err)
	}

	rows, err := svc.FetchTodayObservations(context.Background())
	if err != nil {
		t.Fatalf("FetchTodayObservations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].ObservedAt.Format(time.RFC3339) != "2026-04-18T21:30:00+08:00" {
		t.Fatalf("ObservedAt: got %s, want 2026-04-18T21:30:00+08:00", rows[0].ObservedAt.Format(time.RFC3339))
	}
}

func TestFetchTodayObservations_ReturnsMETARError(t *testing.T) {
	client := &fakeMETARObservationClient{
		err: errors.New("boom"),
	}
	svc, err := NewBuildableFetchObservationService(client, time.Now)
	if err != nil {
		t.Fatalf("init service: %v", err)
	}

	_, err = svc.FetchTodayObservations(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !client.called {
		t.Fatal("METAR should be called")
	}
}

func TestFetchTodayObservations_UsesConfiguredStation(t *testing.T) {
	client := &fakeMETARObservationClient{
		reports: []aviationweather.METARReport{
			{ICAOID: "ZSPD", ReportTime: "2026-04-18T12:00:00.000Z", Temp: floatPtr(15)},
			{ICAOID: "ZBAA", ReportTime: "2026-04-18T12:30:00.000Z", Temp: floatPtr(24)},
		},
	}
	now := func() time.Time {
		return time.Date(2026, 4, 18, 21, 0, 0, 0, time.FixedZone("CST", 8*3600))
	}
	svc, err := NewBuildableFetchObservationServiceForStation(client, WeatherStation{
		Code:     "ZBAA",
		Timezone: chinaTimezone,
	}, now)
	if err != nil {
		t.Fatalf("init service: %v", err)
	}

	rows, err := svc.FetchTodayObservations(context.Background())
	if err != nil {
		t.Fatalf("FetchTodayObservations: %v", err)
	}
	if got := client.lastParams.IDs; len(got) != 1 || got[0] != "ZBAA" {
		t.Fatalf("METAR IDs: got %v, want [ZBAA]", got)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].StationCode != "ZBAA" {
		t.Fatalf("StationCode: got %q, want ZBAA", rows[0].StationCode)
	}
	if rows[0].TempC != 24 {
		t.Fatalf("TempC: got %.1f, want 24", rows[0].TempC)
	}
}

// NewBuildableFetchObservationService mirrors the production constructor but
// lets tests inject a fake client.
func NewBuildableFetchObservationService(client metarObservationClient, now func() time.Time) (*FetchObservationService, error) {
	return NewBuildableFetchObservationServiceForStation(client, DefaultWeatherStation(), now)
}

func NewBuildableFetchObservationServiceForStation(
	client metarObservationClient,
	station WeatherStation,
	now func() time.Time,
) (*FetchObservationService, error) {
	loc, err := station.Location()
	if err != nil {
		return nil, err
	}
	return &FetchObservationService{client: client, station: station, loc: loc, now: now}, nil
}
