package hermes

import (
	"strings"
	"testing"
)

// validResult is a minimal full-schema JSON the skill is expected to return.
const validResult = `{"predicted_best_bucket":"18C","secondary_risk_bucket":"19C","confidence":0.9,"key_reasons":["reason one","reason two"],"risk_flags":["observed_high_exceeds_latest_forecast"],"next_check_in_minutes":60}`

// hermesPrefix simulates the banner and query echo that Hermes CLI prepends to
// its output before the actual skill response.
const hermesPrefix = "" +
	"╭─ Hermes ──────────────────────────────────────────────────────╮\n" +
	"│  hermes v0.1.0  ·  model: claude-sonnet-4-6                   │\n" +
	"╰───────────────────────────────────────────────────────────────╯\n" +
	"\n" +
	"Toolsets: skills\n" +
	"Skills:   highest-temp-analysis\n" +
	"\n" +
	"Query: Use the highest-temp-analysis skill. Analyze this weather payload:\n" +
	`{"station_code":"ZSPD","target_date_local":"2026-04-16"}` + "\n\n"

// ---- extractLastJSONObject --------------------------------------------------

func TestExtractLastJSONObject_pure_json(t *testing.T) {
	got, err := extractLastJSONObject([]byte(validResult))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != validResult {
		t.Errorf("got %q, want %q", got, validResult)
	}
}

func TestExtractLastJSONObject_banner_prefix(t *testing.T) {
	raw := hermesPrefix + validResult + "\n"
	got, err := extractLastJSONObject([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != validResult {
		t.Errorf("got %q, want %q", got, validResult)
	}
}

func TestExtractLastJSONObject_payload_echo_before_result(t *testing.T) {
	// The payload JSON from the prompt is echoed back before the skill result.
	// The algorithm must return the skill result (last object), not the payload.
	payloadEcho := `{"station_code":"ZSPD","target_date_local":"2026-04-16"}`
	raw := "Query: " + payloadEcho + "\n" + validResult
	got, err := extractLastJSONObject([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != validResult {
		t.Errorf("extractLastJSONObject returned payload echo instead of skill result\ngot:  %s\nwant: %s", got, validResult)
	}
}

func TestExtractLastJSONObject_no_json_returns_error(t *testing.T) {
	raw := "No JSON here, just plain text and numbers 42."
	_, err := extractLastJSONObject([]byte(raw))
	if err == nil {
		t.Fatal("expected error for output with no JSON, got nil")
	}
}

func TestExtractLastJSONObject_malformed_last_object_returns_error(t *testing.T) {
	// A syntactically invalid object at the end must produce an error rather
	// than silently falling back to the payload echo before it.
	raw := `Query: {"station_code":"ZSPD"}` + "\n" + `{predicted_best_bucket: not valid json}`
	_, err := extractLastJSONObject([]byte(raw))
	if err == nil {
		t.Fatal("expected error for malformed last JSON, got nil")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error should mention 'not valid JSON', got: %v", err)
	}
}

func TestExtractLastJSONObject_braces_inside_string(t *testing.T) {
	// The '}' inside the string value must not be treated as a closing brace.
	raw := []byte(`Some prefix {"key": "has } brace inside", "v": 1}`)
	want := `{"key": "has } brace inside", "v": 1}`
	got, err := extractLastJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractLastJSONObject_escaped_quote_in_string(t *testing.T) {
	// A \" inside a string must not end the string prematurely.
	raw := []byte(`{"k": "say \"hello\" and } done"}`)
	got, err := extractLastJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(raw) {
		t.Errorf("got %q, want %q", got, string(raw))
	}
}

func TestExtractLastJSONObject_trailing_whitespace_ok(t *testing.T) {
	raw := validResult + "\n\n"
	got, err := extractLastJSONObject([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != validResult {
		t.Errorf("got %q, want %q", got, validResult)
	}
}

// ---- parseOutput ------------------------------------------------------------

func TestParseOutput_pure_json(t *testing.T) {
	result, err := parseOutput([]byte(validResult))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PredictedBestBucket != "18C" {
		t.Errorf("PredictedBestBucket: got %q, want 18C", result.PredictedBestBucket)
	}
	if result.SecondaryRiskBucket == nil || *result.SecondaryRiskBucket != "19C" {
		t.Errorf("SecondaryRiskBucket: got %v, want 19C", result.SecondaryRiskBucket)
	}
	if result.Confidence != 0.9 {
		t.Errorf("Confidence: got %v, want 0.9", result.Confidence)
	}
	if len(result.KeyReasons) != 2 {
		t.Errorf("KeyReasons len: got %d, want 2", len(result.KeyReasons))
	}
	if len(result.RiskFlags) != 1 || result.RiskFlags[0] != "observed_high_exceeds_latest_forecast" {
		t.Errorf("RiskFlags: got %v", result.RiskFlags)
	}
	if result.NextCheckInMinutes != 60 {
		t.Errorf("NextCheckInMinutes: got %d, want 60", result.NextCheckInMinutes)
	}
}

func TestParseOutput_null_secondary_risk(t *testing.T) {
	raw := `{"predicted_best_bucket":"14C or below","secondary_risk_bucket":null,"confidence":0.6,"key_reasons":["sparse data"],"risk_flags":[],"next_check_in_minutes":30}`
	result, err := parseOutput([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SecondaryRiskBucket != nil {
		t.Errorf("SecondaryRiskBucket: got %v, want nil", result.SecondaryRiskBucket)
	}
	if len(result.RiskFlags) != 0 {
		t.Errorf("RiskFlags: got %v, want empty", result.RiskFlags)
	}
}

func TestParseOutput_with_hermes_banner(t *testing.T) {
	// Full simulated Hermes output: banner + query echo + skill result.
	raw := hermesPrefix + validResult + "\n"
	result, err := parseOutput([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error with banner prefix: %v", err)
	}
	if result.PredictedBestBucket != "18C" {
		t.Errorf("PredictedBestBucket: got %q, want 18C", result.PredictedBestBucket)
	}
	if result.NextCheckInMinutes != 60 {
		t.Errorf("NextCheckInMinutes: got %d, want 60", result.NextCheckInMinutes)
	}
}

func TestParseOutput_no_json_returns_error_with_raw(t *testing.T) {
	raw := []byte("Hermes could not find the skill. Check your toolsets configuration.")
	_, err := parseOutput(raw)
	if err == nil {
		t.Fatal("expected error for output with no JSON, got nil")
	}
	if !strings.Contains(err.Error(), "raw output") {
		t.Errorf("error should include 'raw output', got: %v", err)
	}
}

func TestParseOutput_malformed_json_returns_error(t *testing.T) {
	raw := []byte(`{predicted_best_bucket: "18C", confidence: 0.9}`)
	_, err := parseOutput(raw)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// ---- buildPrompt ------------------------------------------------------------

func TestBuildPrompt_contains_skill_and_payload(t *testing.T) {
	payload := []byte(`{"station_code":"ZSPD","target_date_local":"2026-04-16"}`)
	prompt := buildPrompt("highest-temp-analysis", payload)

	if !strings.Contains(prompt, "highest-temp-analysis") {
		t.Error("prompt should reference the skill name")
	}
	if !strings.Contains(prompt, string(payload)) {
		t.Error("prompt should embed the full payload JSON inline")
	}
}
