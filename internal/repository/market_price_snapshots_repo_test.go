package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/repository"
)

func TestMarketPriceSnapshotsRepo_InsertMany(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewMarketPriceSnapshotsRepo(pool)
	ctx := context.Background()

	var tableName sql.NullString
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.market_price_snapshots')::text").Scan(&tableName); err != nil {
		t.Fatalf("check market_price_snapshots table: %v", err)
	}
	if !tableName.Valid {
		t.Skip("market_price_snapshots table is not migrated")
	}

	const (
		stationCode = "TEST_PM"
		targetDate  = "2099-12-31"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM market_price_snapshots WHERE station_code = $1", stationCode)
	})

	gammaYes := 0.73
	bestBid := 0.71
	bestAsk := 0.75
	modelProb := 0.73
	observedHigh := 24.0
	inserted, err := repo.InsertMany(ctx, []domain.MarketPriceSnapshot{
		{
			StationCode:            stationCode,
			TargetDateLocal:        targetDate,
			CapturedAt:             time.Date(2099, 12, 31, 10, 30, 0, 0, time.UTC),
			EventSlug:              "highest-temperature-in-test-on-december-31-2099",
			EventID:                "event-1",
			MarketID:               "market-24",
			MarketSlug:             "test-24c",
			BucketLabel:            "24C",
			YesTokenID:             "yes-24",
			NoTokenID:              "no-24",
			GammaYesPrice:          &gammaYes,
			BestBid:                &bestBid,
			BestAsk:                &bestAsk,
			Active:                 true,
			Closed:                 false,
			PredictedBestBucket:    "24C",
			AnalysisConfidence:     floatPointer(0.9),
			ModelBucketProbability: &modelProb,
			ObservedHighSoFarC:     &observedHigh,
			ObservationPoints:      18,
		},
	})
	if err != nil {
		t.Fatalf("InsertMany: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1", inserted)
	}
	inserted, err = repo.InsertMany(ctx, []domain.MarketPriceSnapshot{
		{
			StationCode:     stationCode,
			TargetDateLocal: targetDate,
			CapturedAt:      time.Date(2099, 12, 31, 10, 30, 0, 0, time.UTC),
			EventSlug:       "highest-temperature-in-test-on-december-31-2099",
			MarketSlug:      "test-24c",
			BucketLabel:     "24C",
			YesTokenID:      "yes-24",
			Active:          true,
			Closed:          false,
		},
	})
	if err != nil {
		t.Fatalf("InsertMany duplicate: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("duplicate inserted = %d, want 0", inserted)
	}

	var gotBucket string
	var gotBestBid float64
	var gotModelProb float64
	var gotObsPoints int
	err = pool.QueryRow(ctx, `
SELECT bucket_label, best_bid, model_bucket_probability, observation_points
FROM market_price_snapshots
WHERE station_code = $1 AND target_date_local = $2::date
`, stationCode, targetDate).Scan(&gotBucket, &gotBestBid, &gotModelProb, &gotObsPoints)
	if err != nil {
		t.Fatalf("select inserted row: %v", err)
	}
	if gotBucket != "24C" || gotBestBid != bestBid || gotModelProb != modelProb || gotObsPoints != 18 {
		t.Fatalf("inserted row mismatch: bucket=%q bid=%v prob=%v obs=%d", gotBucket, gotBestBid, gotModelProb, gotObsPoints)
	}
}

func floatPointer(v float64) *float64 {
	return &v
}
