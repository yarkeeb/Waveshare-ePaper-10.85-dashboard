package providers

import (
	"context"
	"errors"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"log/slog"
	"sync"
	"time"
)

type Job struct {
	Name     string
	Interval time.Duration
	Poll     func(context.Context) (any, error)
	Close    func()
}

func Jobs(cfg config.Config, c *Client) []Job {
	jobs := []Job{}
	if !cfg.Strava {
		jobs = append(jobs, Job{Name: "system", Interval: 30 * time.Second, Poll: SystemPoll()})
	}
	if !cfg.Roborock.Enabled && !cfg.Antigravity && !cfg.Codex {
		jobs = append(jobs, Job{Name: "ping", Interval: 20 * time.Second, Poll: PingPoll()})
	}
	loc, _ := time.LoadLocation(cfg.Timezone)
	add := func(enabled bool, name string, seconds int, poll func(context.Context) (any, error)) {
		if enabled {
			jobs = append(jobs, Job{Name: name, Interval: time.Duration(seconds) * time.Second, Poll: poll})
		}
	}
	add(cfg.Weather, "weather", 600, func(ctx context.Context) (any, error) { return c.Weather(ctx, cfg) })
	add(cfg.Weather, "aqi", 600, func(ctx context.Context) (any, error) { return c.AQI(ctx, cfg) })
	add(cfg.Crypto && !cfg.Bambu.Enabled, "crypto", 600, c.Crypto)
	add(cfg.Gmail, "gmail", 300, func(ctx context.Context) (any, error) { return c.Gmail(ctx, cfg.StateDir) })
	add(cfg.Strava, "strava", 900, func(ctx context.Context) (any, error) { return c.Strava(ctx, cfg.StateDir, loc) })
	add(cfg.Claude, "claude", 600, func(ctx context.Context) (any, error) { return c.Claude(ctx, cfg.StateDir) })
	add(cfg.Codex, "codex", 600, func(ctx context.Context) (any, error) { return c.Codex(ctx, cfg.StateDir) })
	add(cfg.Antigravity, "antigravity", 60, func(ctx context.Context) (any, error) { return c.Antigravity(ctx, cfg.StateDir) })
	add(cfg.Spotify.Enabled, "spotify", 20, func(ctx context.Context) (any, error) { return c.Music(ctx, cfg) })
	add(cfg.Roborock.Enabled, "roborock", 60, func(ctx context.Context) (any, error) { return c.Roborock(ctx, cfg) })
	if cfg.Bambu.Enabled {
		b := NewBambu(cfg)
		jobs = append(jobs, Job{Name: "bambu", Interval: 5 * time.Second, Poll: b.Poll, Close: b.Close})
	}
	return jobs
}
func poll(ctx context.Context, s *model.Store, j Job) error {
	timeout := 30 * time.Second
	if j.Name == "strava" {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	v, e := j.Poll(ctx)
	s.Put(j.Name, v, e)
	if e != nil && ctx.Err() != context.Canceled {
		slog.Warn("widget update failed", "widget", j.Name, "error", e)
	}
	return e
}
func Once(ctx context.Context, s *model.Store, jobs []Job) {
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Go(func() {
			if j.Close != nil {
				defer j.Close()
			}
			poll(ctx, s, j)
		})
	}
	wg.Wait()
}
func Start(ctx context.Context, s *model.Store, jobs []Job) func() {
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Go(func() {
			if j.Close != nil {
				defer j.Close()
			}
			delay := j.Interval
			for {
				err := poll(ctx, s, j)
				if err == nil {
					delay = j.Interval
				} else {
					delay = nextDelay(delay, err)
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	return wg.Wait
}
func Demo(s *model.Store, now time.Time) {
	put := func(k string, v any) { s.Put(k, v, nil) }
	w := model.Weather{Temperature: 21, Humidity: 62, Pressure: 1018, WindSpeed: 12, WindDirection: 45, UV: 3, Code: 2, Day: true}
	for i := 1; i <= 4; i++ {
		w.Forecast = append(w.Forecast, model.Forecast{Time: now.Truncate(time.Hour).Add(time.Duration(i) * time.Hour), Temperature: float64(22 - i), Code: i % 4})
	}
	put("weather", w)
	put("aqi", 34)
	put("system", model.System{Load: 0.32, FreeMB: 287, History: []float64{.1, .2, .4, .3, .3, .2, .5, .1, .4, .2, .32}})
	put("ping", model.Ping{Milliseconds: 24, History: []float64{22, 28, 24, 20, 23, 34, 28, 25, 21, 24}})
	put("crypto", model.Crypto{BTC: []float64{65000, 67000, 64000, 68000, 70000, 69000}, ETH: []float64{2300, 2400, 2250, 2500, 2450, 2600}})
	put("gmail", 7)
	put("strava", model.Strava{Rides: 142, Distance: 5840, CurrentDistance: 1430, PreviousDistance: 2190, Bike: 5840, Hike: 218})
	put("bambu", model.Printer{Status: "RUNNING", Percent: 64, Remaining: 42, Layer: 128, Layers: 200})
	put("roborock", model.Vacuum{Status: "Clean", Battery: 82, Cleaning: true, Area: 24.5, Reference: 60, Percent: 41})
	for _, k := range []string{"claude", "codex", "antigravity"} {
		put(k, model.Usage{Windows: []model.Window{{Name: "5 hours", Used: 32, Reset: now.Add(2 * time.Hour)}, {Name: "7 days", Used: 58, Reset: now.Add(3 * 24 * time.Hour)}}})
	}
	put("spotify", model.Music{Playing: true, Artist: "Demo artist", Track: "A quiet morning"})
}

func nextDelay(previous time.Duration, err error) time.Duration {
	delay := min(max(previous*2, 30*time.Second), 30*time.Minute)
	var status HTTPError
	if errors.As(err, &status) {
		delay = max(delay, status.RetryAfter)
	}
	return delay
}
