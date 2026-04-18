package aviationweather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const metarPath = "/api/data/metar"

// METARParams defines the query parameters for the METAR endpoint.
type METARParams struct {
	IDs   []string
	Hours int
}

func (p METARParams) validate() error {
	if len(p.IDs) == 0 {
		return fmt.Errorf("aviationweather: IDs must not be empty")
	}
	for _, id := range p.IDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("aviationweather: IDs must not contain empty values")
		}
	}
	if p.Hours <= 0 {
		return fmt.Errorf("aviationweather: Hours must be > 0")
	}
	return nil
}

// METARReport is the subset of the AWC METAR JSON we use for ingestion.
type METARReport struct {
	ICAOID      string   `json:"icaoId"`
	ReceiptTime string   `json:"receiptTime"`
	ObsTime     int64    `json:"obsTime"`
	ReportTime  string   `json:"reportTime"`
	Temp        *float64 `json:"temp"`
	Dewp        *float64 `json:"dewp"`
	Wspd        *float64 `json:"wspd"`
}

// METAR fetches decoded reports from the Aviation Weather Center Data API.
func (c *Client) METAR(ctx context.Context, p METARParams) ([]METARReport, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("ids", strings.Join(p.IDs, ","))
	params.Set("format", "json")
	params.Set("hours", strconv.Itoa(p.Hours))

	data, err := c.get(ctx, metarPath, params)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []METARReport{}, nil
	}

	var reports []METARReport
	if err := json.Unmarshal(data, &reports); err != nil {
		return nil, fmt.Errorf("aviationweather: decode response: %w", err)
	}
	return reports, nil
}
