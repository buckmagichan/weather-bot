package repository_test

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/repository"
)

// testPool returns a live pgxpool for integration tests. The test is skipped
// when no DB credentials are available in the environment.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// Load .env from the project root (two levels up from internal/repository/).
	_ = godotenv.Load("../../.env")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		user := os.Getenv("POSTGRES_USER")
		pass := os.Getenv("POSTGRES_PASSWORD")
		db := os.Getenv("POSTGRES_DB")
		if user == "" || db == "" {
			t.Skip("no database credentials; set DATABASE_URL or POSTGRES_* env vars")
		}
		u := url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(user, pass),
			Host:     "localhost:5432",
			Path:     db,
			RawQuery: "sslmode=disable",
		}
		dsn = u.String()
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("cannot connect to database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// makeTestForecastSnap builds a minimal but valid ForecastSnapshot for
// insertion. highC and fetchedAt are varied between test rows to produce
// different content hashes (required by the unique index).
func makeTestForecastSnap(stationCode, targetDate string, highC float64, fetchedAt time.Time) *domain.ForecastSnapshot {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	t, _ := time.ParseInLocation("2006-01-02", targetDate, loc)
	hourlyTimes := make([]time.Time, 24)
	for i := range hourlyTimes {
		hourlyTimes[i] = t.Add(time.Duration(i) * time.Hour)
	}
	return &domain.ForecastSnapshot{
		StationCode:      stationCode,
		TargetDateLocal:  targetDate,
		FetchedAt:        fetchedAt,
		Timezone:         "Asia/Shanghai",
		ForecastHighC:    highC,
		HourlyTime:       hourlyTimes,
		HourlyTempC:      make([]float64, 24),
		HourlyDewPointC:  make([]float64, 24),
		HourlyCloudCover: make([]int, 24),
		HourlyPrecipProb: make([]int, 24),
		HourlyWindKMH:    make([]float64, 24),
	}
}

func TestForecastSnapshotRepo_GetLatestForDate(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewForecastSnapshotRepo(pool)
	ctx := context.Background()

	const (
		stationCode = "TEST_FORECAST"
		targetDate  = "2099-12-31" // far-future date; won't collide with real data
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM forecast_snapshots WHERE station_code = $1", stationCode)
	})

	now := time.Now().UTC().Truncate(time.Microsecond) // pg TIMESTAMPTZ has µs precision
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-1 * time.Hour)

	snap1 := makeTestForecastSnap(stationCode, targetDate, 20.0, older)
	snap2 := makeTestForecastSnap(stationCode, targetDate, 21.0, newer) // different highC → different hash

	if _, err := repo.Insert(ctx, snap1); err != nil {
		t.Fatalf("insert snap1: %v", err)
	}
	if _, err := repo.Insert(ctx, snap2); err != nil {
		t.Fatalf("insert snap2: %v", err)
	}

	row, found, err := repo.GetLatestForDate(ctx, stationCode, targetDate)
	if err != nil {
		t.Fatalf("GetLatestForDate: %v", err)
	}
	if !found {
		t.Fatal("GetLatestForDate: expected found=true, got false")
	}
	// The latest row must be snap2 (newer fetched_at, highC=21.0).
	if row.ForecastHighC != 21.0 {
		t.Errorf("ForecastHighC: got %.1f, want 21.0", row.ForecastHighC)
	}
	if row.HourlyPoints != 24 {
		t.Errorf("HourlyPoints: got %d, want 24", row.HourlyPoints)
	}
}

func TestForecastSnapshotRepo_GetPreviousForDate(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewForecastSnapshotRepo(pool)
	ctx := context.Background()

	const (
		stationCode = "TEST_FORECAST_PREV"
		targetDate  = "2099-12-30"
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM forecast_snapshots WHERE station_code = $1", stationCode)
	})

	now := time.Now().UTC().Truncate(time.Microsecond)
	snap1 := makeTestForecastSnap(stationCode, targetDate, 20.0, now.Add(-2*time.Hour))
	snap2 := makeTestForecastSnap(stationCode, targetDate, 21.0, now.Add(-1*time.Hour))

	if _, err := repo.Insert(ctx, snap1); err != nil {
		t.Fatalf("insert snap1: %v", err)
	}
	if _, err := repo.Insert(ctx, snap2); err != nil {
		t.Fatalf("insert snap2: %v", err)
	}

	row, found, err := repo.GetPreviousForDate(ctx, stationCode, targetDate)
	if err != nil {
		t.Fatalf("GetPreviousForDate: %v", err)
	}
	if !found {
		t.Fatal("GetPreviousForDate: expected found=true, got false")
	}
	// The previous row is snap1 (older fetched_at, highC=20.0).
	if row.ForecastHighC != 20.0 {
		t.Errorf("ForecastHighC: got %.1f, want 20.0", row.ForecastHighC)
	}
}

func TestForecastSnapshotRepo_GetLatestForDate_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := repository.NewForecastSnapshotRepo(pool)
	ctx := context.Background()

	_, found, err := repo.GetLatestForDate(ctx, "NONEXISTENT", "2099-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for nonexistent station/date")
	}
}
