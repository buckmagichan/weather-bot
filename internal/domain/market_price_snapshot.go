package domain

import "time"

// MarketPriceSnapshot is one point-in-time view of a Polymarket temperature
// bucket. Gamma provides event/market metadata and fallback prices; CLOB
// prices are filled when an order book is available.
type MarketPriceSnapshot struct {
	StationCode     string    `json:"station_code"`
	TargetDateLocal string    `json:"target_date_local"`
	CapturedAt      time.Time `json:"captured_at"`

	EventSlug string `json:"event_slug"`
	EventID   string `json:"event_id"`

	MarketID    string `json:"market_id"`
	MarketSlug  string `json:"market_slug"`
	BucketLabel string `json:"bucket_label"`
	YesTokenID  string `json:"yes_token_id"`
	NoTokenID   string `json:"no_token_id"`

	GammaYesPrice  *float64 `json:"gamma_yes_price"`
	GammaNoPrice   *float64 `json:"gamma_no_price"`
	BestBid        *float64 `json:"best_bid"`
	BestAsk        *float64 `json:"best_ask"`
	Midpoint       *float64 `json:"midpoint"`
	Spread         *float64 `json:"spread"`
	LastTradePrice *float64 `json:"last_trade_price"`

	Volume    *float64 `json:"volume"`
	Liquidity *float64 `json:"liquidity"`
	Active    bool     `json:"active"`
	Closed    bool     `json:"closed"`

	PredictedBestBucket    string   `json:"predicted_best_bucket"`
	SecondaryRiskBucket    *string  `json:"secondary_risk_bucket"`
	AnalysisConfidence     *float64 `json:"analysis_confidence"`
	ModelBucketProbability *float64 `json:"model_bucket_probability"`
	DistributionConfidence *float64 `json:"distribution_confidence"`
	ExpectedHighC          *float64 `json:"expected_high_c"`
	ObservedHighSoFarC     *float64 `json:"observed_high_so_far_c"`
	LatestObservedTempC    *float64 `json:"latest_observed_temp_c"`
	LatestForecastHighC    *float64 `json:"latest_forecast_high_c"`
	RemainingForecastHighC *float64 `json:"remaining_forecast_high_c"`
	TempChangeLast3hC      *float64 `json:"temp_change_last_3h_c"`
	ObservationPoints      int      `json:"observation_points"`
}
