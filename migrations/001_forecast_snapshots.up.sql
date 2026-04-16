CREATE TABLE forecast_snapshots (
    id                      BIGSERIAL        PRIMARY KEY,
    station_code            TEXT             NOT NULL,
    target_date_local       DATE             NOT NULL,
    fetched_at              TIMESTAMPTZ      NOT NULL,
    timezone                TEXT             NOT NULL,
    forecast_high_c         DOUBLE PRECISION NOT NULL,
    hourly_time_json        JSONB            NOT NULL,
    hourly_temp_c_json      JSONB            NOT NULL,
    hourly_dew_point_c_json JSONB            NOT NULL,
    hourly_cloud_cover_json JSONB            NOT NULL,
    hourly_precip_prob_json JSONB            NOT NULL,
    hourly_wind_kmh_json    JSONB            NOT NULL,
    content_hash            TEXT             NOT NULL,
    created_at              TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_forecast_snapshots_station_date
    ON forecast_snapshots (station_code, target_date_local);

CREATE INDEX idx_forecast_snapshots_fetched_at
    ON forecast_snapshots (fetched_at DESC);

-- Deduplicates on forecast content, not fetch timestamp.
-- Two fetches returning identical data produce the same hash and the second
-- insert is silently skipped.
CREATE UNIQUE INDEX idx_forecast_snapshots_unique_content
    ON forecast_snapshots (station_code, target_date_local, content_hash);
