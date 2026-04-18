package aviationweather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMETARParamsValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       METARParams
		wantErr string
	}{
		{name: "valid", p: METARParams{IDs: []string{"ZSPD"}, Hours: 24}},
		{name: "missing ids", p: METARParams{Hours: 24}, wantErr: "IDs"},
		{name: "empty id", p: METARParams{IDs: []string{" "}, Hours: 24}, wantErr: "empty"},
		{name: "bad hours", p: METARParams{IDs: []string{"ZSPD"}, Hours: 0}, wantErr: "Hours"},
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

func TestMETARQueryParams(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	rows, err := c.METAR(context.Background(), METARParams{IDs: []string{"ZSPD", "ZSSS"}, Hours: 36})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}

	checks := map[string]string{
		"ids":    "ZSPD,ZSSS",
		"format": "json",
		"hours":  "36",
	}
	for key, want := range checks {
		if got := captured.Get(key); got != want {
			t.Errorf("query param %q: got %q, want %q", key, got, want)
		}
	}
}

func TestMETARDecodeResponse(t *testing.T) {
	resp := `[{
		"icaoId":"ZSPD",
		"receiptTime":"2026-04-18T13:35:25.157Z",
		"obsTime":1776519000,
		"reportTime":"2026-04-18T13:30:00.000Z",
		"temp":14,
		"dewp":11,
		"wspd":8,
		"rawOb":"METAR ZSPD 181330Z 34004MPS CAVOK 14/11 Q1014 NOSIG"
	}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	rows, err := c.METAR(context.Background(), METARParams{IDs: []string{"ZSPD"}, Hours: 24})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].ICAOID != "ZSPD" {
		t.Errorf("ICAOID: got %q, want %q", rows[0].ICAOID, "ZSPD")
	}
	if rows[0].Temp == nil || *rows[0].Temp != 14 {
		t.Errorf("Temp: got %v, want 14", rows[0].Temp)
	}
	if rows[0].Wspd == nil || *rows[0].Wspd != 8 {
		t.Errorf("Wspd: got %v, want 8", rows[0].Wspd)
	}
}
