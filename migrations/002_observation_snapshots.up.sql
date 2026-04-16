CREATE TABLE observation_snapshots (
    id           BIGSERIAL        PRIMARY KEY,
    station_code TEXT             NOT NULL,
    observed_at  TIMESTAMPTZ      NOT NULL,
    timezone     TEXT             NOT NULL,
    temp_c       DOUBLE PRECISION NOT NULL,
    dew_point_c  DOUBLE PRECISION NULL,
    wind_kmh     DOUBLE PRECISION NULL,
    cloud_cover  INTEGER          NULL,
    precip_mm    DOUBLE PRECISION NULL,
    created_at   TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_observation_snapshots_station_observed
    ON observation_snapshots (station_code, observed_at DESC);

-- Each station has at most one observation per timestamp.
CREATE UNIQUE INDEX idx_observation_snapshots_unique
    ON observation_snapshots (station_code, observed_at);
