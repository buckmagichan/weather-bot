package domain

import (
	"encoding/json"
	"time"
)

// AnalysisPersistenceRecord bundles everything needed to store one Hermes
// analysis run to the analysis_results table.
//
// HermesPayloadJSON holds a pre-serialized hermes.HermesAnalysisPayload so
// that this type does not need to import the hermes package (which imports domain
// itself, which would create a cycle).
type AnalysisPersistenceRecord struct {
	StationCode       string
	TargetDateLocal   string
	GeneratedAt       time.Time
	Analysis          *AnalysisResult
	Summary           *WeatherFeatureSummary
	Distribution      *TemperatureBucketDistribution
	HermesPayloadJSON json.RawMessage
}
