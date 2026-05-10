package polymarket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchEventPriceSnapshots(t *testing.T) {
	var sawPrices bool
	var sawMidpoints bool
	var sawSpreads bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/events":
			if got := r.URL.Query().Get("slug"); got != "highest-temperature-in-shanghai-on-may-8-2026" {
				t.Fatalf("slug query = %q", got)
			}
			_, _ = w.Write([]byte(`[{
				"id": "event-1",
				"slug": "highest-temperature-in-shanghai-on-may-8-2026",
				"markets": [{
					"id": "market-24",
					"question": "Shanghai 24C",
					"slug": "shanghai-24c",
					"groupItemTitle": "24°C",
					"outcomePrices": "[\"0.73\",\"0.27\"]",
					"clobTokenIds": "[\"yes-24\",\"no-24\"]",
					"bestBid": "0.70",
					"bestAsk": "0.76",
					"lastTradePrice": "0.72",
					"volume": "123.4",
					"liquidity": "456.7",
					"active": true,
					"closed": false
				}]
			}]`))
		case "/prices":
			sawPrices = true
			var req []clobPriceRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode prices request: %v", err)
			}
			if len(req) != 2 || req[0].TokenID != "yes-24" || req[0].Side != "BUY" || req[1].Side != "SELL" {
				t.Fatalf("unexpected prices request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"yes-24":{"BUY":"0.71","SELL":"0.75"}}`))
		case "/midpoints":
			sawMidpoints = true
			_, _ = w.Write([]byte(`{"yes-24":"0.73"}`))
		case "/spreads":
			sawSpreads = true
			_, _ = w.Write([]byte(`{"yes-24":"0.04"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(
		WithGammaBaseURL(server.URL),
		WithCLOBBaseURL(server.URL),
	)
	capturedAt := time.Date(2026, 5, 8, 10, 30, 0, 0, time.FixedZone("CST", 8*3600))

	snaps, err := client.FetchEventPriceSnapshots(
		context.Background(),
		"ZSPD",
		"2026-05-08",
		"highest-temperature-in-shanghai-on-may-8-2026",
		capturedAt,
	)
	if err != nil {
		t.Fatalf("FetchEventPriceSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d", len(snaps))
	}
	if !sawPrices || !sawMidpoints || !sawSpreads {
		t.Fatalf("expected all CLOB endpoints to be called; prices=%v midpoints=%v spreads=%v", sawPrices, sawMidpoints, sawSpreads)
	}

	snap := snaps[0]
	if snap.StationCode != "ZSPD" || snap.TargetDateLocal != "2026-05-08" {
		t.Fatalf("station/date = %s/%s", snap.StationCode, snap.TargetDateLocal)
	}
	if snap.EventID != "event-1" || snap.MarketID != "market-24" || snap.MarketSlug != "shanghai-24c" {
		t.Fatalf("event/market identity not parsed: %#v", snap)
	}
	if snap.BucketLabel != "24C" {
		t.Fatalf("BucketLabel = %q", snap.BucketLabel)
	}
	if snap.YesTokenID != "yes-24" || snap.NoTokenID != "no-24" {
		t.Fatalf("token IDs = %q/%q", snap.YesTokenID, snap.NoTokenID)
	}
	assertFloatPtr(t, "GammaYesPrice", snap.GammaYesPrice, 0.73)
	assertFloatPtr(t, "GammaNoPrice", snap.GammaNoPrice, 0.27)
	assertFloatPtr(t, "BestBid", snap.BestBid, 0.71)
	assertFloatPtr(t, "BestAsk", snap.BestAsk, 0.75)
	assertFloatPtr(t, "Midpoint", snap.Midpoint, 0.73)
	assertFloatPtr(t, "Spread", snap.Spread, 0.04)
	assertFloatPtr(t, "LastTradePrice", snap.LastTradePrice, 0.72)
	assertFloatPtr(t, "Volume", snap.Volume, 123.4)
	assertFloatPtr(t, "Liquidity", snap.Liquidity, 456.7)
	if !snap.Active || snap.Closed {
		t.Fatalf("active/closed = %v/%v", snap.Active, snap.Closed)
	}
	if snap.CapturedAt.Location() != time.UTC {
		t.Fatalf("CapturedAt location = %s, want UTC", snap.CapturedAt.Location())
	}
}

func TestFetchEventPriceSnapshotsKeepsGammaDataWhenCLOBFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		if r.URL.Path != "/events" {
			http.Error(w, `{"error":"no orderbook"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{
			"id": 11,
			"slug": "slug",
			"markets": [{
				"id": 22,
				"question": "Shanghai 25C",
				"slug": "shanghai-25c",
				"groupItemTitle": "25°C",
				"outcomePrices": ["0.25","0.75"],
				"clobTokenIds": ["yes-25","no-25"],
				"active": true,
				"closed": false
			}]
		}]`))
	}))
	defer server.Close()

	client := NewClient(WithGammaBaseURL(server.URL), WithCLOBBaseURL(server.URL))
	snaps, err := client.FetchEventPriceSnapshots(context.Background(), "ZSPD", "2026-05-08", "slug", time.Now())
	if err != nil {
		t.Fatalf("FetchEventPriceSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d", len(snaps))
	}
	if snaps[0].EventID != "11" || snaps[0].MarketID != "22" {
		t.Fatalf("numeric IDs were not stringified: %#v", snaps[0])
	}
	assertFloatPtr(t, "GammaYesPrice", snaps[0].GammaYesPrice, 0.25)
	if snaps[0].BestBid != nil || snaps[0].Midpoint != nil || snaps[0].Spread != nil {
		t.Fatalf("CLOB fields should stay nil on CLOB failure: %#v", snaps[0])
	}
}

func TestFetchEventPriceSnapshotsRequiresExactEventSlug(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`[{
			"id": "wrong-event",
			"slug": "highest-temperature-in-shanghai-on-may-9-2026",
			"markets": []
		}]`))
	}))
	defer server.Close()

	client := NewClient(WithGammaBaseURL(server.URL), WithCLOBBaseURL(server.URL))
	_, err := client.FetchEventPriceSnapshots(
		context.Background(),
		"ZSPD",
		"2026-05-08",
		"highest-temperature-in-shanghai-on-may-8-2026",
		time.Now(),
	)
	if !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("FetchEventPriceSnapshots err = %v, want ErrEventNotFound", err)
	}
}

func TestNullableFloatRejectsMalformedNonEmptyValue(t *testing.T) {
	var value nullableFloat
	if err := json.Unmarshal([]byte(`"not-a-price"`), &value); err == nil {
		t.Fatal("expected malformed non-empty nullable float to return an error")
	}

	if err := json.Unmarshal([]byte(`""`), &value); err != nil {
		t.Fatalf("empty string should decode as nil nullable float: %v", err)
	}
	if value.Set {
		t.Fatal("empty string should not set nullable float")
	}
}

func TestNormalizeBucketLabelHandlesCelsiusVariants(t *testing.T) {
	tests := map[string]string{
		"24°C":  "24C",
		"24 °C": "24C",
		"24℃":   "24C",
	}
	for input, want := range tests {
		if got := normalizeBucketLabel(input); got != want {
			t.Fatalf("normalizeBucketLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func assertFloatPtr(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s is nil", name)
	}
	if *got != want {
		t.Fatalf("%s = %v, want %v", name, *got, want)
	}
}
