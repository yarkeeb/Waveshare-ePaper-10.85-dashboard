package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/display"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/providers"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/render"
	"image/png"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	if e := run(os.Args[1:], os.Stdin, os.Stdout); e != nil {
		slog.Error("dashboard stopped", "error", e)
		os.Exit(1)
	}
}
func run(args []string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	flags.SetOutput(out)
	cfgPath := flags.String("config", "", "JSON configuration path (defaults if omitted)")
	output := flags.String("output", "dashboard.png", "PNG output path; empty disables PNG")
	hardware := flags.Bool("display", false, "use physical Waveshare 10.85 display (Linux)")
	once := flags.Bool("once", false, "fetch and render one frame")
	demo := flags.Bool("demo", false, "use offline example data")
	auth := flags.String("auth", "", "authorize strava, gmail, claude, antigravity or roborock")
	printConfig := flags.Bool("print-config", false, "print default JSON configuration")
	check := flags.Bool("check", false, "check enabled providers and print JSON status without touching the display")
	migrate := flags.String("migrate-from", "", "import credential JSON files from an existing installation without overwriting")
	if e := flags.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	actions := 0
	for _, enabled := range []bool{*check, *auth != "", *migrate != "", *printConfig} {
		if enabled {
			actions++
		}
	}
	if actions > 1 || (actions > 0 && (*demo || *hardware || *once)) {
		return fmt.Errorf("choose one of -check, -auth, -migrate-from, -print-config, or rendering flags")
	}
	if *printConfig {
		b, e := json.MarshalIndent(config.Default(), "", "  ")
		if e != nil {
			return e
		}
		fmt.Fprintln(out, string(b))
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
	if *migrate != "" {
		names, e := providers.MigrateCredentials(*migrate, cfg.StateDir)
		if e != nil {
			return e
		}
		for _, name := range names {
			fmt.Fprintln(out, "Imported", name)
		}
		fmt.Fprintln(out, "Roborock pickle sessions are not imported; use -auth roborock if needed.")
		return nil
	}
	if *check {
		return providers.Check(ctx, providers.Jobs(cfg, client), out)
	}
	if *auth != "" {
		if *auth == "roborock" {
			return client.RoborockAuth(ctx, cfg, in, out)
		}
		return client.Auth(ctx, cfg.StateDir, *auth, in, out)
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
