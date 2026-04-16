package meteostat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// PointHourlyParams are the query parameters for the /point/hourly endpoint.
type PointHourlyParams struct {
	Lat   float64
	Lon   float64
	Start string // YYYY-MM-DD
	End   string // YYYY-MM-DD
	Alt   int    // station elevation in metres; improves model interpolation
	Tz    string // IANA timezone; returned times use this zone when set
}

const dateLayout = "2006-01-02"

// validate checks that the required fields of PointHourlyParams are present
// and within valid ranges. Tz and Alt are optional and not validated.
func (p PointHourlyParams) validate() error {
	if p.Lat < -90 || p.Lat > 90 {
		return fmt.Errorf("meteostat: lat %g out of range [-90, 90]", p.Lat)
	}
	if p.Lon < -180 || p.Lon > 180 {
		return fmt.Errorf("meteostat: lon %g out of range [-180, 180]", p.Lon)
	}
	if p.Start == "" {
		return fmt.Errorf("meteostat: Start must not be empty")
	}
	if _, err := time.Parse(dateLayout, p.Start); err != nil {
		return fmt.Errorf("meteostat: Start %q is not a valid date (want YYYY-MM-DD)", p.Start)
	}
	if p.End == "" {
		return fmt.Errorf("meteostat: End must not be empty")
	}
	if _, err := time.Parse(dateLayout, p.End); err != nil {
		return fmt.Errorf("meteostat: End %q is not a valid date (want YYYY-MM-DD)", p.End)
	}
	return nil
}

// HourlyDataPoint is one row from the Meteostat /point/hourly response.
// All measurement fields are pointers because Meteostat returns null for
// periods without sensor coverage.
type HourlyDataPoint struct {
	Time string   `json:"time"` // "YYYY-MM-DD HH:MM:SS"
	Temp *float64 `json:"temp"` // temperature in °C
	DewPt *float64 `json:"dwpt"` // dew point in °C
	Prcp *float64 `json:"prcp"` // precipitation in mm
	WSpd *float64 `json:"wspd"` // wind speed in km/h (metric default)
	Coco *int     `json:"coco"` // WMO weather condition code (1–27)
}

type pointHourlyResponse struct {
	Data []HourlyDataPoint `json:"data"`
}

// PointHourly calls the /point/hourly endpoint and returns the data rows.
// Returns an empty slice (not an error) when Meteostat has no data for the
// requested window.
func (c *Client) PointHourly(ctx context.Context, p PointHourlyParams) ([]HourlyDataPoint, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("lat", strconv.FormatFloat(p.Lat, 'f', -1, 64))
	params.Set("lon", strconv.FormatFloat(p.Lon, 'f', -1, 64))
	params.Set("start", p.Start)
	params.Set("end", p.End)
	if p.Alt != 0 {
		params.Set("alt", strconv.Itoa(p.Alt))
	}
	if p.Tz != "" {
		params.Set("tz", p.Tz)
	}

	data, err := c.get(ctx, "/point/hourly", params)
	if err != nil {
		return nil, err
	}

	var resp pointHourlyResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("meteostat: decode response: %w", err)
	}
	return resp.Data, nil
}
