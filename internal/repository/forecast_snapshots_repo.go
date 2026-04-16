package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

// ForecastSnapshotRepo persists ForecastSnapshot records to PostgreSQL.
type ForecastSnapshotRepo struct {
	pool *pgxpool.Pool
}

// NewForecastSnapshotRepo creates a ForecastSnapshotRepo backed by pool.
func NewForecastSnapshotRepo(pool *pgxpool.Pool) *ForecastSnapshotRepo {
	return &ForecastSnapshotRepo{pool: pool}
}

const insertForecastSnapshotSQL = `
INSERT INTO forecast_snapshots (
    station_code,
    target_date_local,
    fetched_at,
    timezone,
    forecast_high_c,
    hourly_time_json,
    hourly_temp_c_json,
    hourly_dew_point_c_json,
    hourly_cloud_cover_json,
    hourly_precip_prob_json,
    hourly_wind_kmh_json,
    content_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (station_code, target_date_local, content_hash) DO NOTHING`

// Insert persists a ForecastSnapshot to the forecast_snapshots table.
// A content_hash is computed from the forecast data before insert.
// If an identical forecast (same station, date, and content) has already been
// stored, the insert is skipped and inserted returns false.
func (r *ForecastSnapshotRepo) Insert(ctx context.Context, snap *domain.ForecastSnapshot) (inserted bool, err error) {
	targetDate, err := time.Parse("2006-01-02", snap.TargetDateLocal)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: parse target date %q: %w", snap.TargetDateLocal, err)
	}

	hash, err := computeContentHash(snap)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: compute content hash: %w", err)
	}

	hourlyTimeJSON, err := toJSONB(snap.HourlyTime)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: marshal hourly_time: %w", err)
	}
	hourlyTempJSON, err := toJSONB(snap.HourlyTempC)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: marshal hourly_temp_c: %w", err)
	}
	hourlyDewJSON, err := toJSONB(snap.HourlyDewPointC)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: marshal hourly_dew_point_c: %w", err)
	}
	hourlyCloudJSON, err := toJSONB(snap.HourlyCloudCover)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: marshal hourly_cloud_cover: %w", err)
	}
	hourlyPrecipJSON, err := toJSONB(snap.HourlyPrecipProb)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: marshal hourly_precip_prob: %w", err)
	}
	hourlyWindJSON, err := toJSONB(snap.HourlyWindKMH)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: marshal hourly_wind_kmh: %w", err)
	}

	tag, err := r.pool.Exec(ctx, insertForecastSnapshotSQL,
		snap.StationCode,
		targetDate,
		snap.FetchedAt,
		snap.Timezone,
		snap.ForecastHighC,
		hourlyTimeJSON,
		hourlyTempJSON,
		hourlyDewJSON,
		hourlyCloudJSON,
		hourlyPrecipJSON,
		hourlyWindJSON,
		hash,
	)
	if err != nil {
		return false, fmt.Errorf("insert snapshot: exec: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// contentHashPayload defines the fields that contribute to content identity.
// fetched_at and created_at are deliberately excluded — they reflect when the
// fetch happened, not what the forecast says.
type contentHashPayload struct {
	StationCode      string      `json:"station_code"`
	TargetDateLocal  string      `json:"target_date_local"`
	Timezone         string      `json:"timezone"`
	ForecastHighC    float64     `json:"forecast_high_c"`
	HourlyTime       []time.Time `json:"hourly_time"`
	HourlyTempC      []float64   `json:"hourly_temp_c"`
	HourlyDewPointC  []float64   `json:"hourly_dew_point_c"`
	HourlyCloudCover []int       `json:"hourly_cloud_cover"`
	HourlyPrecipProb []int       `json:"hourly_precip_prob"`
	HourlyWindKMH    []float64   `json:"hourly_wind_kmh"`
}

// computeContentHash returns a SHA-256 hex string derived from the forecast
// content fields. The hash is deterministic: the same forecast data always
// produces the same hash regardless of when it was fetched.
func computeContentHash(snap *domain.ForecastSnapshot) (string, error) {
	payload := contentHashPayload{
		StationCode:      snap.StationCode,
		TargetDateLocal:  snap.TargetDateLocal,
		Timezone:         snap.Timezone,
		ForecastHighC:    snap.ForecastHighC,
		HourlyTime:       snap.HourlyTime,
		HourlyTempC:      snap.HourlyTempC,
		HourlyDewPointC:  snap.HourlyDewPointC,
		HourlyCloudCover: snap.HourlyCloudCover,
		HourlyPrecipProb: snap.HourlyPrecipProb,
		HourlyWindKMH:    snap.HourlyWindKMH,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// toJSONB marshals v to a json.RawMessage. pgx v5's JSONB codec accepts
// json.RawMessage directly, so no pgtype wrapper is needed.
func toJSONB(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// ForecastRow carries the scalar fields needed by the feature-summary pipeline.
// The hourly JSONB arrays are not deserialized; HourlyPoints is obtained via
// jsonb_array_length computed in SQL.
type ForecastRow struct {
	ForecastHighC float64
	FetchedAt     time.Time
	HourlyPoints  int
}

const getLatestForecastSQL = `
SELECT forecast_high_c,
       fetched_at,
       jsonb_array_length(hourly_time_json),
       jsonb_array_length(hourly_temp_c_json),
       jsonb_array_length(hourly_dew_point_c_json),
       jsonb_array_length(hourly_cloud_cover_json),
       jsonb_array_length(hourly_precip_prob_json),
       jsonb_array_length(hourly_wind_kmh_json)
FROM   forecast_snapshots
WHERE  station_code      = $1
  AND  target_date_local = $2
ORDER  BY fetched_at DESC
LIMIT  1`

// GetLatestForDate returns the most recent ForecastRow for the given station
// and date. found is false (with a nil error) when no rows exist.
// Returns an error if the hourly arrays are not all the same length, which
// indicates snapshot corruption.
func (r *ForecastSnapshotRepo) GetLatestForDate(
	ctx context.Context,
	stationCode, targetDate string,
) (row ForecastRow, found bool, err error) {
	parsed, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return ForecastRow{}, false,
			fmt.Errorf("get latest forecast: parse date %q: %w", targetDate, err)
	}
	var timeLen, tempLen, dewLen, cloudLen, precipLen, windLen int
	scanErr := r.pool.QueryRow(ctx, getLatestForecastSQL, stationCode, parsed).
		Scan(&row.ForecastHighC, &row.FetchedAt,
			&timeLen, &tempLen, &dewLen, &cloudLen, &precipLen, &windLen)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return ForecastRow{}, false, nil
	}
	if scanErr != nil {
		return ForecastRow{}, false,
			fmt.Errorf("get latest forecast: scan: %w", scanErr)
	}
	if tempLen != timeLen || dewLen != timeLen || cloudLen != timeLen || precipLen != timeLen || windLen != timeLen {
		return ForecastRow{}, false, fmt.Errorf(
			"get latest forecast: hourly array length mismatch for %s %s: time=%d temp=%d dew=%d cloud=%d precip=%d wind=%d",
			stationCode, targetDate, timeLen, tempLen, dewLen, cloudLen, precipLen, windLen,
		)
	}
	row.HourlyPoints = timeLen
	return row, true, nil
}

const getPreviousForecastSQL = `
SELECT forecast_high_c,
       fetched_at,
       jsonb_array_length(hourly_time_json),
       jsonb_array_length(hourly_temp_c_json),
       jsonb_array_length(hourly_dew_point_c_json),
       jsonb_array_length(hourly_cloud_cover_json),
       jsonb_array_length(hourly_precip_prob_json),
       jsonb_array_length(hourly_wind_kmh_json)
FROM   forecast_snapshots
WHERE  station_code      = $1
  AND  target_date_local = $2
ORDER  BY fetched_at DESC
LIMIT  1
OFFSET 1`

// GetPreviousForDate returns the second-most-recent ForecastRow for the given
// station and date (the snapshot immediately before the latest one).
// found is false when fewer than two snapshots exist for the date.
// Returns an error if the hourly arrays are not all the same length, which
// indicates snapshot corruption.
func (r *ForecastSnapshotRepo) GetPreviousForDate(
	ctx context.Context,
	stationCode, targetDate string,
) (row ForecastRow, found bool, err error) {
	parsed, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return ForecastRow{}, false,
			fmt.Errorf("get previous forecast: parse date %q: %w", targetDate, err)
	}
	var timeLen, tempLen, dewLen, cloudLen, precipLen, windLen int
	scanErr := r.pool.QueryRow(ctx, getPreviousForecastSQL, stationCode, parsed).
		Scan(&row.ForecastHighC, &row.FetchedAt,
			&timeLen, &tempLen, &dewLen, &cloudLen, &precipLen, &windLen)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return ForecastRow{}, false, nil
	}
	if scanErr != nil {
		return ForecastRow{}, false,
			fmt.Errorf("get previous forecast: scan: %w", scanErr)
	}
	if tempLen != timeLen || dewLen != timeLen || cloudLen != timeLen || precipLen != timeLen || windLen != timeLen {
		return ForecastRow{}, false, fmt.Errorf(
			"get previous forecast: hourly array length mismatch for %s %s: time=%d temp=%d dew=%d cloud=%d precip=%d wind=%d",
			stationCode, targetDate, timeLen, tempLen, dewLen, cloudLen, precipLen, windLen,
		)
	}
	row.HourlyPoints = timeLen
	return row, true, nil
}
