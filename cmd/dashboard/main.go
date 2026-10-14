package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/display"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/providers"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/render"
	"image/png"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	if e := run(); e != nil {
		slog.Error("dashboard stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	cfgPath := flag.String("config", "", "JSON configuration path (defaults if omitted)")
	output := flag.String("output", "dashboard.png", "PNG output path; empty disables PNG")
	hardware := flag.Bool("display", false, "use physical Waveshare 10.85 display (Linux)")
	once := flag.Bool("once", false, "fetch and render one frame")
	demo := flag.Bool("demo", false, "use offline example data")
	auth := flag.String("auth", "", "authorize strava, gmail, claude, antigravity or roborock")
	printConfig := flag.Bool("print-config", false, "print default JSON configuration")
	flag.Parse()
	if *printConfig {
		b, e := json.MarshalIndent(config.Default(), "", "  ")
		if e != nil {
			return e
		}
		fmt.Println(string(b))
		return nil
	}
	cfg, e := config.Load(*cfgPath)
	if e != nil {
		return e
	}
	loc, _ := time.LoadLocation(cfg.Timezone)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := providers.NewClient()
	if *auth != "" {
		if *auth == "roborock" {
			return client.RoborockAuth(ctx, cfg, os.Stdin, os.Stdout)
		}
		return client.Auth(ctx, cfg.StateDir, *auth, os.Stdin, os.Stdout)
	}
	if !*hardware && *output == "" {
		return fmt.Errorf("enable -display or provide -output")
	}
	renderer, e := render.New()
	if e != nil {
		return e
	}
	defer renderer.Close()
	var panel *display.Panel
	if *hardware {
		bus, e := display.Open(cfg)
		if e != nil {
			return e
		}
		reinit := runtime.NumCPU() < 2
		if cfg.Panel.ReinitEachFrame != nil {
			reinit = *cfg.Panel.ReinitEachFrame
		}
		panel = display.New(bus, reinit)
		defer func() {
			if e := panel.Close(); e != nil {
				slog.Warn("panel shutdown", "error", e)
			}
		}()
	}
	store := model.NewStore()
	if *demo {
		providers.Demo(store, time.Now().In(loc))
		slog.Info("offline demo data enabled")
	} else if *once {
		providers.Once(ctx, store, providers.Jobs(cfg, client))
	} else {
		wait := providers.Start(ctx, store, providers.Jobs(cfg, client))
		defer func() { cancel(); wait() }()
	}
	frame := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		start := time.Now()
		img := renderer.Render(cfg, store.Snapshot(), start.In(loc))
		if *output != "" {
			var b bytes.Buffer
			if e = png.Encode(&b, img); e != nil {
				return e
			}
			if e = providers.AtomicWrite(*output, b.Bytes(), 0644); e != nil {
				return e
			}
		}
		if panel != nil {
			frameCtx, stop := context.WithTimeout(ctx, 100*time.Second)
			e = panel.Draw(frameCtx, img, frame%cfg.FullRefreshEvery == 0)
			stop()
			if e != nil {
				if ctx.Err() != nil {
					return nil
				}
				return e
			}
		}
		frame++
		slog.Info("frame rendered", "frame", frame, "duration", time.Since(start).Round(time.Millisecond))
		if *once {
			return nil
		}
		delay := max(5*time.Second, time.Duration(cfg.RefreshSeconds)*time.Second-time.Since(start))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
