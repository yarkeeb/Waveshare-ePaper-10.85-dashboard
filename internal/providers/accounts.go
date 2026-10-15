package providers

import (
	"context"
	"errors"
	"fmt"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"math"
	"sort"
	"strings"
	"time"
)

func (c *Client) Gmail(ctx context.Context, dir string) (any, error) {
	var r struct {
		Unread *int `json:"messagesUnread"`
	}
	if e := c.authorized(ctx, dir, "gmail", "GET", "https://gmail.googleapis.com/gmail/v1/users/me/labels/INBOX", nil, nil, &r); e != nil {
		return nil, e
	}
	if r.Unread == nil {
		return nil, errors.New("Gmail unread count missing")
	}
	return *r.Unread, nil
}
func (c *Client) Strava(ctx context.Context, dir string, loc *time.Location) (any, error) {
	s := model.Strava{}
	year := time.Now().In(loc).Year()
	for page := 1; page <= 10000; page++ {
		var activities []struct {
			Type      string
			SportType string `json:"sport_type"`
			Distance  float64
			Date      time.Time `json:"start_date"`
		}
		endpoint := fmt.Sprintf("https://www.strava.com/api/v3/athlete/activities?per_page=100&page=%d", page)
		if e := c.authorized(ctx, dir, "strava", "GET", endpoint, nil, nil, &activities); e != nil {
			return nil, e
		}
		for _, a := range activities {
			d := a.Distance / 1000
			t := a.Type
			if t == "" {
				t = a.SportType
			}
			switch t {
			case "Ride", "VirtualRide", "EBikeRide", "GravelRide", "MountainBikeRide":
				s.Rides++
				s.Distance += d
				s.Bike += d
				y := a.Date.In(loc).Year()
				if y == year {
					s.CurrentRides++
					s.CurrentDistance += d
				} else if y == year-1 {
					s.PreviousRides++
					s.PreviousDistance += d
				}
			case "Walk", "Hike":
				s.Hike += d
			}
		}
		if len(activities) < 100 {
			return s, nil
		}
	}
	return nil, errors.New("Strava pagination limit exceeded")
}
func (c *Client) Claude(ctx context.Context, dir string) (any, error) {
	var r map[string]any
	if e := c.authorized(ctx, dir, "claude", "GET", "https://api.anthropic.com/api/oauth/usage", map[string]string{"anthropic-beta": "oauth-2025-04-20", "User-Agent": "claude-code/2.0.0"}, nil, &r); e != nil {
		return nil, e
	}
	return namedUsage(r)
}
func namedUsage(r map[string]any) (model.Usage, error) {
	u := model.Usage{}
	for _, v := range []struct{ key, name string }{{"five_hour", "5 hours"}, {"seven_day", "7 days"}} {
		m := object(r[v.key])
		if value, ok := m["utilization"]; ok && validUsage(value) {
			reset, _ := time.Parse(time.RFC3339, str(m, "resets_at"))
			u.Windows = append(u.Windows, model.Window{Name: v.name, Used: clamp(number(value)), Reset: reset})
		}
	}
	if len(u.Windows) == 0 {
		return u, errors.New("usage windows missing")
	}
	return u, nil
}
func validUsage(v any) bool {
	n, ok := v.(float64)
	return ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0
}
func clamp(n float64) float64 { return max(0, min(100, n)) }
func (c *Client) Codex(ctx context.Context, dir string) (any, error) {
	var r map[string]any
	if e := c.authorized(ctx, dir, "codex", "GET", "https://chatgpt.com/backend-api/wham/usage", map[string]string{"User-Agent": "codex-dashboard"}, nil, &r); e != nil {
		return nil, e
	}
	return normalizeCodex(r)
}
func (c *Client) Antigravity(ctx context.Context, dir string) (any, error) {
	var assist map[string]any
	extra := map[string]string{"User-Agent": "antigravity"}
	if e := c.authorized(ctx, dir, "antigravity", "POST", "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", extra, map[string]any{"metadata": map[string]string{"ideType": "ANTIGRAVITY", "platform": "PLATFORM_UNSPECIFIED", "pluginType": "GEMINI"}}, &assist); e != nil {
		return nil, e
	}
	project := str(assist, "cloudaicompanionProject")
	if project == "" {
		project = str(object(assist["cloudaicompanionProject"]), "id")
	}
	body := map[string]string{}
	if project != "" {
		body["project"] = project
	}
	var raw map[string]any
	if e := c.authorized(ctx, dir, "antigravity", "POST", "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", extra, body, &raw); e != nil {
		return nil, e
	}
	u := model.Usage{}
	for id, value := range object(raw["models"]) {
		if strings.HasPrefix(id, "chat_") || strings.HasPrefix(id, "tab_") || strings.HasPrefix(id, "rev") || strings.Contains(id, "image") || strings.Contains(id, "mquery") || strings.Contains(id, "lite") {
			continue
		}
		m := object(value)
		q := object(m["quotaInfo"])
		remaining, ok := q["remainingFraction"]
		if !ok || !validUsage(remaining) {
			continue
		}
		label := str(m, "label", "displayName")
		if label == "" {
			label = id
		}
		reset, _ := time.Parse(time.RFC3339, str(q, "resetTime"))
		u.Windows = append(u.Windows, model.Window{Name: label, Used: clamp((1 - number(remaining)) * 100), Reset: reset})
	}
	sort.Slice(u.Windows, func(i, j int) bool { return u.Windows[i].Name < u.Windows[j].Name })
	if len(u.Windows) == 0 {
		return nil, errors.New("no Antigravity quotas returned")
	}
	return u, nil
}
