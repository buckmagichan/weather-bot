package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/providers/aviationweather"
)

const (
	obsTimezone      = "Asia/Shanghai"
	obsStation       = "ZSPD"
	obsLookbackHours = 36
	knotsToKMH       = 1.852
)

// FetchObservationService retrieves METAR observation data for ZSPD from the
// Aviation Weather Center Data API. The timezone location is loaded once at
// construction time.
type FetchObservationService struct {
	client metarObservationClient
	loc    *time.Location
	now    func() time.Time
}

type metarObservationClient interface {
	METAR(ctx context.Context, p aviationweather.METARParams) ([]aviationweather.METARReport, error)
}

// NewFetchObservationService creates a FetchObservationService backed by client.
func NewFetchObservationService(client metarObservationClient) (*FetchObservationService, error) {
	loc, err := time.LoadLocation(obsTimezone)
	if err != nil {
		return nil, fmt.Errorf("fetch observation service: load timezone: %w", err)
	}
	return &FetchObservationService{client: client, loc: loc, now: time.Now}, nil
}

// FetchTodayObservations fetches all available METAR observations whose local
// report date is today in Asia/Shanghai. Returns an empty slice (not an error)
// if the upstream API has no data yet for the current day.
func (s *FetchObservationService) FetchTodayObservations(ctx context.Context) ([]domain.ObservationSnapshot, error) {
	today := s.now().In(s.loc).Format("2006-01-02")

	reports, err := s.client.METAR(ctx, aviationweather.METARParams{
		IDs:   []string{obsStation},
		Hours: obsLookbackHours,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch today observations: metar: %w", err)
	}
	return buildObservationSnapshots(reports, today, s.loc)
}

func buildObservationSnapshots(
	reports []aviationweather.METARReport,
	targetDate string,
	loc *time.Location,
) ([]domain.ObservationSnapshot, error) {
	snaps := make([]domain.ObservationSnapshot, 0, len(reports))
	for _, report := range reports {
		// Skip rows with a missing timestamp or temperature — temp is the
		// primary signal and an observation without it is not useful.
		if report.Temp == nil {
			continue
		}
		t, err := parseObservedAt(report)
		if err != nil {
			return nil, fmt.Errorf("fetch today observations: parse report time: %w", err)
		}
		observedAt := t.In(loc)
		if observedAt.Format("2006-01-02") != targetDate {
			continue
		}

		var windKMH *float64
		if report.Wspd != nil {
			v := *report.Wspd * knotsToKMH
			windKMH = &v
		}
		snaps = append(snaps, domain.ObservationSnapshot{
			StationCode: obsStation,
			ObservedAt:  observedAt,
			Timezone:    obsTimezone,
			TempC:       *report.Temp,
			DewPointC:   report.Dewp,
			WindKMH:     windKMH,
		})
	}

	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].ObservedAt.Before(snaps[j].ObservedAt)
	})
	return snaps, nil
}

func parseObservedAt(report aviationweather.METARReport) (time.Time, error) {
	if report.ReportTime != "" {
		t, err := time.Parse(time.RFC3339, report.ReportTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("report_time %q: %w", report.ReportTime, err)
		}
		return t, nil
	}
	if report.ObsTime != 0 {
		return time.Unix(report.ObsTime, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("missing report_time and obs_time")
}
