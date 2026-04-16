package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// ObservationSnapshotRepo persists ObservationSnapshot records to PostgreSQL.
type ObservationSnapshotRepo struct {
	pool *pgxpool.Pool
}

// NewObservationSnapshotRepo creates an ObservationSnapshotRepo backed by pool.
func NewObservationSnapshotRepo(pool *pgxpool.Pool) *ObservationSnapshotRepo {
	return &ObservationSnapshotRepo{pool: pool}
}

const insertObservationSnapshotSQL = `
INSERT INTO observation_snapshots (
    station_code,
    observed_at,
    timezone,
    temp_c,
    dew_point_c,
    wind_kmh,
    cloud_cover,
    precip_mm
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (station_code, observed_at) DO NOTHING`

// Insert persists one ObservationSnapshot. Nullable fields are passed as
// pointers — pgx maps nil to SQL NULL automatically.
// Returns inserted=false when the row already exists (duplicate timestamp).
func (r *ObservationSnapshotRepo) Insert(ctx context.Context, snap *domain.ObservationSnapshot) (inserted bool, err error) {
	tag, err := r.pool.Exec(ctx, insertObservationSnapshotSQL,
		snap.StationCode,
		snap.ObservedAt,
		snap.Timezone,
		snap.TempC,
		snap.DewPointC,
		snap.WindKMH,
		snap.CloudCover,
		snap.PrecipMM,
	)
	if err != nil {
		return false, fmt.Errorf("insert observation: exec: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

const listObservationsForDateSQL = `
SELECT station_code,
       observed_at,
       timezone,
       temp_c,
       dew_point_c,
       wind_kmh,
       cloud_cover,
       precip_mm
FROM   observation_snapshots
WHERE  station_code = $1
  AND  observed_at >= $2
  AND  observed_at <  $3
ORDER  BY observed_at ASC`

// ListForDate returns all ObservationSnapshot rows for stationCode whose
// observed_at falls within the calendar day identified by targetDate
// (YYYY-MM-DD) in the given location. Rows are ordered by observed_at ASC.
// Returns an empty slice (not an error) when no rows are found.
func (r *ObservationSnapshotRepo) ListForDate(
	ctx context.Context,
	stationCode, targetDate string,
	loc *time.Location,
) ([]domain.ObservationSnapshot, error) {
	t, err := time.ParseInLocation("2006-01-02", targetDate, loc)
	if err != nil {
		return nil, fmt.Errorf("list observations for date: parse date %q: %w", targetDate, err)
	}
	startUTC := t.UTC()
	endUTC := t.AddDate(0, 0, 1).UTC()

	rows, err := r.pool.Query(ctx, listObservationsForDateSQL, stationCode, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("list observations for date: query: %w", err)
	}
	defer rows.Close()

	var snaps []domain.ObservationSnapshot
	for rows.Next() {
		var s domain.ObservationSnapshot
		if err := rows.Scan(
			&s.StationCode,
			&s.ObservedAt,
			&s.Timezone,
			&s.TempC,
			&s.DewPointC,
			&s.WindKMH,
			&s.CloudCover,
			&s.PrecipMM,
		); err != nil {
			return nil, fmt.Errorf("list observations for date: scan: %w", err)
		}
		snaps = append(snaps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list observations for date: rows: %w", err)
	}
	return snaps, nil
}
