package providers

import (
	"context"
	"errors"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"testing"
	"time"
)

func TestRunnerShutdownCancelsPollAndCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	closed := make(chan struct{})
	job := Job{Name: "test", Interval: time.Hour, Poll: func(ctx context.Context) (any, error) { close(started); <-ctx.Done(); return nil, ctx.Err() }, Close: func() { close(closed) }}
	wait := Start(ctx, model.NewStore(), []Job{job})
	<-started
	cancel()
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
	select {
	case <-closed:
	default:
		t.Fatal("provider not closed")
	}
}
func TestRunnerHonorsServerBackoff(t *testing.T) {
	if got := nextDelay(5*time.Second, HTTPError{Status: 429, RetryAfter: 2 * time.Hour}); got != 2*time.Hour {
		t.Fatal(got)
	}
	if got := nextDelay(20*time.Minute, errors.New("offline")); got != 30*time.Minute {
		t.Fatal(got)
	}
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	if got := retryAfter("3600", now); got != time.Hour {
		t.Fatal(got)
	}
	if got := retryAfter(now.Add(time.Hour).Format("Mon, 02 Jan 2006 15:04:05 GMT"), now); got != time.Hour {
		t.Fatal(got)
	}
}
func TestInactiveFallbacksAreNotPolled(t *testing.T) {
	cfg := config.Default()
	cfg.Strava = true
	cfg.Codex = true
	cfg.Bambu.Enabled = true
	for _, j := range Jobs(cfg, NewClient()) {
		if j.Name == "system" || j.Name == "ping" || j.Name == "crypto" {
			t.Fatal("hidden fallback polled", j.Name)
		}
	}
}
