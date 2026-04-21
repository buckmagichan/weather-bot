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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Local date used by both the ingestion and summary steps.
	shanghaiLoc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		log.Fatalf("load timezone: %v", err)
	}
	today := time.Now().In(shanghaiLoc).Format("2006-01-02")

	// --- Forecast ---
	forecastClient := openmeteo.NewClient()
	forecastSvc, err := services.NewFetchForecastService(forecastClient)
	if err != nil {
		log.Fatalf("init forecast service: %v", err)
	}
	snap, err := forecastSvc.FetchDailySnapshot(ctx)
	if err != nil {
		log.Fatalf("fetch forecast: %v", err)
	}
	forecastRepo := repository.NewForecastSnapshotRepo(pool)
	forecastInserted, err := forecastRepo.Insert(ctx, snap)
	if err != nil {
		log.Fatalf("insert forecast: %v", err)
	}
	if forecastInserted {
		log.Printf("forecast saved (%s  %.1f C)", snap.TargetDateLocal, snap.ForecastHighC)
	}

	// --- Observations ---
	obsClient := aviationweather.NewClient()
	obsSvc, err := services.NewFetchObservationService(obsClient)
	if err != nil {
		log.Fatalf("init observation service: %v", err)
	}
	// Observation fetch gets its own budget because upstream METAR latency can
	// be bursty and should not consume the forecast/DB time budget.
	obsCtx, obsCancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer obsCancel()
	observations, err := obsSvc.FetchTodayObservations(obsCtx)
	if err != nil {
		log.Printf("fetch observations: %v — continuing without new observations", err)
		observations = nil
	}
	obsRepo := repository.NewObservationSnapshotRepo(pool)
	for i := range observations {
		if _, err := obsRepo.Insert(ctx, &observations[i]); err != nil {
			log.Fatalf("insert observation: %v", err)
		}
	}

	// --- Feature Summary ---
	summarySvc, err := services.NewBuildFeatureSummaryService(forecastRepo, obsRepo)
	if err != nil {
		log.Fatalf("init build feature summary service: %v", err)
	}
	summary, err := summarySvc.Build(ctx, "ZSPD", today, time.Now())
	if err != nil {
		log.Fatalf("build feature summary: %v", err)
	}

	// --- Bucket Distribution ---
	bucketSvc := services.NewBuildBucketDistributionService()
	dist := bucketSvc.Build(summary)

	// --- Hermes Payload ---
	hermesSvc := services.NewBuildHermesPayloadService()
	payload, err := hermesSvc.Build(summary, dist)
	if err != nil {
		log.Fatalf("build hermes payload: %v", err)
	}

	// --- Hermes Analysis ---
	// Use a dedicated context: LLM inference takes longer than the 15 s DB budget.
	hermesCtx, hermesCancel := context.WithTimeout(context.Background(), buildHermesTimeout())
	defer hermesCancel()

	bridge := hermes.NewBridge()
	analysisSvc := services.NewBuildAnalysisService(bridge)
	analysis, err := analysisSvc.Build(hermesCtx, summary, dist)
	if err != nil {
		log.Printf("hermes analysis: %v — skipping", err)
		return
	}
	fmt.Println("\n--- Hermes Analysis ---")
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
		log.Printf("marshal hermes payload for persistence: %v — skipping", err)
		return
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
	analysisRepo := repository.NewAnalysisResultsRepo(pool)
	analysisInserted, err := analysisRepo.Insert(persistCtx, rec)
	if err != nil {
		log.Printf("persist analysis: %v — skipping", err)
		return
	}
	if analysisInserted {
		fmt.Println("Analysis:            saved to postgres")
	} else {
		fmt.Println("Analysis:            already in postgres (duplicate)")
	}
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
