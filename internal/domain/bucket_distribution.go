package domain

import "time"

// BucketProbability is one labelled bucket in a temperature distribution.
type BucketProbability struct {
	Label string  `json:"label"` // human-readable label, e.g. "17C or below", "18C", "20C or above"
	Prob  float64 `json:"prob"`  // probability ∈ [0, 1]
}

// TemperatureBucketDistribution is the output of the bucket distribution
// service: a probability distribution over temperature buckets, an adjusted
// point estimate, and a confidence score.
type TemperatureBucketDistribution struct {
	StationCode     string    `json:"station_code"`
	TargetDateLocal string    `json:"target_date_local"` // YYYY-MM-DD in local station time
	GeneratedAt     time.Time `json:"generated_at"`      // UTC

	ExpectedHighC float64             `json:"expected_high_c"`
	BucketProbs   []BucketProbability `json:"bucket_probs"` // probabilities sum to ≈ 1.0
	Confidence    float64             `json:"confidence"`   // [0, 1]: how much data backed this estimate
}
