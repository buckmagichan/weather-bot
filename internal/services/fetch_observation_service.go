package services

import (
	"context"
	"fmt"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/providers/meteostat"
)

const (
	// obsLat/obsLon/obsAlt target the same ZSPD airport grid cell used by
	// FetchForecastService so observation and forecast data are spatially aligned.
	obsLat = 31.1443
	obsLon = 121.8083
	obsAlt = 2 // metres — matches ZSPD airport elevation

	obsTimezone = "Asia/Shanghai"
	obsStation  = "ZSPD"

	// Meteostat returns times as "YYYY-MM-DD HH:MM:SS" (space-separated, no
	// timezone suffix). We parse them in the station's local timezone.
	meteostatTimeLayout = "2006-01-02 15:04:05"
)

// FetchObservationService retrieves hourly observation data for ZSPD from the
// Meteostat API. The timezone location is loaded once at construction time.
type FetchObservationService struct {
	client *meteostat.Client
	loc    *time.Location
}

// NewFetchObservationService creates a FetchObservationService backed by client.
func NewFetchObservationService(client *meteostat.Client) (*FetchObservationService, error) {
	loc, err := time.LoadLocation(obsTimezone)
	if err != nil {
		return nil, fmt.Errorf("fetch observation service: load timezone: %w", err)
	}
	return &FetchObservationService{client: client, loc: loc}, nil
}

// FetchTodayObservations fetches all available hourly observations for today's
// local date at ZSPD. Returns an empty slice (not an error) if Meteostat has
// no data yet for the current day (e.g. shortly after midnight or data lag).
func (s *FetchObservationService) FetchTodayObservations(ctx context.Context) ([]domain.ObservationSnapshot, error) {
	today := time.Now().In(s.loc).Format("2006-01-02")

	rows, err := s.client.PointHourly(ctx, meteostat.PointHourlyParams{
		Lat:   obsLat,
		Lon:   obsLon,
		Alt:   obsAlt,
		Start: today,
		End:   today,
		Tz:    obsTimezone,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch today observations: %w", err)
	}

	snaps := make([]domain.ObservationSnapshot, 0, len(rows))
	for _, row := range rows {
		// Skip rows with a missing timestamp or temperature — temp is the
		// primary signal and an observation without it is not useful.
		if row.Time == "" || row.Temp == nil {
			continue
		}
		t, err := time.ParseInLocation(meteostatTimeLayout, row.Time, s.loc)
		if err != nil {
			return nil, fmt.Errorf("fetch today observations: parse time %q: %w", row.Time, err)
		}
		snaps = append(snaps, domain.ObservationSnapshot{
			StationCode: obsStation,
			ObservedAt:  t,
			Timezone:    obsTimezone,
			TempC:       *row.Temp,
			DewPointC:   row.DewPt,
			WindKMH:     row.WSpd,
			CloudCover:  row.Coco,
			PrecipMM:    row.Prcp,
		})
	}
	return snaps, nil
}
