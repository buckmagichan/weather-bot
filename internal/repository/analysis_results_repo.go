package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// AnalysisResultsRepo persists Hermes analysis results to PostgreSQL.
type AnalysisResultsRepo struct {
	pool *pgxpool.Pool
}

// NewAnalysisResultsRepo creates an AnalysisResultsRepo backed by pool.
func NewAnalysisResultsRepo(pool *pgxpool.Pool) *AnalysisResultsRepo {
	return &AnalysisResultsRepo{pool: pool}
}

const insertAnalysisResultSQL = `
INSERT INTO analysis_results (
    station_code,
    target_date_local,
    generated_at,
    predicted_best_bucket,
    secondary_risk_bucket,
    confidence,
    key_reasons_json,
    risk_flags_json,
    next_check_in_minutes,
    feature_summary_json,
    bucket_distribution_json,
    hermes_payload_json,
    analysis_content_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (station_code, target_date_local, analysis_content_hash) DO NOTHING`

// Insert persists an AnalysisPersistenceRecord to the analysis_results table.
//
// Deduplication is keyed on analysis_content_hash — a SHA-256 of all
// semantically meaningful fields. Two runs that produce the same analysis are
// considered identical regardless of when they ran, so the second insert is
// silently dropped and inserted returns false.
func (r *AnalysisResultsRepo) Insert(
	ctx context.Context,
	rec *domain.AnalysisPersistenceRecord,
) (inserted bool, err error) {
	targetDate, err := time.Parse("2006-01-02", rec.TargetDateLocal)
	if err != nil {
		return false, fmt.Errorf("insert analysis result: parse target date %q: %w", rec.TargetDateLocal, err)
	}

	// Serialize JSONB columns for DB storage.
	keyReasonsJSON, err := toJSONB(rec.Analysis.KeyReasons)
	if err != nil {
		return false, fmt.Errorf("insert analysis result: marshal key_reasons: %w", err)
	}
	riskFlagsJSON, err := toJSONB(rec.Analysis.RiskFlags)
	if err != nil {
		return false, fmt.Errorf("insert analysis result: marshal risk_flags: %w", err)
	}
	summaryJSON, err := toJSONB(rec.Summary)
	if err != nil {
		return false, fmt.Errorf("insert analysis result: marshal feature_summary: %w", err)
	}
	distJSON, err := toJSONB(rec.Distribution)
	if err != nil {
		return false, fmt.Errorf("insert analysis result: marshal bucket_distribution: %w", err)
	}

	hash, err := computeAnalysisContentHash(rec, riskFlagsJSON)
	if err != nil {
		return false, fmt.Errorf("insert analysis result: compute content hash: %w", err)
	}

	tag, err := r.pool.Exec(ctx, insertAnalysisResultSQL,
		rec.StationCode,
		targetDate,
		rec.GeneratedAt,
		rec.Analysis.PredictedBestBucket,
		rec.Analysis.SecondaryRiskBucket, // nil → SQL NULL
		rec.Analysis.Confidence,
		keyReasonsJSON,
		riskFlagsJSON,
		rec.Analysis.NextCheckInMinutes,
		summaryJSON,
		distJSON,
		rec.HermesPayloadJSON,
		hash,
	)
	if err != nil {
		return false, fmt.Errorf("insert analysis result: exec: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// analysisContentHashInput is the canonical serialisable form of the fields
// that determine the logical identity of an analysis result.
//
// Deliberately excluded from the hash:
//   - generated_at / created_at / id — volatile timestamps and DB key
//   - feature_summary_json / bucket_distribution_json — contain volatile
//     generated_at timestamps; weather-relevant content is already captured
//     (in clean form) inside hermes_payload_json
//   - key_reasons_json — LLM free text; wording varies between runs for the
//     same decision; stored for display, not for identity
//   - confidence — LLM float; skill says "copy or adjust from payload"; the
//     LLM drifts (e.g. 0.75 vs 0.80) for identical inputs; the deterministic
//     confidence is already captured inside hermes_payload_json →
//     bucket_distribution.confidence
//
// hermes_payload_json is included with its own generated_at stripped via
// stripGeneratedAt so the hash is stable across repeated runs.
type analysisContentHashInput struct {
	StationCode         string          `json:"station_code"`
	TargetDateLocal     string          `json:"target_date_local"`
	PredictedBestBucket string          `json:"predicted_best_bucket"`
	SecondaryRiskBucket *string         `json:"secondary_risk_bucket"`
	RiskFlagsJSON       json.RawMessage `json:"risk_flags_json"`
	NextCheckInMinutes  int             `json:"next_check_in_minutes"`
	HermesPayloadJSON   json.RawMessage `json:"hermes_payload_json"`
}

// computeAnalysisContentHash returns a SHA-256 hex digest of the analysis
// content. riskFlagsJSON is reused from the Insert call. key_reasons_json and
// confidence are intentionally excluded — see analysisContentHashInput. The
// Hermes payload has its generated_at stripped before hashing so repeated runs
// with the same weather inputs and structural decision produce the same hash.
func computeAnalysisContentHash(
	rec *domain.AnalysisPersistenceRecord,
	riskFlagsJSON json.RawMessage,
) (string, error) {
	stablePayload, err := stripGeneratedAt(rec.HermesPayloadJSON)
	if err != nil {
		return "", fmt.Errorf("strip generated_at from hermes payload: %w", err)
	}
	input := analysisContentHashInput{
		StationCode:         rec.StationCode,
		TargetDateLocal:     rec.TargetDateLocal,
		PredictedBestBucket: rec.Analysis.PredictedBestBucket,
		SecondaryRiskBucket: rec.Analysis.SecondaryRiskBucket,
		RiskFlagsJSON:       riskFlagsJSON,
		NextCheckInMinutes:  rec.Analysis.NextCheckInMinutes,
		HermesPayloadJSON:   stablePayload,
	}
	b, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// stripGeneratedAt returns the JSON object raw with the "generated_at" key
// removed. It unmarshals into map[string]json.RawMessage (preserving all other
// values verbatim) and re-marshals with sorted keys, producing a stable byte
// sequence that is safe to include in a hash.
func stripGeneratedAt(raw json.RawMessage) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "generated_at")
	b, err := json.Marshal(m)
	return json.RawMessage(b), err
}
