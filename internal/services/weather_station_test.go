package services

import (
	"strings"
	"testing"
)

func TestMarketWeatherStations(t *testing.T) {
	stations := MarketWeatherStations()
	want := map[string]WeatherStation{
		"ZSPD": {
			Latitude:  31.1434,
			Longitude: 121.8050,
		},
		"ZUCK": {
			Latitude:  29.7192,
			Longitude: 106.6417,
		},
		"ZBAA": {
			Latitude:  40.0801,
			Longitude: 116.5850,
		},
		"ZGGG": {
			Latitude:  23.3924,
			Longitude: 113.2990,
		},
		"ZUUU": {
			Latitude:  30.5785,
			Longitude: 103.9470,
		},
		"ZHHH": {
			Latitude:  30.7838,
			Longitude: 114.2081,
		},
		"ZSQD": {
			Latitude:  36.3620,
			Longitude: 120.0882,
		},
	}

	if len(stations) != len(want) {
		t.Fatalf("got %d stations, want %d", len(stations), len(want))
	}
	for _, station := range stations {
		expected, ok := want[station.Code]
		if !ok {
			t.Fatalf("unexpected station code %q", station.Code)
		}
		if station.Latitude != expected.Latitude || station.Longitude != expected.Longitude {
			t.Fatalf("%s coordinates: got %.4f, %.4f; want %.4f, %.4f",
				station.Code,
				station.Latitude,
				station.Longitude,
				expected.Latitude,
				expected.Longitude,
			)
		}
		if station.Timezone != chinaTimezone {
			t.Fatalf("%s timezone: got %q, want %q", station.Code, station.Timezone, chinaTimezone)
		}
		if station.TemperatureProfile == "" {
			t.Fatalf("%s TemperatureProfile is empty", station.Code)
		}
		if station.ResolutionSourceURL == "" {
			t.Fatalf("%s ResolutionSourceURL is empty", station.Code)
		}
		if station.Name == "" {
			t.Fatalf("%s Name is empty", station.Code)
		}
		if station.PolymarketCitySlug == "" {
			t.Fatalf("%s PolymarketCitySlug is empty", station.Code)
		}
		may5URL, err := station.PolymarketEventURL("2026-05-05")
		if err != nil {
			t.Fatalf("%s PolymarketEventURL May 5: %v", station.Code, err)
		}
		if !strings.Contains(may5URL, "may-5-2026") {
			t.Fatalf("%s PolymarketEventURL May 5: got %q", station.Code, may5URL)
		}
		may6URL, err := station.PolymarketEventURL("2026-05-06")
		if err != nil {
			t.Fatalf("%s PolymarketEventURL May 6: %v", station.Code, err)
		}
		if !strings.Contains(may6URL, "may-6-2026") {
			t.Fatalf("%s PolymarketEventURL May 6: got %q", station.Code, may6URL)
		}
	}
}

func TestMarketWeatherStations_TemperatureProfiles(t *testing.T) {
	want := map[string]string{
		"ZBAA": TemperatureProfileNorthInland,
		"ZSPD": TemperatureProfileCoastalFastLock,
		"ZGGG": TemperatureProfileHumidSouth,
		"ZUUU": TemperatureProfileBasinInland,
		"ZUCK": TemperatureProfileBasinInland,
		"ZHHH": TemperatureProfileBasinInland,
		"ZSQD": TemperatureProfileCoastalFastLock,
	}

	for _, station := range MarketWeatherStations() {
		if got := station.TemperatureProfile; got != want[station.Code] {
			t.Fatalf("%s TemperatureProfile: got %q, want %q", station.Code, got, want[station.Code])
		}
	}
}

func TestMarketWeatherStations_Order(t *testing.T) {
	stations := MarketWeatherStations()
	want := []string{"ZBAA", "ZSPD", "ZGGG", "ZUUU", "ZUCK", "ZHHH", "ZSQD"}

	if len(stations) != len(want) {
		t.Fatalf("got %d stations, want %d", len(stations), len(want))
	}
	for i, station := range stations {
		if station.Code != want[i] {
			t.Fatalf("station[%d]: got %s, want %s", i, station.Code, want[i])
		}
	}
}

func TestMarketWeatherStations_ReturnsFreshSlice(t *testing.T) {
	stations := MarketWeatherStations()
	if len(stations) == 0 {
		t.Fatal("MarketWeatherStations returned no stations")
	}
	stations[0].Code = "MUTATED"

	if got := MarketWeatherStations()[0].Code; got == "MUTATED" {
		t.Fatal("MarketWeatherStations returned mutable backing slice")
	}
}

func TestDefaultWeatherStation_RemainsShanghai(t *testing.T) {
	if got := DefaultWeatherStation().Code; got != "ZSPD" {
		t.Fatalf("DefaultWeatherStation: got %s, want ZSPD", got)
	}
}
