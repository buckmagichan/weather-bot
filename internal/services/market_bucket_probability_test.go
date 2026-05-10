package services

import (
	"math"
	"testing"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

func TestPolymarketBucketProbabilityMapsExactAndDynamicBoundaryBuckets(t *testing.T) {
	dist := &domain.TemperatureBucketDistribution{
		BucketProbs: []domain.BucketProbability{
			{Label: "-20C or below", Prob: 0.01},
			{Label: "22C", Prob: 0.02},
			{Label: "23C", Prob: 0.03},
			{Label: "24C", Prob: 0.50},
			{Label: "24.5C", Prob: 0.07},
			{Label: "27C", Prob: 0.04},
			{Label: "28C", Prob: 0.05},
			{Label: "50C or above", Prob: 0.06},
		},
	}

	tests := []struct {
		label string
		want  float64
	}{
		{label: "24C", want: 0.50},
		{label: "24.5C", want: 0.07},
		{label: "24.5C or below", want: 0.63},
		{label: "23C or below", want: 0.06},
		{label: "27C or higher", want: 0.15},
		{label: "27c OR ABOVE", want: 0.15},
		{label: "27C and above", want: 0.15},
	}

	resolver := NewPolymarketBucketProbabilityResolver(dist)
	for _, tt := range tests {
		got, ok := resolver.Probability(tt.label)
		if !ok {
			t.Fatalf("Probability(%q) returned !ok", tt.label)
		}
		if math.Abs(got-tt.want) > 1e-9 {
			t.Fatalf("Probability(%q) = %v, want %v", tt.label, got, tt.want)
		}
	}
}
