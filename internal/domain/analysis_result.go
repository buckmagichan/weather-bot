package domain

// AnalysisResult holds the parsed response from the highest-temp-analysis Hermes skill.
// Field names and types mirror the skill output schema exactly.
type AnalysisResult struct {
	PredictedBestBucket string   `json:"predicted_best_bucket"`
	SecondaryRiskBucket *string  `json:"secondary_risk_bucket"`
	Confidence          float64  `json:"confidence"`
	KeyReasons          []string `json:"key_reasons"`
	RiskFlags           []string `json:"risk_flags"`
	NextCheckInMinutes  int      `json:"next_check_in_minutes"`
}
