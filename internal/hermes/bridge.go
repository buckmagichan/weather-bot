package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/buckmagichan/weather-bot/internal/domain"
)

const defaultHermesBin = "hermes"
const hermesSkill = "highest-temp-analysis"

// Bridge calls the Hermes CLI to produce an AnalysisResult from a HermesAnalysisPayload.
type Bridge struct {
	bin string
}

// NewBridge returns a Bridge using the default "hermes" binary on PATH.
func NewBridge() *Bridge {
	return &Bridge{bin: defaultHermesBin}
}

// NewBridgeWithBin returns a Bridge using the given binary path (useful in tests).
func NewBridgeWithBin(bin string) *Bridge {
	return &Bridge{bin: bin}
}

// Analyze serializes the payload inline into the prompt, invokes the Hermes CLI,
// and extracts + parses the JSON response from stdout.
func (b *Bridge) Analyze(ctx context.Context, payload HermesAnalysisPayload) (*domain.AnalysisResult, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("hermes bridge: marshal payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, b.bin, "chat", "--toolsets", "skills", "-q", buildPrompt(hermesSkill, payloadJSON))
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("hermes bridge timeout: %w", ctxErr)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("hermes bridge: %w\nstderr: %s", err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("hermes bridge: %w", err)
	}

	return parseOutput(out)
}

func buildPrompt(skill string, payloadJSON []byte) string {
	return fmt.Sprintf(
		"Use the %s skill. Analyze this weather payload and return ONLY a valid JSON object — no markdown, no code fences, no prose:\n\n%s",
		skill,
		string(payloadJSON),
	)
}

// parseOutput extracts the last valid JSON object from raw Hermes stdout
// (which may contain banner text, query echo, and UI chrome) and unmarshals
// it into an AnalysisResult.
func parseOutput(raw []byte) (*domain.AnalysisResult, error) {
	result, extracted, err := extractLastAnalysisResult(raw)
	if err != nil {
		return nil, fmt.Errorf("parse hermes output: %w\nraw output:\n%s", err, string(raw))
	}
	if extracted == nil {
		return nil, fmt.Errorf("parse hermes output: no valid analysis result found\nraw output:\n%s", string(raw))
	}
	return result, nil
}

// extractLastJSONObject locates the last complete top-level JSON object in raw.
// It tolerates arbitrary non-JSON prefix (Hermes banner, query echo, UI chrome).
//
// Strategy: scan right-to-left for '}', then forward-scan for the '{' that
// opens the object ending there. The first matching brace pair is returned if
// it passes json.Valid; if the pair is found but the JSON is malformed an error
// is returned immediately rather than silently falling back to an earlier object
// (which could be the payload echo rather than the skill result).
func extractLastJSONObject(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)

	for end := len(trimmed) - 1; end >= 0; end-- {
		if trimmed[end] != '}' {
			continue
		}
		// Find the '{' whose matching '}' is exactly at end.
		for start := 0; start <= end; start++ {
			if trimmed[start] != '{' {
				continue
			}
			objEnd, ok := findObjectEnd(trimmed, start)
			if !ok || objEnd != end {
				continue
			}
			candidate := trimmed[start : end+1]
			if json.Valid(candidate) {
				return candidate, nil
			}
			// Brace pair found but content is not valid JSON — report this
			// rather than falling back to an earlier object.
			return nil, fmt.Errorf("found JSON-like object but it is not valid JSON:\n%s", candidate)
		}
		// No matching '{' for this '}'; try the next earlier '}'.
	}

	return nil, fmt.Errorf("no valid JSON object found")
}

var errUnrelatedJSONObject = errors.New("json object does not match analysis-result schema")

var analysisResultKeys = map[string]struct{}{
	"predicted_best_bucket": {},
	"secondary_risk_bucket": {},
	"confidence":            {},
	"key_reasons":           {},
	"risk_flags":            {},
	"next_check_in_minutes": {},
}

var normalizedAnalysisResultKeys = map[string]string{
	normalizeToken("predicted_best_bucket"): "predicted_best_bucket",
	normalizeToken("secondary_risk_bucket"): "secondary_risk_bucket",
	normalizeToken("confidence"):            "confidence",
	normalizeToken("key_reasons"):           "key_reasons",
	normalizeToken("risk_flags"):            "risk_flags",
	normalizeToken("next_check_in_minutes"): "next_check_in_minutes",
}

var normalizedRiskFlags = map[string]string{
	normalizeToken("observed_high_exceeds_latest_forecast"): "observed_high_exceeds_latest_forecast",
	normalizeToken("missing_previous_forecast"):             "missing_previous_forecast",
	normalizeToken("no_observation_data"):                   "no_observation_data",
	normalizeToken("limited_observation_coverage"):          "limited_observation_coverage",
}

// extractLastAnalysisResult scans JSON objects from the end of the Hermes
// output and returns the last object that matches the expected analysis-result
// schema. Unrelated JSON objects are skipped; malformed analysis-like objects
// are treated as errors.
func extractLastAnalysisResult(raw []byte) (*domain.AnalysisResult, []byte, error) {
	trimmed := bytes.TrimSpace(raw)

	for end := len(trimmed) - 1; end >= 0; end-- {
		if trimmed[end] != '}' {
			continue
		}
		for start := 0; start <= end; start++ {
			if trimmed[start] != '{' {
				continue
			}
			objEnd, ok := findObjectEnd(trimmed, start)
			if !ok || objEnd != end {
				continue
			}
			candidate := trimmed[start : end+1]
			if !json.Valid(candidate) {
				return nil, nil, fmt.Errorf("found JSON-like object but it is not valid JSON:\n%s", candidate)
			}

			result, err := parseAnalysisResultCandidate(candidate)
			switch {
			case err == nil:
				return result, candidate, nil
			case errors.Is(err, errUnrelatedJSONObject):
				continue
			default:
				return nil, candidate, fmt.Errorf("%w\nextracted JSON:\n%s", err, string(candidate))
			}
		}
	}

	return nil, nil, fmt.Errorf("no valid JSON analysis result found")
}

func parseAnalysisResultCandidate(candidate []byte) (*domain.AnalysisResult, error) {
	var rawObj map[string]json.RawMessage
	if err := json.Unmarshal(candidate, &rawObj); err != nil {
		return nil, fmt.Errorf("unmarshal analysis candidate: %w", err)
	}

	canonicalObj, err := canonicalizeAnalysisResultObject(rawObj)
	if err != nil {
		return nil, err
	}

	normalizedJSON, err := json.Marshal(canonicalObj)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized analysis result: %w", err)
	}

	var result domain.AnalysisResult
	if err := json.Unmarshal(normalizedJSON, &result); err != nil {
		return nil, fmt.Errorf("unmarshal analysis result: %w", err)
	}
	normalizeAnalysisResult(&result)
	if err := validateAnalysisResult(result); err != nil {
		return nil, err
	}
	return &result, nil
}

func canonicalizeAnalysisResultObject(rawObj map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	matchedKeys := 0
	canonicalObj := make(map[string]json.RawMessage, len(analysisResultKeys))

	for key, value := range rawObj {
		standardKey, ok := normalizedAnalysisResultKeys[normalizeToken(key)]
		if !ok {
			continue
		}
		matchedKeys++
		if _, exists := canonicalObj[standardKey]; exists {
			return nil, fmt.Errorf("analysis result contains duplicate aliases for key %q", standardKey)
		}
		canonicalObj[standardKey] = value
	}

	if matchedKeys == 0 {
		return nil, errUnrelatedJSONObject
	}
	if len(rawObj) != len(analysisResultKeys) {
		return nil, fmt.Errorf("analysis result must contain exactly %d keys, got %d", len(analysisResultKeys), len(rawObj))
	}
	for key := range analysisResultKeys {
		if _, ok := canonicalObj[key]; !ok {
			return nil, fmt.Errorf("analysis result missing required key %q", key)
		}
	}

	return canonicalObj, nil
}

func normalizeAnalysisResult(result *domain.AnalysisResult) {
	if result == nil {
		return
	}
	for i, flag := range result.RiskFlags {
		if normalized, ok := normalizedRiskFlags[normalizeToken(flag)]; ok {
			result.RiskFlags[i] = normalized
		}
	}
}

func normalizeToken(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validateAnalysisResult(result domain.AnalysisResult) error {
	if strings.TrimSpace(result.PredictedBestBucket) == "" {
		return fmt.Errorf("analysis result has empty predicted_best_bucket")
	}
	if result.SecondaryRiskBucket != nil && strings.TrimSpace(*result.SecondaryRiskBucket) == "" {
		return fmt.Errorf("analysis result has empty secondary_risk_bucket")
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		return fmt.Errorf("analysis result confidence %.4f outside [0,1]", result.Confidence)
	}
	if len(result.KeyReasons) < 2 || len(result.KeyReasons) > 4 {
		return fmt.Errorf("analysis result must contain 2-4 key_reasons, got %d", len(result.KeyReasons))
	}
	for i, reason := range result.KeyReasons {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("analysis result key_reasons[%d] is empty", i)
		}
	}
	for i, flag := range result.RiskFlags {
		if strings.TrimSpace(flag) == "" {
			return fmt.Errorf("analysis result risk_flags[%d] is empty", i)
		}
	}
	if result.NextCheckInMinutes <= 0 {
		return fmt.Errorf("analysis result next_check_in_minutes must be > 0, got %d", result.NextCheckInMinutes)
	}
	return nil
}

// findObjectEnd returns the index of the '}' that closes the '{' at start,
// correctly skipping over quoted strings (including escape sequences) and
// nested objects. Returns (0, false) if the object is not closed.
func findObjectEnd(data []byte, start int) (int, bool) {
	if start >= len(data) || data[start] != '{' {
		return 0, false
	}
	depth := 0
	inString := false
	for i := start; i < len(data); i++ {
		b := data[i]
		if inString {
			if b == '\\' {
				i++ // skip escaped character
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
