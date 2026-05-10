package services

import (
	"strconv"
	"strings"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// PolymarketBucketProbabilityResolver precomputes lookup state for repeated
// Polymarket bucket probability lookups against one distribution.
type PolymarketBucketProbabilityResolver struct {
	probsByBucket map[string]float64
}

// NewPolymarketBucketProbabilityResolver creates a reusable probability
// resolver for a Polymarket event's market bucket labels.
func NewPolymarketBucketProbabilityResolver(dist *domain.TemperatureBucketDistribution) PolymarketBucketProbabilityResolver {
	resolver := PolymarketBucketProbabilityResolver{}
	if dist == nil {
		return resolver
	}
	resolver.probsByBucket = make(map[string]float64, len(dist.BucketProbs))
	for _, bucket := range dist.BucketProbs {
		resolver.probsByBucket[bucket.Label] = bucket.Prob
	}
	return resolver
}

// Probability returns the model probability for a Polymarket market bucket
// label. It handles dynamic boundary buckets such as "23C or below" by summing
// the fixed internal distribution buckets on that side.
func (r PolymarketBucketProbabilityResolver) Probability(label string) (float64, bool) {
	if len(r.probsByBucket) == 0 {
		return 0, false
	}
	return polymarketBucketProbability(label, r.probsByBucket)
}

func polymarketBucketProbability(label string, probsByBucket map[string]float64) (float64, bool) {
	if prob, ok := probsByBucket[label]; ok {
		return prob, true
	}

	normalized := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.HasSuffix(normalized, "c or below"):
		cutoff, ok := parseBucketTemp(strings.TrimSuffix(normalized, " or below"))
		if !ok {
			return 0, false
		}
		var total float64
		for bucketLabel, prob := range probsByBucket {
			temp, ok := modelBucketLowerComparableTemp(bucketLabel)
			if ok && temp <= cutoff {
				total += prob
			}
		}
		return total, true
	case strings.HasSuffix(normalized, "c or higher"):
		return sumUpperBoundaryProbability(normalized, " or higher", probsByBucket)
	case strings.HasSuffix(normalized, "c or above"):
		return sumUpperBoundaryProbability(normalized, " or above", probsByBucket)
	case strings.HasSuffix(normalized, "c and above"):
		return sumUpperBoundaryProbability(normalized, " and above", probsByBucket)
	default:
		return 0, false
	}
}

func sumUpperBoundaryProbability(label, suffix string, probsByBucket map[string]float64) (float64, bool) {
	cutoff, ok := parseBucketTemp(strings.TrimSuffix(label, suffix))
	if !ok {
		return 0, false
	}
	var total float64
	for bucketLabel, prob := range probsByBucket {
		temp, ok := modelBucketUpperComparableTemp(bucketLabel)
		if ok && temp >= cutoff {
			total += prob
		}
	}
	return total, true
}

func modelBucketLowerComparableTemp(label string) (float64, bool) {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if strings.HasSuffix(normalized, "c or below") {
		temp, ok := parseBucketTemp(strings.TrimSuffix(normalized, " or below"))
		return temp, ok
	}
	return parseBucketTemp(label)
}

func modelBucketUpperComparableTemp(label string) (float64, bool) {
	normalized := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.HasSuffix(normalized, "c or above"):
		temp, ok := parseBucketTemp(strings.TrimSuffix(normalized, " or above"))
		return temp, ok
	case strings.HasSuffix(normalized, "c or higher"):
		temp, ok := parseBucketTemp(strings.TrimSuffix(normalized, " or higher"))
		return temp, ok
	case strings.HasSuffix(normalized, "c and above"):
		temp, ok := parseBucketTemp(strings.TrimSuffix(normalized, " and above"))
		return temp, ok
	}
	return parseBucketTemp(label)
}

func parseBucketTemp(label string) (float64, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(label))
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "c"))
	n, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
