# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go run ./cmd/app           # run the app (forecast → observations → feature summary → bucket distribution → Hermes analysis)
go test ./... -v           # run all tests
go test ./path/to/pkg -run TestName -v  # run a single test
go mod tidy                # tidy dependencies
```

```bash
# PostgreSQL (Docker)
docker compose --env-file .env -f infra/docker-compose.postgres.yml up -d
docker compose --env-file .env -f infra/docker-compose.postgres.yml down
docker compose --env-file .env -f infra/docker-compose.postgres.yml logs -f postgres
```

```bash
# Migrations (run after starting postgres)
docker exec -i weather-bot-postgres psql -U weatherbot -d weatherbot < migrations/001_forecast_snapshots.up.sql
docker exec -i weather-bot-postgres psql -U weatherbot -d weatherbot < migrations/002_observation_snapshots.up.sql
```

`.env` is loaded automatically at startup via `godotenv.Load()`. No `source .env` needed. Required vars: `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`. Optional: `METEOSTAT_API_KEY` (skips observation ingestion if absent), `POSTGRES_SSL_MODE` (libpq sslmode value; defaults to `disable` for local Docker — set to `require` or higher for any remote DB).

## Architecture

Go backend service (`github.com/buckmagichan/weather-bot`) predicting Shanghai (ZSPD) daily highest temperature. Forecasts are fetched from Open-Meteo and observations from Meteostat; both are persisted to PostgreSQL. A feature summary, bucket probability distribution, Hermes analysis payload, and Hermes analysis result are computed in-process.

### Layer overview

```
cmd/app/main.go
    ├── services.FetchForecastService           →  openmeteo.Client    →  api.open-meteo.com/v1
    ├── repository.ForecastSnapshotRepo         →  pgxpool             →  forecast_snapshots
    ├── services.FetchObservationService        →  meteostat.Client    →  meteostat.p.rapidapi.com
    ├── repository.ObservationSnapshotRepo      →  pgxpool             →  observation_snapshots
    ├── services.BuildFeatureSummaryService     (reads from both repos)
    ├── services.BuildBucketDistributionService (pure computation, no DB)
    ├── services.BuildHermesPayloadService      (pure computation, no DB)
    └── services.BuildAnalysisService           →  hermes.Bridge       →  hermes CLI (highest-temp-analysis skill)
```

### `cmd/app/main.go`
Loads `.env` via `godotenv`. Builds DSN from `DATABASE_URL` (preferred) or `POSTGRES_*` env vars (default host `localhost:5432`). Two contexts: 10 s for DB pool creation, 15 s for DB/network business logic, 90 s for the Hermes CLI call. Runs forecast → observation → feature summary → bucket distribution → Hermes payload → Hermes analysis in sequence. Prints each section; the Hermes payload is printed as pretty JSON under `--- Hermes Payload ---`, and the analysis result is printed under `--- Hermes Analysis ---`. Exits early after forecast if `METEOSTAT_API_KEY` is absent; logs and continues if the Hermes call fails (non-fatal).

### `internal/domain/`
- `forecast.go` — `ForecastSnapshot`: flat struct for DB persistence. Key fields: `StationCode`, `TargetDateLocal` (YYYY-MM-DD local), `ForecastHighC`, parallel hourly slices (`HourlyTime []time.Time`, `HourlyTempC`, `HourlyDewPointC`, `HourlyCloudCover`, `HourlyPrecipProb`, `HourlyWindKMH`). Wind speed in km/h (Open-Meteo default).
- `observation.go` — `ObservationSnapshot`: single hourly observation. `TempC` required; `DewPointC`, `WindKMH`, `CloudCover` (WMO coco code 1–27), `PrecipMM` are nullable pointers.
- `feature_summary.go` — `WeatherFeatureSummary`: derived view over latest forecast + observations. All optional fields are `*float64`/`*time.Time` — nil means "no data", never an error. All exported fields carry `json` tags (snake_case).
- `bucket_distribution.go` — `TemperatureBucketDistribution` + `BucketProbability`: output of the bucket service. `BucketProbs` sums to exactly 1.0. All exported fields carry `json` tags (snake_case).
- `analysis_result.go` — `AnalysisResult`: parsed response from the `highest-temp-analysis` Hermes skill. Fields: `PredictedBestBucket string`, `SecondaryRiskBucket *string` (nil when no secondary risk), `Confidence float64`, `KeyReasons []string`, `RiskFlags []string`, `NextCheckInMinutes int`. All fields carry `json` tags (snake_case).
- `analysis_persistence.go` — `AnalysisPersistenceRecord`: bundles everything needed for one DB row: `StationCode`, `TargetDateLocal`, `GeneratedAt`, `Analysis *AnalysisResult`, `Summary *WeatherFeatureSummary`, `Distribution *TemperatureBucketDistribution`, `HermesPayloadJSON json.RawMessage` (pre-serialised to avoid import cycle with the hermes package).

### `internal/services/`
- `fetch_forecast_service.go` — `FetchDailySnapshot(ctx)`: hardcoded ZSPD coords `31.1443, 121.8083` (elevation 2 m, not city centre). Hourly times parsed with `time.ParseInLocation` into Asia/Shanghai.
- `fetch_observation_service.go` — `FetchTodayObservations(ctx)`: calls Meteostat `/point/hourly` for today's local date. Time format `"2006-01-02 15:04:05"`. Skips rows with missing `temp` or `time`.
- `build_feature_summary_service.go` — `Build(ctx, stationCode, targetDate, now time.Time)`: reads from `forecastStore` and `observationStore` interfaces (concrete repos satisfy them). Returns error if no forecast found; gracefully handles missing obs with nil fields. **Observed-so-far filtering**: only observations with `observed_at <= now` are used — Meteostat sometimes returns a full-day dataset ahead of real time, so future-hour rows are excluded. `computeTempChangeLast3h` uses a `[latest-3h, latest]` window (inclusive, ≥2 obs required). Pass `time.Now()` in production; pass a fixed time in tests.
- `build_bucket_distribution_service.go` — `Build(summary)`: pure function, no DB, no error return. `summary` must not be nil — passing nil panics with an explicit message (programming error, not a runtime condition). Three additive rules: forecast trend (weight 0.25, cap ±0.5 C), observed high vs forecast (gap 0.30/0.15), 3h momentum (±0.10 C for |change|>1). Gaussian CDF via `math.Erfc` for bucket probabilities. Spread: 0.75/0.90/1.20 σ based on available data. Confidence: 0.50 base + bonuses (max 0.90).
- `build_hermes_payload_service.go` — `Build(summary, dist)`: pure function, no DB. Validates that `summary` and `dist` share the same `StationCode` and `TargetDateLocal`; returns an error on mismatch or nil inputs. Produces a `hermes.HermesAnalysisPayload` with: (1) rounded numeric values (`round2` for temperatures/confidence, `round4` for probabilities); (2) Hermes-facing view types that omit redundant identity/timestamp fields; (3) `SanityFlags []string` computed by `computeSanityFlags`. **Sanity flag rules** (evaluated in order): `observed_high_exceeds_latest_forecast` when `ObservedHighSoFarC > LatestForecastHighC`; `missing_previous_forecast` when `PreviousForecastHighC == nil`; `no_observation_data` when `ObservationPoints == 0`; `limited_observation_coverage` when `0 < ObservationPoints < 6`.
- `build_analysis_service.go` — `Build(ctx, summary, dist)`: thin orchestrator — assembles the Hermes payload via `BuildHermesPayloadService`, then calls `hermes.Bridge.Analyze` and returns the parsed `domain.AnalysisResult`. All errors are wrapped with context.
- `analysis_results_repo.go` — `AnalysisResultsRepo.Insert(ctx, rec)`: serialises JSONB fields once (reused for both hash and DB insert), computes `analysis_content_hash` (SHA-256 of all content fields excluding `generated_at`/`created_at`/`id`), executes `INSERT ... ON CONFLICT DO NOTHING`, returns `inserted bool`. The hash struct `analysisContentHashInput` uses `json.RawMessage` for the three JSONB blobs so they are included verbatim in the digest. **Hash exclusions** (LLM fields that vary between runs for identical inputs): `confidence` (LLM "adjusts" the pipeline value, e.g. 0.75 vs 0.80), `key_reasons_json` (free-text prose, wording varies), `feature_summary_json`/`bucket_distribution_json` (contain volatile `generated_at`; weather content is already inside `hermes_payload_json`). **`hermes_payload_json`** is included with its own `generated_at` stripped via `stripGeneratedAt` (unmarshal → delete key → re-marshal with sorted keys). Stable hash inputs: `station_code`, `target_date_local`, `predicted_best_bucket`, `secondary_risk_bucket`, `risk_flags_json`, `next_check_in_minutes`, `hermes_payload_json` (stripped).

### `internal/hermes/`
- `skill_payload.go` — defines the stable Hermes boundary types. **`HermesAnalysisPayload`**: top-level envelope with `StationCode`, `TargetDateLocal`, `GeneratedAt` (single timestamp, not repeated in nested views), `FeatureSummary`, `BucketDistribution`, `SanityFlags`. **`FeatureSummaryView`**: Hermes projection of `WeatherFeatureSummary` — omits `StationCode`, `TargetDateLocal`, `GeneratedAt`, `ForecastSnapshotFetchedAt`; all floats pre-rounded. **`BucketDistributionView`** + **`BucketProbView`**: Hermes projection of `TemperatureBucketDistribution` — omits identity and timestamp fields; all floats pre-rounded. **`MarshalPayload(p)`** returns compact JSON `([]byte, error)`. **`MustPrettyJSON(p)`** returns 2-space-indented JSON string (panics on marshal failure).
- `bridge.go` — `Bridge`: calls `hermes chat --toolsets skills -q <prompt>` via `exec.CommandContext`. Payload JSON is embedded inline in the prompt (no temp file). `parseOutput` calls `extractLastJSONObject` to locate the last valid top-level JSON object in stdout (tolerates banner text, query echo, UI chrome), then unmarshals it into `domain.AnalysisResult`. `extractLastJSONObject` scans right-to-left for `}`, then forward-scans for the matching `{` via `findObjectEnd` (handles quoted strings and `\"` escapes correctly). If the last brace pair is found but fails `json.Valid`, returns an error immediately rather than silently falling back to an earlier object. `NewBridgeWithBin(bin)` allows binary path injection for testing.

### `internal/providers/`
- `openmeteo/client.go` — `Client` with functional options (`WithTimeout`, `WithBaseURL`, `WithHTTPClient`). Default 10 s timeout. Non-200 errors include path + body truncated to 256 bytes.
- `openmeteo/forecast.go` — `Forecast(ctx, ForecastParams)`. `Var*` / unit / timezone constants. Pre-flight validation (coord ranges, ForecastDays 1–16, PastDays 0–92, enum units). Post-decode slice alignment check. 17 tests via `httptest.NewServer`.
- `meteostat/client.go` — same option pattern as openmeteo; adds `x-rapidapi-key`/`x-rapidapi-host` headers. API key from `METEOSTAT_API_KEY` env var.
- `meteostat/observation.go` — `PointHourly(ctx, PointHourlyParams)`. Pre-flight validation: lat `[-90,90]`, lon `[-180,180]`, Start/End non-empty `YYYY-MM-DD` (matches Open-Meteo pattern). All measurement fields are `*float64`/`*int` (Meteostat returns null for sparse data).

### `internal/repository/`
- `db.go` — `NewPostgresPool(ctx, dsn)`: creates pool, pings before returning.
- `forecast_snapshots_repo.go` — `Insert` (SHA-256 content_hash dedup), `GetLatestForDate`, `GetPreviousForDate` (returns `ForecastRow`; fetches `jsonb_array_length` for all six hourly arrays and returns an error if lengths diverge — surfaces corruption without JSONB deserialization).
- `observation_snapshots_repo.go` — `Insert` (unique on `(station_code, observed_at)`), `ListForDate(ctx, stationCode, targetDate, loc)` (UTC range query, ASC order). Returns ALL stored rows for the target local date; the observed-so-far cutoff is applied upstream in `BuildFeatureSummaryService`, not here.

### `migrations/`
- `001_forecast_snapshots.up.sql` — `forecast_snapshots` table: unique index on `(station_code, target_date_local, content_hash)`.
- `002_observation_snapshots.up.sql` — `observation_snapshots` table: `observed_at TIMESTAMPTZ NOT NULL`; unique index on `(station_code, observed_at)`.
- `003_analysis_results.up.sql` — `analysis_results` table: all scalar analysis fields + four JSONB columns (`key_reasons_json`, `risk_flags_json`, `feature_summary_json`, `bucket_distribution_json`, `hermes_payload_json`); unique index on `(station_code, target_date_local, analysis_content_hash)`; plain indexes on `(station_code, target_date_local)` and `(generated_at DESC)`. Down migration drops the table.
- Applied manually via `docker exec psql`. No migration runner yet.

### `infra/`
PostgreSQL via `docker-compose.postgres.yml`. Credentials in `.env`. Volume at `/var/lib/postgresql/data`.

### `hermes-skills/weather/highest-temp-analysis/SKILL.md`
Local Hermes skill. Takes a `HermesAnalysisPayload` JSON as input and returns a strict 6-field JSON object: `predicted_best_bucket`, `secondary_risk_bucket` (string or null), `confidence`, `key_reasons` (2–4 strings), `risk_flags` (string array), `next_check_in_minutes` (integer, prefer 30/60/90). Output contract is strict: raw JSON only, no markdown, no extra keys. Version 1.1.0.

## Implementation status

| Step | Description | Status |
|------|-------------|--------|
| 1 | Forecast ingestion (Open-Meteo → DB) | ✅ done |
| 2 | Observation ingestion (Meteostat → DB) | ✅ done |
| 3 | Feature summary (`WeatherFeatureSummary`) | ✅ done |
| 4 | Bucket distribution (`TemperatureBucketDistribution`) | ✅ done |
| 5 | Hermes payload assembly (`HermesAnalysisPayload`) | ✅ done |
| 6.1 | Hermes skill (`highest-temp-analysis`) — local install + contract | ✅ done |
| 6.2 | Hermes bridge (Go → CLI → `domain.AnalysisResult`) | ✅ done |
| 7 | Persistence of analysis results (`analysis_results` table) | ✅ done |
| 8 | Scheduled/periodic execution | 🔜 next |
