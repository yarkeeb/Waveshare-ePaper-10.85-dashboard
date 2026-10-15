package render

import (
	"bytes"
	"errors"
	assets "github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/providers"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRenderWidgetCombinations(t *testing.T) {
	r, e := New()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	now := time.Date(2026, 10, 15, 14, 35, 0, 0, time.UTC)
	for _, mode := range []string{"default", "devices", "usage", "music", "empty", "antigravity", "stale"} {
		t.Run(mode, func(t *testing.T) {
			c := config.Default()
			c.Gmail = true
			s := model.NewStore()
			if mode != "empty" {
				providers.Demo(s, now)
			}
			if mode == "stale" {
				for key := range s.Snapshot() {
					s.Put(key, nil, errors.New("offline"))
				}
			}
			switch mode {
			case "devices":
				c.Strava = true
				c.Bambu.Enabled = true
				c.Roborock.Enabled = true
			case "usage":
				c.Codex = true
				c.Claude = true
			case "music":
				c.Spotify.Enabled = true
			case "antigravity":
				c.Antigravity = true
			}
			im := r.Render(c, s.Snapshot(), now)
			if im.Bounds().Dx() != 1360 || im.Bounds().Dy() != 480 {
				t.Fatal(im.Bounds())
			}
			black := 0
			for _, p := range im.Pix {
				if p != 0 && p != 255 {
					t.Fatal("non-monochrome pixel")
				}
				if p == 0 {
					black++
				}
			}
			if black < 1000 {
				t.Fatal("blank image")
			}
			var buf bytes.Buffer
			if e = png.Encode(&buf, im); e != nil {
				t.Fatal(e)
			}
			if dir := os.Getenv("DASHBOARD_PREVIEW_DIR"); dir != "" {
				if e = os.MkdirAll(dir, 0755); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(dir, mode+".png"), buf.Bytes(), 0644); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestAllBundledIconsDecode(t *testing.T) {
	entries, e := assets.Files.ReadDir("icons")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		b, e := assets.Files.ReadFile("icons/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e := decodeIcon(b); e != nil {
			t.Errorf("%s: %v", entry.Name(), e)
		}
	}
}
