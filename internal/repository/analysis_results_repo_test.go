package repository_test

// Integration tests for AnalysisResultsRepo. These require a live PostgreSQL
// database and are skipped automatically when none is reachable.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/repository"
)

// makeAnalysisRecord builds a minimal but fully-valid AnalysisPersistenceRecord
// for the given station and date. Each call produces a consistent record that
// can be inserted and deduplicated against.
func makeAnalysisRecord(stationCode, targetDate string) *domain.AnalysisPersistenceRecord {
	secondary := "19C"
	return &domain.AnalysisPersistenceRecord{
		StationCode:     stationCode,
		TargetDateLocal: targetDate,
		GeneratedAt:     time.Date(2099, 1, 1, 10, 0, 0, 0, time.UTC),
		Analysis: &domain.AnalysisResult{
			PredictedBestBucket: "18C",
			SecondaryRiskBucket: &secondary,
			Confidence:          0.9,
			KeyReasons:          []string{"reason one", "reason two"},
			RiskFlags:           []string{"observed_high_exceeds_latest_forecast"},
			NextCheckInMinutes:  60,
		},
		Summary: &domain.WeatherFeatureSummary{
			StationCode:         stationCode,
			TargetDateLocal:     targetDate,
			LatestForecastHighC: 18.0,
			ObservationPoints:   10,
			HourlyPoints:        24,
		},
		Distribution: &domain.TemperatureBucketDistribution{
			StationCode:     stationCode,
			TargetDateLocal: targetDate,
			ExpectedHighC:   18.1,
			Confidence:      0.9,
			BucketProbs: []domain.BucketProbability{
				{Label: "17C or below", Prob: 0.20},
				{Label: "18C", Prob: 0.50},
				{Label: "19C or above", Prob: 0.30},
			},
		},
		HermesPayloadJSON: json.RawMessage(`{"station_code":"` + stationCode + `","target_date_local":"` + targetDate + `"}`),
	}
}

func TestAnalysisResultsRepo_Insert_first_row_inserted(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAnalysisResultsRepo(pool)
	ctx := context.Background()

	const (
		stationCode = "TEST_AR_INSERT"
		targetDate  = "2099-11-01"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM analysis_results WHERE station_code = $1", stationCode)
	})

	rec := makeAnalysisRecord(stationCode, targetDate)
	inserted, err := repo.Insert(ctx, rec)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !inserted {
		t.Error("expected inserted=true for first row, got false")
	}
}

func TestAnalysisResultsRepo_Insert_duplicate_content_skipped(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAnalysisResultsRepo(pool)
	ctx := context.Background()

	const (
		stationCode = "TEST_AR_DUP"
		targetDate  = "2099-11-02"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM analysis_results WHERE station_code = $1", stationCode)
	})

	rec := makeAnalysisRecord(stationCode, targetDate)

	inserted1, err := repo.Insert(ctx, rec)
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if !inserted1 {
		t.Fatal("first insert: expected inserted=true")
	}

	// Same analysis content, different generated_at — must be deduplicated.
	rec.GeneratedAt = rec.GeneratedAt.Add(2 * time.Hour)
	inserted2, err := repo.Insert(ctx, rec)
	if err != nil {
		t.Fatalf("second Insert: %v", err)
	}
	if inserted2 {
		t.Error("second insert with same content: expected inserted=false (duplicate), got true")
	}
}

func TestAnalysisResultsRepo_Insert_changed_content_inserts_new_row(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAnalysisResultsRepo(pool)
	ctx := context.Background()

	const (
		stationCode = "TEST_AR_CHANGED"
		targetDate  = "2099-11-03"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM analysis_results WHERE station_code = $1", stationCode)
	})

	rec := makeAnalysisRecord(stationCode, targetDate)

	inserted1, err := repo.Insert(ctx, rec)
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if !inserted1 {
		t.Fatal("first insert: expected inserted=true")
	}

	// Later in the day the forecast is revised: analysis changes bucket.
	rec.Analysis.PredictedBestBucket = "19C"
	rec.GeneratedAt = rec.GeneratedAt.Add(4 * time.Hour)

	inserted2, err := repo.Insert(ctx, rec)
	if err != nil {
		t.Fatalf("second Insert: %v", err)
	}
	if !inserted2 {
		t.Error("second insert with changed bucket: expected inserted=true for new content, got false")
	}
}

func TestAnalysisResultsRepo_Insert_null_secondary_risk(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewAnalysisResultsRepo(pool)
	ctx := context.Background()

	const (
		stationCode = "TEST_AR_NULL"
		targetDate  = "2099-11-04"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM analysis_results WHERE station_code = $1", stationCode)
	})

	rec := makeAnalysisRecord(stationCode, targetDate)
	rec.Analysis.SecondaryRiskBucket = nil // must store as SQL NULL without error

	inserted, err := repo.Insert(ctx, rec)
	if err != nil {
		t.Fatalf("Insert with nil SecondaryRiskBucket: %v", err)
	}
	if !inserted {
		t.Error("expected inserted=true, got false")
	}
}
