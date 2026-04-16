package meteostat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// =============================================================================
// validate() — parameter validation
// =============================================================================

func TestPointHourlyParams_Validate(t *testing.T) {
	valid := PointHourlyParams{
		Lat:   31.14,
		Lon:   121.81,
		Start: "2026-04-14",
		End:   "2026-04-14",
	}

	tests := []struct {
		name    string
		p       PointHourlyParams
		wantErr string // substring; empty means no error expected
	}{
		{
			name: "valid params",
			p:    valid,
		},
		{
			name:    "lat too low",
			p:       func() PointHourlyParams { q := valid; q.Lat = -91; return q }(),
			wantErr: "lat",
		},
		{
			name:    "lat too high",
			p:       func() PointHourlyParams { q := valid; q.Lat = 91; return q }(),
			wantErr: "lat",
		},
		{
			name:    "lon too low",
			p:       func() PointHourlyParams { q := valid; q.Lon = -181; return q }(),
			wantErr: "lon",
		},
		{
			name:    "lon too high",
			p:       func() PointHourlyParams { q := valid; q.Lon = 181; return q }(),
			wantErr: "lon",
		},
		{
			name:    "empty Start",
			p:       func() PointHourlyParams { q := valid; q.Start = ""; return q }(),
			wantErr: "Start must not be empty",
		},
		{
			name:    "Start not YYYY-MM-DD",
			p:       func() PointHourlyParams { q := valid; q.Start = "14-04-2026"; return q }(),
			wantErr: "Start",
		},
		{
			name:    "empty End",
			p:       func() PointHourlyParams { q := valid; q.End = ""; return q }(),
			wantErr: "End must not be empty",
		},
		{
			name:    "End not YYYY-MM-DD",
			p:       func() PointHourlyParams { q := valid; q.End = "not-a-date"; return q }(),
			wantErr: "End",
		},
		{
			name: "optional Tz accepted",
			p:    func() PointHourlyParams { q := valid; q.Tz = "Asia/Shanghai"; return q }(),
		},
		{
			name: "optional Alt accepted",
			p:    func() PointHourlyParams { q := valid; q.Alt = 4; return q }(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// =============================================================================
// PointHourly — request construction & HTTP behaviour
// =============================================================================

func TestPointHourly_InvalidParams_ReturnError(t *testing.T) {
	c := NewClient("key")
	_, err := c.PointHourly(context.Background(), PointHourlyParams{
		Lat: 999, Lon: 0, Start: "2026-04-14", End: "2026-04-14",
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "lat") {
		t.Errorf("error %q should mention lat", err.Error())
	}
}

func TestPointHourly_RequestConstruction(t *testing.T) {
	const wantResponse = `{"data":[]}`
	var capturedReq *http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantResponse))
	}))
	defer srv.Close()

	p := PointHourlyParams{
		Lat:   31.14,
		Lon:   121.81,
		Start: "2026-04-14",
		End:   "2026-04-14",
		Alt:   4,
		Tz:    "Asia/Shanghai",
	}
	c := NewClient("test-key", WithBaseURL(srv.URL))
	rows, err := c.PointHourly(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}

	q := capturedReq.URL.Query()
	checks := map[string]string{
		"lat":   "31.14",
		"lon":   "121.81",
		"start": "2026-04-14",
		"end":   "2026-04-14",
		"alt":   "4",
		"tz":    "Asia/Shanghai",
	}
	for key, want := range checks {
		if got := q.Get(key); got != want {
			t.Errorf("query param %q: got %q, want %q", key, got, want)
		}
	}

	if got := capturedReq.Header.Get("x-rapidapi-key"); got != "test-key" {
		t.Errorf("x-rapidapi-key header: got %q, want %q", got, "test-key")
	}
	if got := capturedReq.Header.Get("x-rapidapi-host"); got != rapidAPIHost {
		t.Errorf("x-rapidapi-host header: got %q, want %q", got, rapidAPIHost)
	}
}

// TestPointHourly_SparseNullFields verifies that null measurement fields in the
// Meteostat response decode correctly to nil pointers (not zero values).
func TestPointHourly_SparseNullFields(t *testing.T) {
	// Meteostat returns null for periods without sensor coverage.
	resp := `{
		"data": [
			{"time":"2026-04-14 00:00:00","temp":14.5,"dwpt":null,"prcp":null,"wspd":null,"coco":null},
			{"time":"2026-04-14 01:00:00","temp":null,"dwpt":null,"prcp":null,"wspd":null,"coco":null}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := NewClient("key", WithBaseURL(srv.URL))
	rows, err := c.PointHourly(context.Background(), PointHourlyParams{
		Lat: 31.14, Lon: 121.81, Start: "2026-04-14", End: "2026-04-14",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// First row: temp is present, rest are nil.
	r0 := rows[0]
	if r0.Temp == nil || *r0.Temp != 14.5 {
		t.Errorf("row[0].Temp: got %v, want 14.5", r0.Temp)
	}
	if r0.DewPt != nil {
		t.Errorf("row[0].DewPt: expected nil, got %v", *r0.DewPt)
	}
	if r0.Prcp != nil {
		t.Errorf("row[0].Prcp: expected nil, got %v", *r0.Prcp)
	}
	if r0.WSpd != nil {
		t.Errorf("row[0].WSpd: expected nil, got %v", *r0.WSpd)
	}
	if r0.Coco != nil {
		t.Errorf("row[0].Coco: expected nil, got %v", *r0.Coco)
	}

	// Second row: all nil including temp.
	if rows[1].Temp != nil {
		t.Errorf("row[1].Temp: expected nil, got %v", *rows[1].Temp)
	}
}

// TestPointHourly_Non200_ReturnsError ensures HTTP errors surface correctly.
func TestPointHourly_Non200_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Invalid API key"}`))
	}))
	defer srv.Close()

	c := NewClient("bad-key", WithBaseURL(srv.URL))
	_, err := c.PointHourly(context.Background(), PointHourlyParams{
		Lat: 31.14, Lon: 121.81, Start: "2026-04-14", End: "2026-04-14",
	})
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q should mention 401", err.Error())
	}
}

// TestPointHourly_MalformedJSON_ReturnsError ensures decode errors are surfaced.
func TestPointHourly_MalformedJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := NewClient("key", WithBaseURL(srv.URL))
	_, err := c.PointHourly(context.Background(), PointHourlyParams{
		Lat: 31.14, Lon: 121.81, Start: "2026-04-14", End: "2026-04-14",
	})
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if _, ok := err.(*json.SyntaxError); !ok && !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

// TestPointHourly_AllDataFields verifies a fully-populated row decodes correctly.
func TestPointHourly_AllDataFields(t *testing.T) {
	resp := `{"data":[{"time":"2026-04-14 12:00:00","temp":22.3,"dwpt":10.1,"prcp":0.5,"wspd":15.2,"coco":3}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := NewClient("key", WithBaseURL(srv.URL))
	rows, err := c.PointHourly(context.Background(), PointHourlyParams{
		Lat: 31.14, Lon: 121.81, Start: "2026-04-14", End: "2026-04-14",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.Time != "2026-04-14 12:00:00" {
		t.Errorf("Time: got %q, want %q", row.Time, "2026-04-14 12:00:00")
	}
	if row.Temp == nil || *row.Temp != 22.3 {
		t.Errorf("Temp: got %v, want 22.3", row.Temp)
	}
	if row.DewPt == nil || *row.DewPt != 10.1 {
		t.Errorf("DewPt: got %v, want 10.1", row.DewPt)
	}
	if row.Prcp == nil || *row.Prcp != 0.5 {
		t.Errorf("Prcp: got %v, want 0.5", row.Prcp)
	}
	if row.WSpd == nil || *row.WSpd != 15.2 {
		t.Errorf("WSpd: got %v, want 15.2", row.WSpd)
	}
	if row.Coco == nil || *row.Coco != 3 {
		t.Errorf("Coco: got %v, want 3", row.Coco)
	}
}
