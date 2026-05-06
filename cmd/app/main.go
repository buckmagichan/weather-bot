package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"

	"github.com/buckmagichan/weather-bot/internal/domain"
	"github.com/buckmagichan/weather-bot/internal/hermes"
	"github.com/buckmagichan/weather-bot/internal/providers/aviationweather"
	"github.com/buckmagichan/weather-bot/internal/providers/openmeteo"
	"github.com/buckmagichan/weather-bot/internal/providers/wunderground"
	"github.com/buckmagichan/weather-bot/internal/repository"
	"github.com/buckmagichan/weather-bot/internal/services"
)

func main() {
	// Load .env if present. Silently ignored when absent (e.g. in production).
	_ = godotenv.Load()

	dsn := buildDSN()
	if dsn == "" {
		log.Fatal("database config missing: set DATABASE_URL, or POSTGRES_USER + POSTGRES_PASSWORD + POSTGRES_DB")
	}

	// Separate context for pool creation so a slow Docker start does not eat
	// into the time budget for the business-logic calls.
	poolCtx, poolCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer poolCancel()

	pool, err := repository.NewPostgresPool(poolCtx, dsn)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	forecastClient := openmeteo.NewClient()
	forecastRepo := repository.NewForecastSnapshotRepo(pool)
	obsClient := aviationweather.NewClient()
	obsRepo := repository.NewObservationSnapshotRepo(pool)
	resolutionClient := wunderground.NewClient()
	resolutionSvc := services.NewFetchResolutionObservationService(resolutionClient)
	summarySvc, err := services.NewBuildFeatureSummaryService(forecastRepo, obsRepo)
	if err != nil {
		log.Fatalf("init build feature summary service: %v", err)
	}
	bucketSvc := services.NewBuildBucketDistributionService()
	hermesSvc := services.NewBuildHermesPayloadService()
	bridge := hermes.NewBridge()
	analysisSvc := services.NewBuildAnalysisService(bridge)
	analysisRepo := repository.NewAnalysisResultsRepo(pool)
	pipeline := &stationPipeline{
		forecastClient: forecastClient,
		forecastRepo:   forecastRepo,
		obsClient:      obsClient,
		obsRepo:        obsRepo,
		resolutionSvc:  resolutionSvc,
		summarySvc:     summarySvc,
		bucketSvc:      bucketSvc,
		hermesSvc:      hermesSvc,
		analysisSvc:    analysisSvc,
		analysisRepo:   analysisRepo,
	}

	completed := 0
	for _, station := range services.MarketWeatherStations() {
		if err := pipeline.runMarketForStation(station); err != nil {
			log.Printf("%s: %v", station.Label(), err)
			continue
		}
		completed++
	}

	if completed == 0 {
		log.Fatal("no station market completed")
	}
}

type stationPipeline struct {
	forecastClient *openmeteo.Client
	forecastRepo   *repository.ForecastSnapshotRepo
	obsClient      *aviationweather.Client
	obsRepo        *repository.ObservationSnapshotRepo
	resolutionSvc  *services.FetchResolutionObservationService
	summarySvc     *services.BuildFeatureSummaryService
	bucketSvc      *services.BuildBucketDistributionService
	hermesSvc      *services.BuildHermesPayloadService
	analysisSvc    *services.BuildAnalysisService
	analysisRepo   *repository.AnalysisResultsRepo
}

func (p *stationPipeline) runMarketForStation(station services.WeatherStation) error {
	loc, err := station.Location()
	if err != nil {
		return err
	}
	today := time.Now().In(loc).Format("2006-01-02")

	fmt.Printf("\n=== %s — %s ===\n", station.Label(), today)
	marketURL, err := station.PolymarketEventURL(today)
	if err != nil {
		log.Printf("%s polymarket URL: %v", station.Code, err)
	} else if marketURL != "" {
		fmt.Printf("Polymarket:          %s\n", marketURL)
	}

	// --- Forecast ---
	forecastSvc, err := services.NewFetchForecastServiceForStation(p.forecastClient, station)
	if err != nil {
		return fmt.Errorf("init forecast service: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snap, err := forecastSvc.FetchDailySnapshot(ctx)
	if err != nil {
		return fmt.Errorf("fetch forecast: %w", err)
	}
	forecastInserted, err := p.forecastRepo.Insert(ctx, snap)
	if err != nil {
		return fmt.Errorf("insert forecast: %w", err)
	}
	if forecastInserted {
		log.Printf("%s forecast saved (%s  %.1f C)", station.Code, snap.TargetDateLocal, snap.ForecastHighC)
	}

	// --- Observations ---
	obsSvc, err := services.NewFetchObservationServiceForStation(p.obsClient, station)
	if err != nil {
		return fmt.Errorf("init observation service: %w", err)
	}
	// Observation fetch gets its own budget because upstream METAR latency can
	// be bursty and should not consume the forecast/DB time budget.
	obsCtx, obsCancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer obsCancel()
	observations, err := obsSvc.FetchTodayObservations(obsCtx)
	if err != nil {
		log.Printf("%s fetch observations: %v — continuing without new observations", station.Code, err)
		observations = nil
	}
	insertObsCtx, insertObsCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer insertObsCancel()
	for i := range observations {
		if _, err := p.obsRepo.Insert(insertObsCtx, &observations[i]); err != nil {
			log.Printf("%s insert observation at %s: %v — continuing",
				station.Code,
				observations[i].ObservedAt.Format(time.RFC3339),
				err,
			)
		}
	}

	// --- Feature Summary ---
	summaryCtx, summaryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer summaryCancel()
	summary, err := p.summarySvc.BuildWithStationProfile(
		summaryCtx,
		station.Code,
		snap.TargetDateLocal,
		station.Timezone,
		station.TemperatureProfile,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("build feature summary: %w", err)
	}

	// --- Resolution Observation ---
	// Wunderground is the Polymarket settlement source. Treat it as the
	// preferred observed high when available, but keep the pipeline alive on
	// fetch/parsing failures and fall back to METAR-derived observations.
	resolutionCtx, resolutionCancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer resolutionCancel()
	resolutionObs, found, err := p.resolutionSvc.FetchDailyHigh(resolutionCtx, station, snap.TargetDateLocal)
	if err != nil {
		log.Printf("%s fetch resolution high: %v — falling back to METAR high", station.Code, err)
	} else if found {
		services.ApplyResolutionObservedHigh(summary, resolutionObs)
		log.Printf(
			"%s resolution high from Wunderground [%s] (%s  %d C)",
			station.Code,
			resolutionObs.SourceType,
			snap.TargetDateLocal,
			resolutionObs.HighC,
		)
	}

	// --- Bucket Distribution ---
	dist := p.bucketSvc.Build(summary)

	// --- Hermes Payload ---
	payload, err := p.hermesSvc.Build(summary, dist)
	if err != nil {
		return fmt.Errorf("build hermes payload: %w", err)
	}

	// --- Hermes Analysis ---
	// Use a dedicated context: LLM inference takes longer than the DB budget.
	hermesCtx, hermesCancel := context.WithTimeout(context.Background(), buildHermesTimeout())
	defer hermesCancel()
	analysis, analysisSource, err := p.analysisSvc.BuildWithFallback(hermesCtx, summary, dist)
	if err != nil {
		log.Printf("%s hermes analysis: %v — skipping", station.Code, err)
		return nil
	}
	if analysisSource == services.AnalysisSourceLocalFallback {
		log.Printf("%s hermes unavailable/rate-limited — using local deterministic analysis", station.Code)
	}
	if analysisSource == services.AnalysisSourceLocalFallback {
		fmt.Println("\n--- Local Analysis ---")
	} else {
		fmt.Println("\n--- Hermes Analysis ---")
	}
	fmt.Printf("Best bucket:         %s\n", analysis.PredictedBestBucket)
	if analysis.SecondaryRiskBucket != nil {
		fmt.Printf("Secondary risk:      %s\n", *analysis.SecondaryRiskBucket)
	} else {
		fmt.Printf("Secondary risk:      —\n")
	}
	fmt.Printf("Confidence:          %.2f\n", analysis.Confidence)
	fmt.Println("Key reasons:")
	for _, r := range analysis.KeyReasons {
		fmt.Printf("- %s\n", r)
	}
	if len(analysis.RiskFlags) > 0 {
		fmt.Println("Risk flags:")
		for _, f := range analysis.RiskFlags {
			fmt.Printf("- %s\n", f)
		}
	} else {
		fmt.Println("Risk flags:          (none)")
	}
	fmt.Printf("Next check in:       %d minutes\n", analysis.NextCheckInMinutes)

	// --- Persist Analysis ---
	payloadBytes, err := hermes.MarshalPayload(payload)
	if err != nil {
		log.Printf("%s marshal hermes payload for persistence: %v — skipping", station.Code, err)
		return nil
	}
	rec := &domain.AnalysisPersistenceRecord{
		StationCode:       summary.StationCode,
		TargetDateLocal:   summary.TargetDateLocal,
		GeneratedAt:       time.Now().UTC(),
		Analysis:          analysis,
		Summary:           summary,
		Distribution:      dist,
		HermesPayloadJSON: json.RawMessage(payloadBytes),
	}
	persistCtx, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer persistCancel()
	analysisInserted, err := p.analysisRepo.Insert(persistCtx, rec)
	if err != nil {
		log.Printf("%s persist analysis: %v — skipping", station.Code, err)
		return nil
	}
	if analysisInserted {
		fmt.Println("Analysis:            saved to postgres")
	} else {
		fmt.Println("Analysis:            already in postgres (duplicate)")
	}
	return nil
}

func buildHermesTimeout() time.Duration {
	const defaultHermesTimeout = 3 * time.Minute

	seconds := os.Getenv("HERMES_TIMEOUT_SECONDS")
	if seconds == "" {
		return defaultHermesTimeout
	}

	n, err := strconv.Atoi(seconds)
	if err != nil || n <= 0 {
		log.Printf("invalid HERMES_TIMEOUT_SECONDS=%q; using default %s", seconds, defaultHermesTimeout)
		return defaultHermesTimeout
	}
	return time.Duration(n) * time.Second
}

func buildDSN() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	user := os.Getenv("POSTGRES_USER")
	pass := os.Getenv("POSTGRES_PASSWORD")
	dbname := os.Getenv("POSTGRES_DB")
	if user == "" || dbname == "" {
		return ""
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	sslMode := os.Getenv("POSTGRES_SSL_MODE")
	if sslMode == "" {
		sslMode = "disable"
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, pass),
		Host:     host + ":" + port,
		Path:     dbname,
		RawQuery: "sslmode=" + url.QueryEscape(sslMode),
	}
	return u.String()
}
