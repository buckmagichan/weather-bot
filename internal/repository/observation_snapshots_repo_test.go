package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/repository"
)

func TestObservationSnapshotRepo_ListForDate(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewObservationSnapshotRepo(pool)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")

	const (
		stationCode = "TEST_OBS"
		targetDate  = "2099-12-31"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM observation_snapshots WHERE station_code = $1", stationCode)
	})

	// Build three observations within the target date in Asia/Shanghai.
	day, _ := time.ParseInLocation("2006-01-02", targetDate, loc)
	obs := []domain.ObservationSnapshot{
		{StationCode: stationCode, ObservedAt: day.Add(10 * time.Hour), Timezone: "Asia/Shanghai", TempC: 14.0},
		{StationCode: stationCode, ObservedAt: day.Add(11 * time.Hour), Timezone: "Asia/Shanghai", TempC: 15.0},
		{StationCode: stationCode, ObservedAt: day.Add(12 * time.Hour), Timezone: "Asia/Shanghai", TempC: 16.0},
	}
	for i := range obs {
		if _, err := repo.Insert(ctx, &obs[i]); err != nil {
			t.Fatalf("insert obs[%d]: %v", i, err)
		}
	}

	rows, err := repo.ListForDate(ctx, stationCode, targetDate, loc)
	if err != nil {
		t.Fatalf("ListForDate: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// Rows must be ordered by observed_at ASC.
	for i := 1; i < len(rows); i++ {
		if rows[i].ObservedAt.Before(rows[i-1].ObservedAt) {
			t.Errorf("rows[%d].ObservedAt (%v) is before rows[%d].ObservedAt (%v): not ASC order",
				i, rows[i].ObservedAt, i-1, rows[i-1].ObservedAt)
		}
	}

	// Temperatures must match insertion order (proxy for correct row identity).
	wantTemps := []float64{14.0, 15.0, 16.0}
	for i, row := range rows {
		if row.TempC != wantTemps[i] {
			t.Errorf("rows[%d].TempC: got %.1f, want %.1f", i, row.TempC, wantTemps[i])
		}
	}
}

func TestObservationSnapshotRepo_ListForDate_Empty(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewObservationSnapshotRepo(pool)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")

	rows, err := repo.ListForDate(ctx, "NONEXISTENT", "2099-01-01", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestObservationSnapshotRepo_ListForDate_DateBoundary(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewObservationSnapshotRepo(pool)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")

	const (
		stationCode = "TEST_OBS_BOUNDARY"
		targetDate  = "2099-12-28"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM observation_snapshots WHERE station_code = $1", stationCode)
	})

	day, _ := time.ParseInLocation("2006-01-02", targetDate, loc)

	// One observation on the target date, one on the next day — only the first
	// should be returned.
	inWindow := domain.ObservationSnapshot{
		StationCode: stationCode, ObservedAt: day.Add(12 * time.Hour),
		Timezone: "Asia/Shanghai", TempC: 15.0,
	}
	outsideWindow := domain.ObservationSnapshot{
		StationCode: stationCode, ObservedAt: day.Add(25 * time.Hour), // next day
		Timezone: "Asia/Shanghai", TempC: 16.0,
	}

	if _, err := repo.Insert(ctx, &inWindow); err != nil {
		t.Fatalf("insert in-window obs: %v", err)
	}
	if _, err := repo.Insert(ctx, &outsideWindow); err != nil {
		t.Fatalf("insert outside-window obs: %v", err)
	}

	rows, err := repo.ListForDate(ctx, stationCode, targetDate, loc)
	if err != nil {
		t.Fatalf("ListForDate: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row within date boundary, got %d", len(rows))
	}
	if len(rows) == 1 && rows[0].TempC != 15.0 {
		t.Errorf("wrong row returned: TempC=%.1f, want 15.0", rows[0].TempC)
	}
}
