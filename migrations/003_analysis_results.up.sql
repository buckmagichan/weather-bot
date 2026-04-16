CREATE TABLE analysis_results (
    id                       BIGSERIAL    PRIMARY KEY,
    station_code             TEXT         NOT NULL,
    target_date_local        DATE         NOT NULL,
    generated_at             TIMESTAMPTZ  NOT NULL,
    predicted_best_bucket    TEXT         NOT NULL,
    secondary_risk_bucket    TEXT,
    confidence               DOUBLE PRECISION NOT NULL,
    key_reasons_json         JSONB        NOT NULL,
    risk_flags_json          JSONB        NOT NULL,
    next_check_in_minutes    INTEGER      NOT NULL,
    feature_summary_json     JSONB        NOT NULL,
    bucket_distribution_json JSONB        NOT NULL,
    hermes_payload_json      JSONB        NOT NULL,
    analysis_content_hash    TEXT         NOT NULL,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX ON analysis_results (station_code, target_date_local);
CREATE INDEX ON analysis_results (generated_at DESC);
CREATE UNIQUE INDEX ON analysis_results (station_code, target_date_local, analysis_content_hash);
