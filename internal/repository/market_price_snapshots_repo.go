package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// MarketPriceSnapshotsRepo persists Polymarket bucket price snapshots.
type MarketPriceSnapshotsRepo struct {
	pool *pgxpool.Pool
}

// NewMarketPriceSnapshotsRepo creates a MarketPriceSnapshotsRepo backed by pool.
func NewMarketPriceSnapshotsRepo(pool *pgxpool.Pool) *MarketPriceSnapshotsRepo {
	return &MarketPriceSnapshotsRepo{pool: pool}
}

const insertMarketPriceSnapshotSQL = `
INSERT INTO market_price_snapshots (
    station_code,
    target_date_local,
    captured_at,
    event_slug,
    event_id,
    market_id,
    market_slug,
    bucket_label,
    yes_token_id,
    no_token_id,
    gamma_yes_price,
    gamma_no_price,
    best_bid,
    best_ask,
    midpoint,
    spread,
    last_trade_price,
    volume,
    liquidity,
    active,
    closed,
    predicted_best_bucket,
    secondary_risk_bucket,
    analysis_confidence,
    model_bucket_probability,
    distribution_confidence,
    expected_high_c,
    observed_high_so_far_c,
    latest_observed_temp_c,
    latest_forecast_high_c,
    remaining_forecast_high_c,
    temp_change_last_3h_c,
    observation_points
) VALUES (
    $1, $2::date, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
    $21, $22, $23, $24, $25, $26, $27, $28, $29, $30,
    $31, $32, $33
)
ON CONFLICT (station_code, target_date_local, captured_at, bucket_label) DO NOTHING`

// InsertMany stores one captured event snapshot, one row per temperature bucket.
// The returned count is the number of rows inserted; rows skipped by the
// unique capture/bucket constraint are not counted.
func (r *MarketPriceSnapshotsRepo) InsertMany(
	ctx context.Context,
	snapshots []domain.MarketPriceSnapshot,
) (int, error) {
	if len(snapshots) == 0 {
		return 0, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("insert market price snapshots: begin tx: %w", err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	batch := &pgx.Batch{}
	for i := range snapshots {
		snap := snapshots[i]
		batch.Queue(insertMarketPriceSnapshotSQL,
			snap.StationCode,
			snap.TargetDateLocal,
			snap.CapturedAt,
			snap.EventSlug,
			emptyStringToNil(snap.EventID),
			emptyStringToNil(snap.MarketID),
			snap.MarketSlug,
			snap.BucketLabel,
			snap.YesTokenID,
			emptyStringToNil(snap.NoTokenID),
			snap.GammaYesPrice,
			snap.GammaNoPrice,
			snap.BestBid,
			snap.BestAsk,
			snap.Midpoint,
			snap.Spread,
			snap.LastTradePrice,
			snap.Volume,
			snap.Liquidity,
			snap.Active,
			snap.Closed,
			emptyStringToNil(snap.PredictedBestBucket),
			snap.SecondaryRiskBucket,
			snap.AnalysisConfidence,
			snap.ModelBucketProbability,
			snap.DistributionConfidence,
			snap.ExpectedHighC,
			snap.ObservedHighSoFarC,
			snap.LatestObservedTempC,
			snap.LatestForecastHighC,
			snap.RemainingForecastHighC,
			snap.TempChangeLast3hC,
			snap.ObservationPoints,
		)
	}

	results := tx.SendBatch(ctx, batch)
	inserted := 0
	for i := range snapshots {
		tag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return 0, fmt.Errorf("insert market price snapshots: exec bucket %q: %w", snapshots[i].BucketLabel, err)
		}
		inserted += int(tag.RowsAffected())
	}
	if err := results.Close(); err != nil {
		return 0, fmt.Errorf("insert market price snapshots: close batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("insert market price snapshots: commit: %w", err)
	}
	return inserted, nil
}

func emptyStringToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}
