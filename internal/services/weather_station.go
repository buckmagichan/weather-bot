package services

import (
	"fmt"
	"strings"
	"time"
)

const chinaTimezone = "Asia/Shanghai"

const (
	TemperatureProfileCoastalFastLock = "coastal_fast_lock"
	TemperatureProfileHumidSouth      = "humid_south"
	TemperatureProfileNorthInland     = "north_inland"
	TemperatureProfileBasinInland     = "basin_inland"
)

// WeatherStation describes a market settlement station and the coordinates
// used for model forecasts around that station.
type WeatherStation struct {
	Code                string
	Name                string
	Latitude            float64
	Longitude           float64
	Timezone            string
	TemperatureProfile  string
	ResolutionSourceURL string
	PolymarketCitySlug  string
}

func (s WeatherStation) Location() (*time.Location, error) {
	tz := s.Timezone
	if tz == "" {
		tz = chinaTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("load timezone %s for %s: %w", tz, s.Code, err)
	}
	return loc, nil
}

func (s WeatherStation) Label() string {
	if s.Name == "" {
		return s.Code
	}
	return fmt.Sprintf("%s (%s)", s.Name, s.Code)
}

func (s WeatherStation) PolymarketEventURL(targetDateLocal string) (string, error) {
	eventSlug, err := s.PolymarketEventSlug(targetDateLocal)
	if err != nil {
		return "", err
	}
	if eventSlug == "" {
		return "", nil
	}
	return fmt.Sprintf("https://polymarket.com/event/%s", eventSlug), nil
}

func (s WeatherStation) PolymarketEventSlug(targetDateLocal string) (string, error) {
	if s.PolymarketCitySlug == "" {
		return "", nil
	}
	d, err := time.Parse("2006-01-02", targetDateLocal)
	if err != nil {
		return "", fmt.Errorf("build polymarket event slug for %s: parse date %q: %w", s.Code, targetDateLocal, err)
	}
	dateSlug := strings.ToLower(d.Format("January-2-2006"))
	return fmt.Sprintf("highest-temperature-in-%s-on-%s", s.PolymarketCitySlug, dateSlug), nil
}

func DefaultWeatherStation() WeatherStation {
	for _, station := range marketStations {
		if station.Code == "ZSPD" {
			return station
		}
	}
	panic("DefaultWeatherStation: ZSPD missing from marketStations")
}

func MarketWeatherStations() []WeatherStation {
	stations := make([]WeatherStation, len(marketStations))
	copy(stations, marketStations)
	return stations
}

var marketStations = []WeatherStation{
	{
		Code:                "ZBAA",
		Name:                "Beijing Capital International Airport",
		Latitude:            40.0801,
		Longitude:           116.5850,
		Timezone:            chinaTimezone,
		TemperatureProfile:  TemperatureProfileNorthInland,
		ResolutionSourceURL: "https://www.wunderground.com/history/daily/cn/beijing/ZBAA",
		PolymarketCitySlug:  "beijing",
	},
	{
		Code: "ZSPD",
		Name: "Shanghai Pudong International Airport",
		// Airport reference point from current public airport metadata; this is
		// intentionally airport-aligned, not Shanghai city-centre.
		Latitude:            31.1434,
		Longitude:           121.8050,
		Timezone:            chinaTimezone,
		TemperatureProfile:  TemperatureProfileCoastalFastLock,
		ResolutionSourceURL: "https://www.wunderground.com/history/daily/cn/shanghai/ZSPD",
		PolymarketCitySlug:  "shanghai",
	},
	{
		Code:                "ZGGG",
		Name:                "Guangzhou Baiyun International Airport",
		Latitude:            23.3924,
		Longitude:           113.2990,
		Timezone:            chinaTimezone,
		TemperatureProfile:  TemperatureProfileHumidSouth,
		ResolutionSourceURL: "https://www.wunderground.com/history/daily/cn/guangzhou/ZGGG",
		PolymarketCitySlug:  "guangzhou",
	},
	{
		Code:                "ZHHH",
		Name:                "Wuhan Tianhe International Airport",
		Latitude:            30.7838,
		Longitude:           114.2081,
		Timezone:            chinaTimezone,
		TemperatureProfile:  TemperatureProfileBasinInland,
		ResolutionSourceURL: "https://www.wunderground.com/history/daily/cn/wuhan/ZHHH",
		PolymarketCitySlug:  "wuhan",
	},
	{
		Code:                "ZSQD",
		Name:                "Qingdao Jiaodong International Airport",
		Latitude:            36.3620,
		Longitude:           120.0882,
		Timezone:            chinaTimezone,
		TemperatureProfile:  TemperatureProfileCoastalFastLock,
		ResolutionSourceURL: "https://www.wunderground.com/history/daily/cn/qingdao/ZSQD",
		PolymarketCitySlug:  "qingdao",
	},
}
