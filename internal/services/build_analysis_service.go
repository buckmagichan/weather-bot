package services

import (
	"context"
	"fmt"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/hermes"
)

// BuildAnalysisService assembles a Hermes payload and calls the Bridge to
// obtain a parsed AnalysisResult from the highest-temp-analysis skill.
type BuildAnalysisService struct {
	hermesPayloadSvc *BuildHermesPayloadService
	bridge           *hermes.Bridge
}

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
	payload, err := s.hermesPayloadSvc.Build(summary, dist)
	if err != nil {
		return nil, fmt.Errorf("build analysis: assemble payload: %w", err)
	}
	result, err := s.bridge.Analyze(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("build analysis: call hermes: %w", err)
	}
	return result, nil
}
