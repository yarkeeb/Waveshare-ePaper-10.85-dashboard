package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
)

// Numeric strings occur in older usage/token exports. Reject null, booleans,
// partial numbers and non-finite values instead of inventing zero usage.
func finiteNumber(v any) (float64, bool) {
	var n float64
	var err error
	switch v := v.(type) {
	case float64:
		n = v
	case json.Number:
		n, err = v.Float64()
	case string:
		n, err = strconv.ParseFloat(v, 64)
	default:
		return 0, false
	}
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
func walkObjects(v any, visit func(map[string]any) bool) bool {
	switch v := v.(type) {
	case map[string]any:
		if visit(v) {
			return true
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if walkObjects(v[k], visit) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if walkObjects(child, visit) {
				return true
			}
		}
	}
	return false
}
func timestamp(v any) time.Time {
	if s, ok := v.(string); ok {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999"} {
			if t, e := time.Parse(layout, s); e == nil {
				return t
			}
		}
	}
	n, ok := finiteNumber(v)
	if !ok || n <= 0 {
		return time.Time{}
	}
	if n > 1e12 {
		n /= 1000
	}
	if n > 253402300799 {
		return time.Time{}
	}
	sec, frac := math.Modf(n)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC()
}
func windowSeconds(m map[string]any) float64 {
	if n, ok := finiteNumber(first(m, "limit_window_seconds", "limitWindowSeconds", "window_duration_seconds", "windowDurationSeconds", "duration_seconds", "durationSeconds")); ok && n > 0 {
		return n
	}
	if n, ok := finiteNumber(first(m, "windowDurationMins", "window_duration_mins", "durationMins", "duration_mins")); ok && n > 0 {
		return n * 60
	}
	return 0
}
func usageWindow(m map[string]any, fallback string) (model.Window, bool) {
	var used float64
	if n, ok := finiteNumber(first(m, "percent_left", "remaining_percent", "remainingPercent", "available_percent", "availablePercent", "percentRemaining", "percent_remaining")); ok && n >= 0 {
		// Preserve the upstream convention: 0.5 remaining means 50%; 1 means 1%.
		if n < 1 {
			n *= 100
		}
		used = 100 - clamp(n)
	} else {
		n, ok := finiteNumber(first(m, "usedPercent", "used_percent", "utilization", "usagePercent", "usage_percent", "percentUsed", "percent_used"))
		if !ok || n < 0 {
			return model.Window{}, false
		}
		used = clamp(n)
	}
	name := fallback
	switch seconds := windowSeconds(m); {
	case seconds > 0 && math.Abs(seconds-18000) <= 3600:
		name = "5 hours"
	case seconds > 0 && math.Abs(seconds-604800) <= 86400:
		name = "7 days"
	case seconds > 0:
		name = fmt.Sprintf("%.1f hours", seconds/3600)
	}
	reset := timestamp(first(m, "resetsAt", "resets_at", "resetAt", "reset_at", "resetTime", "reset_time", "reset_time_ms", "endTime", "end_time"))
	if reset.IsZero() {
		if n, ok := finiteNumber(m["reset_after_seconds"]); ok && n >= 0 && n <= 31536000 {
			reset = time.Now().Add(time.Duration(n * float64(time.Second)))
		}
	}
	return model.Window{Name: name, Used: used, Reset: reset}, true
}
func normalizeCodex(raw map[string]any) (model.Usage, error) {
	u := model.Usage{}
	pair := func(m map[string]any) bool {
		candidate := model.Usage{}
		for _, p := range []struct {
			keys []string
			name string
		}{
			{[]string{"five_hour", "primary_window", "primaryWindow", "primary"}, "5 hours"},
			{[]string{"seven_day", "secondary_window", "secondaryWindow", "secondary"}, "7 days"},
		} {
			if w, ok := usageWindow(object(first(m, p.keys...)), p.name); ok {
				candidate.Windows = append(candidate.Windows, w)
			}
		}
		if len(candidate.Windows) == 0 {
			return false
		}
		u = candidate
		return true
	}
	// Prefer the main quota over additional model/review quotas when both exist.
	if !pair(raw) {
		found := walkObjects(raw, func(m map[string]any) bool {
			for _, key := range []string{"rate_limit", "rateLimit", "rate_limits", "rateLimits"} {
				if pair(object(m[key])) {
					return true
				}
			}
			return false
		})
		if !found {
			found = walkObjects(raw, pair)
		}
		if !found {
			walkObjects(raw, func(m map[string]any) bool {
				if windowSeconds(m) > 0 {
					if w, ok := usageWindow(m, "Usage"); ok {
						u.Windows = append(u.Windows, w)
					}
				}
				return false
			})
		}
	}
	if len(u.Windows) == 0 {
		return u, errors.New("unrecognized Codex usage response")
	}
	return u, nil
}
