---
name: highest-temp-analysis
description: Explain a structured daily highest-temperature bucket forecast for ZSPD and return JSON analysis.
version: 1.3.1
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

The observation data in the payload may come from METAR reports rather than
hourly model/interpolated data. That means:
- `observation_points` may be greater than 24 for the same local date
- `latest_observation_at` may land on `:00` or `:30`
- `bucket_probs` may contain many one-degree buckets rather than only a few
  coarse ranges
- once `observed_high_so_far_c` already guarantees a bucket floor, you should
  treat lower buckets as effectively ruled out

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
    "latest_observation_at": "2026-04-16T21:30:00+08:00",
    "observation_points": 31,
    "hourly_points": 24
  },
  "bucket_distribution": {
    "expected_high_c": 18.09,
    "confidence": 0.9,
    "bucket_probs": [
      { "label": "14C or below", "prob": 0.0020 },
      { "label": "17C", "prob": 0.2177 },
      { "label": "18C", "prob": 0.4923 },
      { "label": "19C", "prob": 0.2584 },
      { "label": "20C", "prob": 0.0296 }
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

### Reasoning guidance

- If one bucket has probability `1.0`, prefer `secondary_risk_bucket: null`
  unless another bucket still represents a genuine operational risk.
- If `observed_high_so_far_c` already exceeds a bucket threshold, do not frame
  lower buckets as live downside outcomes.
- Late-day coverage with many observations and a cooling trend usually supports
  a longer recheck interval than sparse early-day data.
- When `bucket_probs` contains many one-degree buckets, focus your explanation
  on the top 2 to 3 most likely buckets rather than enumerating the whole list.
- Prefer a `secondary_risk_bucket` only when it represents a meaningful nearby
  alternative outcome. If the runner-up probability is negligible, use `null`.
- When the top two buckets are close, treat the adjacent runner-up as the main
  risk to mention. Avoid choosing a distant or much lower-probability bucket.
- If `observed_high_so_far_c` is already equal to the top bucket floor, frame
  remaining uncertainty as whether the day finishes in that bucket or a higher
  neighbouring bucket.
- Use `30` minutes when the outcome is still actively moving or the top buckets
  are tightly clustered; prefer `60` or `90` when late-day coverage is strong
  and cooling suggests the daily high is already in.

### Conflict handling and self-correction

- Treat `observed_high_so_far_c` as a hard lower bound on the finished daily
  high. If it already rules out a lower bucket, do not select or describe that
  lower bucket as a live outcome.
- When signals disagree, resolve them in this order:
  1. hard floor from `observed_high_so_far_c`
  2. explicit `bucket_probs` labels and probabilities
  3. input `sanity_flags`
  4. softer context such as forecast trend, recent momentum, and data coverage
- Compare buckets by probability value, not by array order. If the array is not
  sorted, still choose the highest-probability valid bucket.
- If probabilities do not sum to exactly `1.0`, treat that as normal rounding
  unless the payload clearly indicates a more serious contradiction.
- If optional fields are missing or `null`, continue with the remaining
  evidence. Do not invent missing values to make the explanation sound fuller.
- If `observed_high_so_far_c` conflicts with lower-probability buckets, prefer
  the bucket floor implied by observation reality over stale downside buckets.
- Treat `sanity_flags` as operational warnings, not automatic overrides. A flag
  should influence confidence, reasons, and check timing, but it should not
  force a bucket choice that contradicts stronger evidence.

### Practical heuristics

- For `key_reasons`, prioritize:
  1. the highest-probability bucket and its approximate probability
  2. whether `observed_high_so_far_c` already rules out lower outcomes
  3. recent trend context such as cooling or warming
  4. data coverage quality when it materially affects confidence
- Do not waste a `key_reasons` slot on buckets whose probability is effectively
  zero unless a sanity flag makes them operationally relevant.
- If `observed_high_so_far_c` already matches the current best bucket, the most
  useful secondary risk is usually the next warmer adjacent bucket, not a lower
  bucket that has already been ruled out.
- If `sanity_flags` is present, prefer reusing those exact labels in
  `risk_flags`. Do not invent novel flag names unless the input already
  provides them.
- Keep `key_reasons` grounded in the payload. Prefer "top bucket at 49%" over
  generic claims like "conditions look favorable."
- Adjust `confidence` conservatively:
  1. reduce it when the top two buckets are close, sanity flags are present, or
     observation coverage is sparse
  2. keep it near the input value when signals are broadly aligned
  3. only increase it modestly when a strong observed floor and a clearly
     leading bucket point to the same outcome

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
    "Upward forecast trend of +0.9C supports 18C to 19C"
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
{ "predicted_best_bucket": "18C", "downside_risk": "14C or below", ... }
```

**Semantically invalid bucket below observed floor** — invalid:

If `observed_high_so_far_c` is already `18.4`, this is wrong because `18C`
cannot remain the final bucket:

```
{"predicted_best_bucket":"18C","secondary_risk_bucket":"17C","confidence":0.88,"key_reasons":["18C still leads"],"risk_flags":[],"next_check_in_minutes":60}
```

**Semantically invalid probability ranking** — invalid:

If `"19C"` has a higher probability than `"18C"`, this is wrong because it
blindly follows array order instead of the numeric values:

```
{"predicted_best_bucket":"18C","secondary_risk_bucket":"19C","confidence":0.8,"key_reasons":["18C appears first"],"risk_flags":[],"next_check_in_minutes":60}
```

**Invented risk flag** — invalid:

If the input `sanity_flags` only contains `observed_high_exceeds_latest_forecast`,
do not invent a new label like this:

```
{"predicted_best_bucket":"19C","secondary_risk_bucket":"20C","confidence":0.72,"key_reasons":["Observed floor is strong","19C has the highest probability"],"risk_flags":["late_day_reversal_risk"],"next_check_in_minutes":30}
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
