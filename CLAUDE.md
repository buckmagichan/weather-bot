# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go run ./cmd/app           # run the app (forecast → observations → feature summary → bucket distribution → analysis)
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
docker exec -i weather-bot-postgres psql -U weatherbot -d weatherbot < migrations/003_analysis_results.up.sql
```

`.env` is loaded automatically at startup via `godotenv.Load()`. No `source .env` needed. Required vars: `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`. Optional: `POSTGRES_SSL_MODE` (libpq sslmode value; defaults to `disable` for local Docker — set to `require` or higher for any remote DB) and `HERMES_TIMEOUT_SECONDS`.

## Architecture

Go backend service (`github.com/buckmagichan/weather-bot`) predicting daily highest temperature markets for configured China airport settlement stations. The current station set, in run/print order, is Beijing Capital (ZBAA), Shanghai Pudong (ZSPD), Guangzhou Baiyun (ZGGG), Chengdu Shuangliu (ZUUU), Chongqing Jiangbei (ZUCK), Wuhan Tianhe (ZHHH), and Qingdao Jiaodong (ZSQD). Forecasts are fetched from Open-Meteo and observations from Aviation Weather Center METAR; both are persisted to PostgreSQL. Wunderground station-history pages are fetched opportunistically as the Polymarket resolution-aligned observed-high source. A feature summary, bucket probability distribution, Hermes analysis payload, and Hermes analysis result are computed in-process for each station.

### Layer overview

```
cmd/app/main.go
    ├── services.FetchForecastService           →  openmeteo.Client    →  api.open-meteo.com/v1
    ├── repository.ForecastSnapshotRepo         →  pgxpool             →  forecast_snapshots
    ├── services.FetchObservationService        →  aviationweather.Client → aviationweather.gov/api/data/metar
    ├── repository.ObservationSnapshotRepo      →  pgxpool             →  observation_snapshots
    ├── services.FetchResolutionObservationService → wunderground.Client → wunderground.com/history/daily
    ├── services.BuildFeatureSummaryService     (reads from both repos)
    ├── services.BuildBucketDistributionService (pure computation, no DB)
    ├── services.BuildHermesPayloadService      (pure computation, no DB)
    └── services.BuildAnalysisService           →  hermes.Bridge       →  hermes CLI (highest-temp-analysis skill), with local fallback
```

### `cmd/app/main.go`
Loads `.env` via `godotenv`. Builds DSN from `DATABASE_URL` (preferred) or `POSTGRES_*` env vars (default host `localhost:5432`). Distinct contexts: 10 s pool creation, 15 s forecast, 25 s observations (METAR latency can be bursty), 10 s observation inserts, 10 s summary, 12 s Wunderground resolution fetch, Hermes CLI timeout from `HERMES_TIMEOUT_SECONDS` (default 3 minutes), 5 s persistence. Iterates `services.MarketWeatherStations()` and runs forecast → observation → feature summary → resolution observation → bucket distribution → Hermes payload → analysis → persist analysis for each station using that station's local current date. Observation/resolution fetch failures are logged and the station continues with the best available data; Hermes rate-limit/timeout falls back to local deterministic analysis, and persistence failures are non-fatal for that station. If one station fails during forecast/summary setup, the app logs it and continues to the next station; it exits fatally only if no station completes.

### `internal/domain/`
- `forecast.go` — `ForecastSnapshot`: flat struct for DB persistence. Key fields: `StationCode`, `TargetDateLocal` (YYYY-MM-DD local), `ForecastHighC`, parallel hourly slices (`HourlyTime []time.Time`, `HourlyTempC`, `HourlyDewPointC`, `HourlyCloudCover`, `HourlyPrecipProb`, `HourlyWindKMH`). Wind speed in km/h (Open-Meteo default).
- `observation.go` — `ObservationSnapshot`: single hourly observation. `TempC` required; `DewPointC`, `WindKMH` are nullable pointers. `CloudCover` and `PrecipMM` are declared but not populated by the METAR provider.
- `feature_summary.go` — `WeatherFeatureSummary`: derived view over latest forecast + observations, plus station timezone, temperature profile, optional Wunderground resolution high/source type, and remaining forecast high after the latest observation. All optional fields are `*float64`/`*time.Time` — nil means "no data", never an error. All exported fields carry `json` tags (snake_case).
- `bucket_distribution.go` — `TemperatureBucketDistribution` + `BucketProbability`: output of the bucket service. `BucketProbs` sums to exactly 1.0. All exported fields carry `json` tags (snake_case).
- `analysis_result.go` — `AnalysisResult`: parsed response from the `highest-temp-analysis` Hermes skill. Fields: `PredictedBestBucket string`, `SecondaryRiskBucket *string` (nil when no secondary risk), `Confidence float64`, `KeyReasons []string`, `RiskFlags []string`, `NextCheckInMinutes int`. All fields carry `json` tags (snake_case).
- `analysis_persistence.go` — `AnalysisPersistenceRecord`: bundles everything needed for one DB row: `StationCode`, `TargetDateLocal`, `GeneratedAt`, `Analysis *AnalysisResult`, `Summary *WeatherFeatureSummary`, `Distribution *TemperatureBucketDistribution`, `HermesPayloadJSON json.RawMessage` (pre-serialised to avoid import cycle with the hermes package).

### `internal/services/`
- `weather_station.go` — `WeatherStation` config for market settlement stations. Current stations, in run/print order: Beijing Capital (ZBAA), Shanghai Pudong (ZSPD), Guangzhou Baiyun (ZGGG), Chengdu Shuangliu (ZUUU), Chongqing Jiangbei (ZUCK), Wuhan Tianhe (ZHHH), Qingdao Jiaodong (ZSQD). Each station carries ICAO code, display name, Open-Meteo coordinates, timezone, temperature profile, Wunderground resolution URL, and a Polymarket city slug used to generate the station-local current-date event URL. Profiles: `coastal_fast_lock` for ZSPD/ZSQD, `humid_south` for ZGGG, `north_inland` for ZBAA, `basin_inland` for ZUUU/ZUCK/ZHHH.
- `fetch_forecast_service.go` — `FetchDailySnapshot(ctx)`: calls Open-Meteo for the configured station coordinates and station-local current date. `NewFetchForecastService` keeps the default Shanghai behavior; `NewFetchForecastServiceForStation` supports all configured market stations. Hourly times parsed with `time.ParseInLocation` into the station timezone.
- `fetch_observation_service.go` — `FetchTodayObservations(ctx)`: calls Aviation Weather Center `METAR` for the configured ICAO station with a 36-hour lookback, filters reports to today's station-local date, converts wind from knots to km/h, skips rows with missing temperature, and filters out reports from other ICAO codes when present. Parses timestamp from `reportTime` (RFC3339) with fallback to `obsTime` (Unix epoch). Returns results sorted ascending by `ObservedAt`. Accepts an injected `now func() time.Time` for testability via `NewBuildableFetchObservationService`.
- `fetch_resolution_observation_service.go` — `FetchDailyHigh(ctx, station, targetDateLocal)`: appends `/date/YYYY-MM-DD` to the configured Wunderground source URL when needed, fetches the page via `wunderground.Client`, and returns a whole-degree Celsius daily high plus `SourceType` when available. `historical_observations` is the preferred settlement-aligned source; `current_observation_fallback` and `page_fallback` are diagnostics for degraded Wunderground parsing. `ApplyResolutionObservedHigh` records the Wunderground high separately and sets `ObservedHighSoFarC` to that settlement-aligned high; METAR remains the fallback when Wunderground is unavailable.
- `build_feature_summary_service.go` — `Build(ctx, stationCode, targetDate, now time.Time)` uses the default China timezone; production calls `BuildWithStationProfile(ctx, stationCode, targetDate, timezone, temperatureProfile, now)` with the configured station timezone/profile. Reads from `forecastStore` and `observationStore` interfaces (concrete repos satisfy them). Returns error if no forecast found; gracefully handles missing obs with nil fields. **Observed-so-far filtering**: only observations with `observed_at <= now` are used, so any future-dated rows are excluded. `computeTempChangeLast3h` uses a `[latest-3h, latest]` window (inclusive, ≥2 obs required). Computes `RemainingForecastHighC` from hourly forecast temps after the latest observation (or after `now` if no observations). Pass `time.Now()` in production; pass a fixed time in tests.
- `build_bucket_distribution_service.go` — `Build(summary)`: pure function, no DB, no error return. `summary` must not be nil — passing nil panics with an explicit message (programming error, not a runtime condition). Uses `summary.Timezone` and `summary.TemperatureProfile` for station-local, city-aware early calibration. Uses Wunderground `ResolutionObservedHighC` as the preferred settlement hard floor when present; otherwise falls back to METAR `ObservedHighSoFarC`. Profile config controls soft/hard lock hour, stable/hard-lock sigma, overshoot adjustment weight, strong-warming upside, and strong-warming cutoff hour. Fixed bucket range -20–50 °C with half-integer CDF midpoints; floor bucket is "-20C or below", ceiling is "50C or above". Confidence: 0.50 base + bonuses (max 0.90).
- `build_hermes_payload_service.go` — `Build(summary, dist)`: pure function, no DB. Validates that `summary` and `dist` share the same `StationCode` and `TargetDateLocal`; returns an error on mismatch or nil inputs. Produces a `hermes.HermesAnalysisPayload` with: (1) rounded numeric values (`round2` for temperatures/confidence, `round4` for probabilities); (2) Hermes-facing view types that omit redundant identity/timestamp fields; (3) `SanityFlags []string` computed by `computeSanityFlags`. **Sanity flag rules** (evaluated in order): `observed_high_exceeds_latest_forecast` when settlement-aligned `ObservedHighSoFarC` exceeds `LatestForecastHighC`; `missing_previous_forecast` when `PreviousForecastHighC == nil`; `no_observation_data` when no METAR observations and no Wunderground high are available; `limited_observation_coverage` when `0 < ObservationPoints < 6` and no Wunderground high is available.
- `build_analysis_service.go` — `Build(ctx, summary, dist)` / `BuildWithFallback(ctx, summary, dist)`: assembles the Hermes payload via `BuildHermesPayloadService`, then calls `hermes.Bridge.Analyze`. Hermes success returns the parsed `domain.AnalysisResult`; Hermes rate-limit or timeout returns a local deterministic `AnalysisResult` derived from `bucket_probs`, sanity flags, and summary features so the station still prints and persists an analysis.

### `internal/hermes/`
- `skill_payload.go` — defines the stable Hermes boundary types. **`HermesAnalysisPayload`**: top-level envelope with `StationCode`, `TargetDateLocal`, `GeneratedAt` (single timestamp, not repeated in nested views), `FeatureSummary`, `BucketDistribution`, `SanityFlags`. **`FeatureSummaryView`**: Hermes projection of `WeatherFeatureSummary` — omits `StationCode`, `TargetDateLocal`, `GeneratedAt`, `ForecastSnapshotFetchedAt`; all floats pre-rounded. **`BucketDistributionView`** + **`BucketProbView`**: Hermes projection of `TemperatureBucketDistribution` — omits identity and timestamp fields; all floats pre-rounded. **`MarshalPayload(p)`** returns compact JSON `([]byte, error)`. **`MustPrettyJSON(p)`** returns 2-space-indented JSON string (panics on marshal failure).
- `bridge.go` — `Bridge`: calls `hermes chat --toolsets skills -q <prompt>` via `exec.CommandContext`. Payload JSON is embedded inline in the prompt (no temp file). `parseOutput` first detects Hermes rate-limit output and returns `ErrRateLimited`; otherwise it calls `extractLastAnalysisResult` to locate the last JSON object in stdout that matches the analysis-result schema (tolerates banner text, query echo, UI chrome, and unrelated JSON objects which are skipped via `errUnrelatedJSONObject`). `extractLastJSONObject` is the lower-level primitive: scans right-to-left for `}`, forward-scans for the matching `{` via `findObjectEnd` (handles quoted strings and `\"` escapes), and if the brace pair is found but fails `json.Valid` returns an error immediately rather than silently falling back. `canonicalizeAnalysisResultObject` normalises keys via `normalizeToken` (strips non-alphanumeric, lowercases) so minor CLI variations in key naming are tolerated. `normalizeAnalysisResult` canonicalises risk flag labels against `normalizedRiskFlags`. `validateAnalysisResult` enforces: non-empty `predicted_best_bucket`, confidence ∈ [0,1], 2–4 non-empty `key_reasons`, non-empty `risk_flags` strings, `next_check_in_minutes > 0`. `NewBridgeWithBin(bin)` allows binary path injection for testing.

### `internal/providers/`
- `openmeteo/client.go` — `Client` with functional options (`WithTimeout`, `WithBaseURL`, `WithHTTPClient`). Default 10 s timeout. Non-200 errors include path + body truncated to 256 bytes.
- `openmeteo/forecast.go` — `Forecast(ctx, ForecastParams)`. `Var*` / unit / timezone constants. Pre-flight validation (coord ranges, ForecastDays 1–16, PastDays 0–92, enum units). Post-decode slice alignment check. 17 tests via `httptest.NewServer`.
- `aviationweather/client.go` — same option pattern as openmeteo; no auth required. Handles HTTP 204 as empty data (returns nil body, not an error).
- `aviationweather/metar.go` — `METAR(ctx, METARParams)`. Builds `/api/data/metar?ids=...&format=json&hours=...`, validates IDs (non-empty) and lookback hours (>0), decodes JSON array into `[]METARReport`. `METARReport` exposes `ICAOID`, `ReceiptTime`, `ObsTime` (Unix epoch int64), `ReportTime` (RFC3339 string), `Temp`, `Dewp`, `Wspd` (all nullable `*float64`).
- `wunderground/client.go` — fetches Wunderground history pages and extracts current/final daily high in whole degrees Celsius. It first derives Weather.com's historical observations API URL from the Wunderground page and takes the maximum hourly historical `temp` for the station/date; if that is unavailable it falls back to current/page patterns. Explicit Fahrenheit values are converted to Celsius; unavailable pages return `ok=false` so callers can fall back to METAR/Open-Meteo.

### `internal/repository/`
- `db.go` — `NewPostgresPool(ctx, dsn)`: creates pool, pings before returning.
- `forecast_snapshots_repo.go` — `Insert` (SHA-256 content_hash dedup), `GetLatestForDate`, `GetPreviousForDate` (returns `ForecastRow`; fetches `jsonb_array_length` for all six hourly arrays and returns an error if lengths diverge, and deserializes hourly time/temp arrays for remaining-forecast-high calculation).
- `observation_snapshots_repo.go` — `Insert` (unique on `(station_code, observed_at)`), `ListForDate(ctx, stationCode, targetDate, loc)` (UTC range query, ASC order). Returns ALL stored rows for the target local date; the observed-so-far cutoff is applied upstream in `BuildFeatureSummaryService`, not here.
- `analysis_results_repo.go` — `AnalysisResultsRepo.Insert(ctx, rec)`: serialises JSONB fields once (reused for both hash and DB insert), computes `analysis_content_hash` (SHA-256 of all content fields excluding `generated_at`/`created_at`/`id`), executes `INSERT ... ON CONFLICT DO NOTHING`, returns `inserted bool`. The hash struct `analysisContentHashInput` uses `json.RawMessage` for the three JSONB blobs so they are included verbatim in the digest. **Hash exclusions** (LLM fields that vary between runs for identical inputs): `confidence` (LLM "adjusts" the pipeline value, e.g. 0.75 vs 0.80), `key_reasons_json` (free-text prose, wording varies), `feature_summary_json`/`bucket_distribution_json` (contain volatile `generated_at`; weather content is already inside `hermes_payload_json`). **`hermes_payload_json`** is included with its own `generated_at` stripped via `stripGeneratedAt` (unmarshal → delete key → re-marshal with sorted keys). Stable hash inputs: `station_code`, `target_date_local`, `predicted_best_bucket`, `secondary_risk_bucket`, `risk_flags_json`, `next_check_in_minutes`, `hermes_payload_json` (stripped).

### `migrations/`
- `001_forecast_snapshots.up.sql` — `forecast_snapshots` table: unique index on `(station_code, target_date_local, content_hash)`.
- `002_observation_snapshots.up.sql` — `observation_snapshots` table: `observed_at TIMESTAMPTZ NOT NULL`; unique index on `(station_code, observed_at)`.
- `003_analysis_results.up.sql` — `analysis_results` table: all scalar analysis fields + four JSONB columns (`key_reasons_json`, `risk_flags_json`, `feature_summary_json`, `bucket_distribution_json`, `hermes_payload_json`); unique index on `(station_code, target_date_local, analysis_content_hash)`; plain indexes on `(station_code, target_date_local)` and `(generated_at DESC)`. Down migration drops the table.
- Applied manually via `docker exec psql`. No migration runner yet.

### `infra/`
PostgreSQL via `docker-compose.postgres.yml`. Credentials in `.env`. Volume at `/var/lib/postgresql/data`.

### `hermes-skills/highest-temp-analysis/SKILL.md`
Local Hermes skill. Takes a `HermesAnalysisPayload` JSON as input and returns a strict 6-field JSON object: `predicted_best_bucket`, `secondary_risk_bucket` (string or null), `confidence`, `key_reasons` (2–4 strings), `risk_flags` (string array), `next_check_in_minutes` (integer, prefer 30/60/90). Output contract is strict: raw JSON only, no markdown, no extra keys. METAR/Wunderground-aware: accounts for >24 observation points per day, `:00`/`:30` timestamps, Wunderground resolution highs, 71 one-degree buckets, and `observed_high_so_far_c` as a hard floor. Station-agnostic: the `station_code` in the payload identifies which configured airport market is being analysed. Version 1.4.3 uses the late-evening historical lock from 21:30 local onward: historical Wunderground source + top bucket ≥0.85 + non-warming trend should return `secondary_risk_bucket: null` and `next_check_in_minutes: 90` unless a live risk is named; top bucket ≥0.90 should generally suppress weak secondary buckets.

## Implementation status

| Step | Description | Status |
|------|-------------|--------|
| 1 | Forecast ingestion (Open-Meteo → DB) | ✅ done |
| 2 | Observation ingestion (METAR → DB) | ✅ done |
| 3 | Feature summary (`WeatherFeatureSummary`) | ✅ done |
| 4 | Bucket distribution (`TemperatureBucketDistribution`) | ✅ done |
| 5 | Hermes payload assembly (`HermesAnalysisPayload`) | ✅ done |
| 6.1 | Hermes skill (`highest-temp-analysis`) — local install + contract | ✅ done |
| 6.2 | Hermes bridge (Go → CLI → `domain.AnalysisResult`) | ✅ done |
| 7 | Persistence of analysis results (`analysis_results` table) | ✅ done |
| 8 | Scheduled/periodic execution | 🔜 next |
