---
name: highest-temp-analysis
description: Explain a structured daily highest-temperature bucket forecast for ZSPD and return JSON analysis.
version: 1.1.0
author: wanghan
license: MIT
---

# highest-temp-analysis

Use this skill when the user provides a structured weather-analysis payload for a daily highest-temperature market.

## Purpose

This skill explains a deterministic weather prediction result produced by the Go pipeline.

The Go pipeline already computes:
- a feature summary
- a temperature bucket distribution
- optional sanity flags

Your job is **not** to recompute the forecast from raw weather data.
Your job is to:
- identify the most likely bucket
- identify the secondary risk bucket
- explain the main reasons
- surface risk flags
- suggest when to check again

You may reason internally. Your final output is a strict JSON object consumed directly by a Go JSON parser. No cleanup, no repair, no post-processing will be applied. The output must be machine-parseable as-is.

## Expected input

The input will be a structured JSON payload with this shape:

```json
{
  "station_code": "ZSPD",
  "target_date_local": "2026-04-16",
  "generated_at": "2026-04-16T14:34:15Z",
  "feature_summary": {
    "latest_forecast_high_c": 17.8,
    "previous_forecast_high_c": 16.9,
    "forecast_trend_c": 0.9,
    "latest_observed_temp_c": 14.4,
    "observed_high_so_far_c": 18.0,
    "temp_change_last_3h_c": -0.6,
    "latest_observation_at": "2026-04-16T22:00:00+08:00",
    "observation_points": 23,
    "hourly_points": 24
  },
  "bucket_distribution": {
    "expected_high_c": 18.09,
    "confidence": 0.9,
    "bucket_probs": [
      { "label": "17C or below", "prob": 0.2177 },
      { "label": "18C", "prob": 0.4923 },
      { "label": "19C", "prob": 0.2604 },
      { "label": "20C or above", "prob": 0.0296 }
    ]
  },
  "sanity_flags": [
    "observed_high_exceeds_latest_forecast"
  ]
}
```

## Output contract

### Required schema

Your entire response MUST be exactly this JSON object and nothing else:

```
{
  "predicted_best_bucket": "<string>",
  "secondary_risk_bucket": "<string or null>",
  "confidence": <number>,
  "key_reasons": ["<string>", ...],
  "risk_flags": ["<string>", ...],
  "next_check_in_minutes": <integer>
}
```

### Field definitions

| Field | Type | Rules |
|---|---|---|
| `predicted_best_bucket` | string | Required. Must be one of the `label` values from `bucket_probs` (e.g. `"18C"`). |
| `secondary_risk_bucket` | string \| null | Required. The next most likely bucket label, or `null` if no meaningful secondary risk. Must be a string or JSON null — never an object. |
| `confidence` | number | Required. Number between 0 and 1 inclusive. Copy or adjust from `bucket_distribution.confidence`. |
| `key_reasons` | array of strings | Required. 2 to 4 short plain-text strings. Each item is a sentence fragment, not an object. |
| `risk_flags` | array of strings | Required. May be empty (`[]`). Each item is a plain string. Never an array of objects. |
| `next_check_in_minutes` | integer | Required. Prefer `30`, `60`, or `90`. Use a shorter interval when data is sparse or sanity flags are present. |

### Permitted keys

The output object MUST contain exactly these six keys and no others:

- `predicted_best_bucket`
- `secondary_risk_bucket`
- `confidence`
- `key_reasons`
- `risk_flags`
- `next_check_in_minutes`

The following keys are **forbidden** and must never appear in the output:

- `most_likely_bucket`
- `reasoning`
- `downside_risk`
- `upside_risk`
- `check_again_at`
- `check_again_reason`
- Any key not listed in the permitted set above

## Good output example

Given the example input above, a correct response looks exactly like this — raw JSON, no prose, no fences:

```
{"predicted_best_bucket":"18C","secondary_risk_bucket":"19C","confidence":0.9,"key_reasons":["Highest bucket probability is 18C at 49%","Observed high already at 18.0C exceeds latest forecast of 17.8C","Upward forecast trend of +0.9C supports 18C or above"],"risk_flags":["observed_high_exceeds_latest_forecast"],"next_check_in_minutes":60}
```

Formatted for readability (same data, both are valid):

```
{
  "predicted_best_bucket": "18C",
  "secondary_risk_bucket": "19C",
  "confidence": 0.9,
  "key_reasons": [
    "Highest bucket probability is 18C at 49%",
    "Observed high already at 18.0C exceeds latest forecast of 17.8C",
    "Upward forecast trend of +0.9C supports 18C or above"
  ],
  "risk_flags": [
    "observed_high_exceeds_latest_forecast"
  ],
  "next_check_in_minutes": 60
}
```

## Bad output examples

The following are all invalid responses. Do not produce any of these.

**Markdown fences** — invalid:

````
```json
{ "predicted_best_bucket": "18C", ... }
```
````

**Prose before JSON** — invalid:

```
Here is my analysis:
{ "predicted_best_bucket": "18C", ... }
```

**Prose after JSON** — invalid:

```
{ "predicted_best_bucket": "18C", ... }
Let me know if you need more detail.
```

**Renamed key** — invalid (`most_likely_bucket` is not a permitted key):

```
{ "most_likely_bucket": "18C", ... }
```

**Wrong field name** — invalid (`reasoning` is not a permitted key):

```
{ "reasoning": "The trend is upward ...", ... }
```

**Object where string is required** — invalid (`secondary_risk_bucket` must be a string or null):

```
{ "secondary_risk_bucket": { "label": "19C", "prob": 0.26 }, ... }
```

**Array of objects where array of strings is required** — invalid (`risk_flags` must be an array of strings):

```
{ "risk_flags": [{ "flag": "observed_high_exceeds_latest_forecast", "severity": "high" }], ... }
```

**Extra forbidden key** — invalid:

```
{ "predicted_best_bucket": "18C", "downside_risk": "17C", ... }
```

## Format rules (summary)

1. Output raw JSON only — no markdown, no code fences, no prose.
2. The response starts with `{` and ends with `}`.
3. No text before `{`. No text after `}`.
4. Exactly six keys — no more, no fewer.
5. Field names are exactly as specified — no synonyms, no camelCase.
6. `secondary_risk_bucket` is a JSON string or JSON `null` — never an object.
7. `risk_flags` is an array of JSON strings — never an array of objects.
8. `key_reasons` contains 2–4 string items.
9. `next_check_in_minutes` is an integer.
