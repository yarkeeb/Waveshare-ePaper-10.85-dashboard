// Command go is the offline benchmark harness, not the production dashboard.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/display"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/render"
)

func main() {
	fixture := flag.String("fixture", "", "shared JSON fixture")
	scenario := flag.String("scenario", "default", "widget layout")
	count := flag.Int("n", 100, "iterations per measurement; 0 measures first frame only")
	preview := flag.String("preview", "", "optional PNG outside measured loops")
	flag.Parse()
	if err := run(*fixture, *scenario, *count, *preview); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path, scenario string, n int, preview string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var fixture map[string]json.RawMessage
	if err = json.Unmarshal(data, &fixture); err != nil {
		return err
	}
	store := model.NewStore()
	for k, v := range fixture {
		// Keep the same concrete values stored by production providers, including
		// the renderer's normal model.Decode serialization overhead.
		var value any
		switch k {
		case "weather":
			value = new(model.Weather)
		case "aqi", "gmail":
			value = new(int)
		case "system":
			value = new(model.System)
		case "ping":
			value = new(model.Ping)
		case "crypto":
			value = new(model.Crypto)
		case "strava":
			value = new(model.Strava)
		case "bambu":
			value = new(model.Printer)
		case "roborock":
			value = new(model.Vacuum)
		case "spotify":
			value = new(model.Music)
		case "claude", "codex", "antigravity":
			value = new(model.Usage)
		default:
			return fmt.Errorf("unexpected fixture key %s", k)
		}
		if err = json.Unmarshal(v, value); err != nil {
			return err
		}
		store.Put(k, reflect.ValueOf(value).Elem().Interface(), nil)
	}
	cfg := config.Default()
	cfg.Gmail = true
	switch scenario {
	case "default":
	case "devices":
		cfg.Strava = true
		cfg.Bambu.Enabled = true
		cfg.Roborock.Enabled = true
	case "usage":
		cfg.Codex = true
		cfg.Claude = true
	case "music":
		cfg.Spotify.Enabled = true
	case "antigravity":
		cfg.Antigravity = true
	default:
		return fmt.Errorf("unknown scenario %s", scenario)
	}
	now := time.Date(2026, 10, 15, 14, 35, 0, 0, time.UTC)
	r, err := render.New()
	if err != nil {
		return err
	}
	defer r.Close()
	var img *image.Gray
	draw := func() { img = r.Render(cfg, store.Snapshot(), now) }
	pack := func() {
		b, e := display.Pack(img)
		if e != nil {
			panic(e)
		}
		if len(b) != 81600 {
			panic("bad framebuffer size")
		}
	}
	encode := func() {
		if e := png.Encode(io.Discard, img); e != nil {
			panic(e)
		}
	}
	draw()
	pack()
	if preview != "" {
		f, e := os.Create(preview)
		if e != nil {
			return e
		}
		e = png.Encode(f, img)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	result := map[string]float64{}
	if n > 0 {
		// Warm both font/icon caches and every measured operation.
		for i := 0; i < 10; i++ {
			draw()
			pack()
			encode()
		}
		measure := func(name string, fn func()) {
			start := time.Now()
			for i := 0; i < n; i++ {
				fn()
			}
			result[name] = float64(time.Since(start).Nanoseconds()) / float64(n) / 1e6
		}
		measure("render_ms", draw)
		measure("pack_ms", pack)
		measure("png_ms", encode)
		measure("render_pack_ms", func() { draw(); pack() })
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
