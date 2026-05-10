CREATE TABLE market_price_snapshots (
    id BIGSERIAL PRIMARY KEY,
    station_code TEXT NOT NULL,
    target_date_local DATE NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL,
    event_slug TEXT NOT NULL,
    event_id TEXT,
    market_id TEXT,
    market_slug TEXT NOT NULL,
    bucket_label TEXT NOT NULL,
    yes_token_id TEXT NOT NULL,
    no_token_id TEXT,
    gamma_yes_price DOUBLE PRECISION,
    gamma_no_price DOUBLE PRECISION,
    best_bid DOUBLE PRECISION,
    best_ask DOUBLE PRECISION,
    midpoint DOUBLE PRECISION,
    spread DOUBLE PRECISION,
    last_trade_price DOUBLE PRECISION,
    volume DOUBLE PRECISION,
    liquidity DOUBLE PRECISION,
    active BOOLEAN NOT NULL,
    closed BOOLEAN NOT NULL,
    predicted_best_bucket TEXT,
    secondary_risk_bucket TEXT,
    analysis_confidence DOUBLE PRECISION,
    model_bucket_probability DOUBLE PRECISION,
    distribution_confidence DOUBLE PRECISION,
    expected_high_c DOUBLE PRECISION,
    observed_high_so_far_c DOUBLE PRECISION,
    latest_observed_temp_c DOUBLE PRECISION,
    latest_forecast_high_c DOUBLE PRECISION,
    remaining_forecast_high_c DOUBLE PRECISION,
    temp_change_last_3h_c DOUBLE PRECISION,
    observation_points INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_market_price_snapshots_station_date_time
    ON market_price_snapshots (station_code, target_date_local, captured_at);

CREATE INDEX idx_market_price_snapshots_event_time
    ON market_price_snapshots (event_slug, captured_at);

CREATE INDEX idx_market_price_snapshots_bucket_time
    ON market_price_snapshots (station_code, target_date_local, bucket_label, captured_at);

CREATE UNIQUE INDEX idx_market_price_snapshots_unique_capture_bucket
    ON market_price_snapshots (station_code, target_date_local, captured_at, bucket_label);
