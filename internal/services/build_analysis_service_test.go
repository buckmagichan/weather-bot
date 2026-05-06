package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/hermes"
	"github.com/buckmagichan/weather-bot/internal/providers/wunderground"
)

func TestBuildAnalysisService_FallsBackOnHermesRateLimit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hermes-rate-limited")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
cat <<'EOF'
RateLimitError [HTTP 429]
{'message': "You've reached your usage limit", 'type': 'rate_limit_reached_error'}
EOF
`), 0o755); err != nil {
		t.Fatalf("write fake hermes: %v", err)
	}

	svc := NewBuildAnalysisService(hermes.NewBridgeWithBin(script))
	obsHigh := 30.0
	change := 3.0
	summary := &domain.WeatherFeatureSummary{
		StationCode:         "ZBAA",
		TargetDateLocal:     "2026-05-05",
		Timezone:            "Asia/Shanghai",
		GeneratedAt:         time.Date(2026, 5, 5, 7, 30, 0, 0, time.UTC),
		LatestForecastHighC: 29.1,
		ObservedHighSoFarC:  &obsHigh,
		TempChangeLast3hC:   &change,
		ObservationPoints:   32,
		HourlyPoints:        24,
	}
	dist := &domain.TemperatureBucketDistribution{
		StationCode:     "ZBAA",
		TargetDateLocal: "2026-05-05",
		ExpectedHighC:   30.65,
		Confidence:      0.9,
		BucketProbs: []domain.BucketProbability{
			{Label: "30C", Prob: 0.2746},
			{Label: "31C", Prob: 0.5436},
			{Label: "32C", Prob: 0.1688},
		},
	}

	analysis, source, err := svc.BuildWithFallback(context.Background(), summary, dist)
	if err != nil {
		t.Fatalf("BuildWithFallback: %v", err)
	}
	if source != AnalysisSourceLocalFallback {
		t.Fatalf("source: got %q, want %q", source, AnalysisSourceLocalFallback)
	}
	if analysis.PredictedBestBucket != "31C" {
		t.Errorf("PredictedBestBucket: got %q, want 31C", analysis.PredictedBestBucket)
	}
	if analysis.SecondaryRiskBucket == nil || *analysis.SecondaryRiskBucket != "30C" {
		t.Errorf("SecondaryRiskBucket: got %v, want 30C", analysis.SecondaryRiskBucket)
	}
	if len(analysis.KeyReasons) < 2 || len(analysis.KeyReasons) > 4 {
		t.Errorf("KeyReasons length: got %d", len(analysis.KeyReasons))
	}
	if analysis.NextCheckInMinutes != 30 {
		t.Errorf("NextCheckInMinutes: got %d, want 30", analysis.NextCheckInMinutes)
	}
}

func TestBuildLocalAnalysis_EveningHistoricalLockUsesLongerNextCheck(t *testing.T) {
	resolutionHigh := 21.0
	latestObsAt := time.Date(2026, 5, 5, 21, 40, 0, 0, time.FixedZone("CST", 8*60*60))
	cooling := -3.0
	payload := hermes.HermesAnalysisPayload{
		StationCode:     "ZSQD",
		TargetDateLocal: "2026-05-05",
		GeneratedAt:     time.Date(2026, 5, 5, 12, 45, 0, 0, time.UTC),
		FeatureSummary: hermes.FeatureSummaryView{
			ResolutionObservedHighC: &resolutionHigh,
			ResolutionSourceType:    wunderground.SourceTypeHistoricalObservations,
			ObservedHighSoFarC:      &resolutionHigh,
			TempChangeLast3hC:       &cooling,
			LatestObservationAt:     &latestObsAt,
			ObservationPoints:       42,
			Timezone:                "Asia/Shanghai",
		},
		BucketDistribution: hermes.BucketDistributionView{
			Confidence: 0.9,
			BucketProbs: []hermes.BucketProbView{
				{Label: "21C", Prob: 0.977},
				{Label: "22C", Prob: 0.023},
			},
		},
	}

	analysis := buildLocalAnalysis(payload)

	if analysis.NextCheckInMinutes != 90 {
		t.Errorf("NextCheckInMinutes: got %d, want 90", analysis.NextCheckInMinutes)
	}
}
