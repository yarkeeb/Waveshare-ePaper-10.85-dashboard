package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexResponseCompatibility(t *testing.T) {
	cases := []struct {
		name, body string
		names      []string
		used       []float64
	}{
		{"remaining beats stale used", `{"rate_limit":{"primary_window":{"used_percent":100,"percent_left":0.9,"limit_window_seconds":604800}}}`, []string{"7 days"}, []float64{10}},
		{"one is one percent", `{"rate_limit":{"primary_window":{"used_percent":1}}}`, []string{"5 hours"}, []float64{1}},
		{"camel case wrapped", `{"data":{"rateLimit":{"primaryWindow":{"usedPercent":"7","windowDurationMins":10080},"secondaryWindow":{"remainingPercent":80,"durationMins":300}}}}`, []string{"7 days", "5 hours"}, []float64{7, 20}},
		{"bucket", `{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":5,"windowDurationMins":300},"secondary":{"usedPercent":10,"windowDurationMins":10080}}}}`, []string{"5 hours", "7 days"}, []float64{5, 10}},
		{"duration list", `{"windows":[{"usage_percent":12,"duration_seconds":18000},{"percentUsed":34,"durationSeconds":604800}]}`, []string{"5 hours", "7 days"}, []float64{12, 34}},
		{"named five only", `{"five_hour":{"utilization":14,"resets_at":"2026-10-15T12:00:00Z"}}`, []string{"5 hours"}, []float64{14}},
		{"prefer main", `{"additional_rate_limits":[{"primary_window":{"used_percent":99}}],"rate_limit":{"primary_window":{"used_percent":2,"limit_window_seconds":604800}}}`, []string{"7 days"}, []float64{2}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var raw map[string]any
			if e := json.Unmarshal([]byte(tt.body), &raw); e != nil {
				t.Fatal(e)
			}
			got, e := normalizeCodex(raw)
			if e != nil {
				t.Fatal(e)
			}
			if len(got.Windows) != len(tt.names) {
				t.Fatal(got)
			}
			for i, w := range got.Windows {
				if w.Name != tt.names[i] || w.Used != tt.used[i] {
					t.Fatal(got)
				}
			}
		})
	}
	for _, body := range []string{`{"primary_window":{"used_percent":null}}`, `{"primary_window":{"used_percent":"NaN"}}`, `{"primary_window":{"used_percent":"5oops"}}`, `{"primary_window":{"used_percent":-1}}`} {
		var raw map[string]any
		_ = json.Unmarshal([]byte(body), &raw)
		if _, e := normalizeCodex(raw); e == nil {
			t.Fatalf("invalid usage accepted: %s", body)
		}
	}
}
func TestResetAndExpiryFormats(t *testing.T) {
	want := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, v := range []any{float64(want.Unix()), float64(want.UnixMilli()), want.Format(time.RFC3339), json.Number("1792065600")} {
		if got := timestamp(v); !got.Equal(want) {
			t.Fatalf("%v = %s, want %s", v, got, want)
		}
	}
}
func TestRefreshUpdatesAllExpiryAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	_ = writeJSON(path, map[string]any{"tokens": map[string]any{"access_token": "old", "refresh_token": "refresh", "expiresAt": 1, "expires_at": 1}})
	calls := 0
	c := mock(func(r *http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"access_token":"new","expires_in":3600}`), nil
	})
	for range 2 {
		if _, _, e := c.access(context.Background(), dir, "codex", false); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 1 {
		t.Fatal("stale expiry alias caused repeated refresh", calls)
	}
}
