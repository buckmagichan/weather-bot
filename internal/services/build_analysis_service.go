package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/hermes"
	"github.com/buckmagichan/weather-bot/internal/providers/wunderground"
)

// BuildAnalysisService assembles a Hermes payload and calls the Bridge to
// obtain a parsed AnalysisResult from the highest-temp-analysis skill.
type BuildAnalysisService struct {
	hermesPayloadSvc *BuildHermesPayloadService
	bridge           *hermes.Bridge
}

type AnalysisSource string

const (
	AnalysisSourceHermes        AnalysisSource = "hermes"
	AnalysisSourceLocalFallback AnalysisSource = "local_fallback"
	lateEveningLockHour                        = 21.5
)

func NewBuildAnalysisService(bridge *hermes.Bridge) *BuildAnalysisService {
	return &BuildAnalysisService{
		hermesPayloadSvc: NewBuildHermesPayloadService(),
		bridge:           bridge,
	}
}

func (s *BuildAnalysisService) Build(
	ctx context.Context,
	summary *domain.WeatherFeatureSummary,
	dist *domain.TemperatureBucketDistribution,
) (*domain.AnalysisResult, error) {
	result, _, err := s.BuildWithFallback(ctx, summary, dist)
	return result, err
}

func (s *BuildAnalysisService) BuildWithFallback(
	ctx context.Context,
	summary *domain.WeatherFeatureSummary,
	dist *domain.TemperatureBucketDistribution,
) (*domain.AnalysisResult, AnalysisSource, error) {
	payload, err := s.hermesPayloadSvc.Build(summary, dist)
	if err != nil {
		return nil, "", fmt.Errorf("build analysis: assemble payload: %w", err)
	}
	result, err := s.bridge.Analyze(ctx, payload)
	if err != nil {
		if hermes.IsRateLimited(err) || hermes.IsUnavailable(err) || ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return buildLocalAnalysis(payload), AnalysisSourceLocalFallback, nil
		}
		return nil, "", fmt.Errorf("build analysis: call hermes: %w", err)
	}
	return result, AnalysisSourceHermes, nil
}

type rankedBucket struct {
	label string
	prob  float64
}

func buildLocalAnalysis(payload hermes.HermesAnalysisPayload) *domain.AnalysisResult {
	ranked := make([]rankedBucket, 0, len(payload.BucketDistribution.BucketProbs))
	for _, bucket := range payload.BucketDistribution.BucketProbs {
		ranked = append(ranked, rankedBucket{label: bucket.Label, prob: bucket.Prob})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].prob > ranked[j].prob
	})

	top := rankedBucket{label: "unknown", prob: 0}
	if len(ranked) > 0 {
		top = ranked[0]
	}
	var secondary *string
	if len(ranked) > 1 && ranked[1].prob >= math.Max(0.05, top.prob*0.12) {
		v := ranked[1].label
		secondary = &v
	}

	return &domain.AnalysisResult{
		PredictedBestBucket: top.label,
		SecondaryRiskBucket: secondary,
		Confidence:          payload.BucketDistribution.Confidence,
		KeyReasons:          localKeyReasons(payload, top, secondary),
		RiskFlags:           append([]string(nil), payload.SanityFlags...),
		NextCheckInMinutes:  localNextCheckInMinutes(payload, top),
	}
}

func localKeyReasons(
	payload hermes.HermesAnalysisPayload,
	top rankedBucket,
	secondary *string,
) []string {
	fs := payload.FeatureSummary
	reasons := []string{
		fmt.Sprintf("%s has the highest bucket probability at %.1f%%", top.label, top.prob*100),
	}
	if fs.ObservedHighSoFarC != nil {
		reasons = append(reasons, fmt.Sprintf("Observed high so far is %.0fC, which rules out lower buckets", *fs.ObservedHighSoFarC))
	}
	if fs.TempChangeLast3hC != nil {
		reasons = append(reasons, fmt.Sprintf("Recent 3h temperature change is %.1fC", *fs.TempChangeLast3hC))
	}
	if secondary != nil {
		reasons = append(reasons, fmt.Sprintf("%s remains the main secondary bucket by probability", *secondary))
	}
	if len(reasons) < 2 {
		reasons = append(reasons, fmt.Sprintf("Expected high is %.1fC from the calibrated distribution", payload.BucketDistribution.ExpectedHighC))
	}
	if len(reasons) > 4 {
		return reasons[:4]
	}
	return reasons
}

func localNextCheckInMinutes(payload hermes.HermesAnalysisPayload, top rankedBucket) int {
	fs := payload.FeatureSummary
	if localAnalysisLockedForEvening(payload, top) {
		return 90
	}
	if fs.TempChangeLast3hC != nil && *fs.TempChangeLast3hC >= 2.0 {
		return 30
	}
	if top.prob < 0.65 {
		return 30
	}
	if fs.ObservationPoints >= 12 && top.prob >= 0.85 {
		return 60
	}
	return 60
}

func localAnalysisLockedForEvening(payload hermes.HermesAnalysisPayload, top rankedBucket) bool {
	fs := payload.FeatureSummary
	if fs.ResolutionSourceType != wunderground.SourceTypeHistoricalObservations {
		return false
	}
	if fs.ResolutionObservedHighC == nil || top.prob < 0.85 {
		return false
	}
	if fs.ObservationPoints < 12 {
		return false
	}
	if fs.TempChangeLast3hC != nil && *fs.TempChangeLast3hC > 0 {
		return false
	}
	return featureSummaryLocalHour(payload) >= lateEveningLockHour
}

func featureSummaryLocalHour(payload hermes.HermesAnalysisPayload) float64 {
	loc := time.UTC
	if payload.FeatureSummary.Timezone != "" {
		if loaded, err := time.LoadLocation(payload.FeatureSummary.Timezone); err == nil {
			loc = loaded
		}
	}
	t := payload.GeneratedAt
	if payload.FeatureSummary.LatestObservationAt != nil {
		t = *payload.FeatureSummary.LatestObservationAt
	}
	local := t.In(loc)
	return float64(local.Hour()) + float64(local.Minute())/60.0
}
