package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/providers/wunderground"
)

// ResolutionObservation is the settlement-source daily high for a station.
type ResolutionObservation struct {
	StationCode     string
	TargetDateLocal string
	HighC           int
	SourceURL       string
	SourceType      string
	FetchedAt       time.Time
}

// FetchResolutionObservationService retrieves Wunderground-derived daily highs
// from each station's configured resolution source URL.
type FetchResolutionObservationService struct {
	client *wunderground.Client
}

func NewFetchResolutionObservationService(client *wunderground.Client) *FetchResolutionObservationService {
	return &FetchResolutionObservationService{client: client}
}

func (s *FetchResolutionObservationService) FetchDailyHigh(
	ctx context.Context,
	station WeatherStation,
	targetDateLocal string,
) (ResolutionObservation, bool, error) {
	if s == nil || s.client == nil {
		return ResolutionObservation{}, false, fmt.Errorf("fetch resolution observation: nil client")
	}
	sourceURL := resolutionSourceURL(station.ResolutionSourceURL, targetDateLocal)
	if sourceURL == "" {
		return ResolutionObservation{}, false, nil
	}
	result, ok, err := s.client.DailyHighCForStationWithSource(ctx, sourceURL, station.Code)
	if err != nil {
		return ResolutionObservation{}, false, err
	}
	if !ok {
		return ResolutionObservation{}, false, nil
	}
	return ResolutionObservation{
		StationCode:     station.Code,
		TargetDateLocal: targetDateLocal,
		HighC:           result.HighC,
		SourceURL:       sourceURL,
		SourceType:      result.SourceType,
		FetchedAt:       time.Now().UTC(),
	}, true, nil
}

// ApplyResolutionObservedHigh folds a settlement-source high into the summary.
// The resolution high is recorded separately and becomes ObservedHighSoFarC
// because it is the preferred settlement-aligned source. METAR remains the
// fallback when Wunderground is unavailable or incomplete.
func ApplyResolutionObservedHigh(summary *domain.WeatherFeatureSummary, obs ResolutionObservation) {
	if summary == nil {
		panic("ApplyResolutionObservedHigh: summary must not be nil")
	}
	high := float64(obs.HighC)
	summary.ResolutionObservedHighC = &high
	summary.ResolutionSourceURL = obs.SourceURL
	summary.ResolutionSourceType = obs.SourceType
	summary.ObservedHighSoFarC = &high
}

func resolutionSourceURL(baseURL, targetDateLocal string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" || targetDateLocal == "" || strings.Contains(baseURL, "/date/") {
		return baseURL
	}
	return baseURL + "/date/" + targetDateLocal
}
