package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

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
	extracted, err := extractLastJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("parse hermes output: %w\nraw output:\n%s", err, string(raw))
	}
	var result domain.AnalysisResult
	if err := json.Unmarshal(extracted, &result); err != nil {
		return nil, fmt.Errorf("parse hermes output: %w\nextracted JSON:\n%s\nraw output:\n%s",
			err, string(extracted), string(raw))
	}
	return &result, nil
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
