package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"time"

	assets "github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const Width = 1360
const Height = 480

type Renderer struct {
	regular, clock *opentype.Font
	faces          map[string]font.Face
	icons          map[string]image.Image
	img            *image.Gray
	ink            color.Gray
}

func New() (*Renderer, error) {
	r := &Renderer{faces: map[string]font.Face{}, icons: map[string]image.Image{}}
	for _, v := range []struct {
		path   string
		target **opentype.Font
	}{{"fnt/src/Aldrich-Regular.ttf", &r.regular}, {"fnt/src/advanced_led_board-7.ttf", &r.clock}} {
		b, e := assets.Files.ReadFile(v.path)
		if e != nil {
			return nil, e
		}
		*v.target, e = opentype.Parse(b)
		if e != nil {
			return nil, e
		}
	}
	return r, nil
}
func (r *Renderer) Close() {
	for _, f := range r.faces {
		_ = f.Close()
	}
}
func (r *Renderer) face(size int, clock bool) font.Face {
	key := fmt.Sprintf("%d-%t", size, clock)
	if f := r.faces[key]; f != nil {
		return f
	}
	fnt := r.regular
	if clock {
		fnt = r.clock
	}
	f, e := opentype.NewFace(fnt, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
	if e != nil {
		panic(e)
	}
	r.faces[key] = f
	return f
}
func (r *Renderer) text(x, y, size, width int, s string) { r.drawText(x, y, size, width, s, false) }
func (r *Renderer) drawText(x, y, size, width int, s string, clock bool) {
	f := r.face(size, clock)
	for font.MeasureString(f, s).Ceil() > width && size > 16 {
		size--
		f = r.face(size, clock)
	}
	if font.MeasureString(f, s).Ceil() > width {
		rr := []rune(s)
		for len(rr) > 0 && font.MeasureString(f, string(rr)+"...").Ceil() > width {
			rr = rr[:len(rr)-1]
		}
		s = string(rr) + "..."
	}
	d := font.Drawer{Dst: r.img, Src: image.NewUniform(r.ink), Face: f, Dot: fixed.P(x, y+f.Metrics().Ascent.Ceil())}
	d.DrawString(s)
}
func (r *Renderer) rect(x, y, w, h int, c color.Gray) {
	draw.Draw(r.img, image.Rect(x, y, x+w, y+h), image.NewUniform(c), image.Point{}, draw.Src)
}
func (r *Renderer) line(x1, y1, x2, y2 int) {
	dx, dy := x2-x1, y2-y1
	n := max(abs(dx), abs(dy))
	for i := 0; i <= n; i++ {
		x, y := x1, y1
		if n > 0 {
			x += dx * i / n
			y += dy * i / n
		}
		r.rect(x, y, 2, 2, r.ink)
	}
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func (r *Renderer) bar(x, y, w, h int, pct float64) {
	r.rect(x, y, w, h, r.ink)
	r.rect(x+2, y+2, w-4, h-4, color.Gray{255})
	r.rect(x+3, y+3, int(float64(w-6)*max(0, min(100, pct))/100), h-6, r.ink)
}
func (r *Renderer) spark(x, y, w, h int, data []float64) {
	if len(data) == 0 {
		return
	}
	lo, hi := data[0], data[0]
	for _, v := range data {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	span := hi - lo
	if span == 0 {
		span = max(1, hi)
		lo = 0
	}
	for i, v := range data {
		bh := int(float64(h) * (v - lo) / span)
		bw := max(1, w/len(data)-2)
		r.rect(x+i*w/len(data), y+h-bh, bw, max(1, bh), r.ink)
	}
}
func (r *Renderer) icon(name string, x, y, size int) {
	key := fmt.Sprintf("%s-%d", name, size)
	img := r.icons[key]
	if img == nil {
		b, e := assets.Files.ReadFile("icons/" + name + ".bmp")
		if e != nil {
			return
		}
		source, e := decodeIcon(b)
		if e != nil {
			return
		}
		scaled := image.NewGray(image.Rect(0, 0, size, size))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), source, source.Bounds(), draw.Src, nil)
		img = scaled
		r.icons[key] = img
	}
	draw.Draw(r.img, image.Rect(x, y, x+size, y+size), img, image.Point{}, draw.Src)
}
func weatherIcon(code int, day bool) string {
	switch {
	case code == 0:
		if day {
			return "icon_sun"
		}
		return "icon_moon"
	case code <= 2:
		return "icon_partly-cloudy-day"
	case code == 3:
		return "icon_clouds"
	case code == 45 || code == 48:
		return "icon_wind"
	case code >= 95:
		return "icon_storm"
	case code >= 71 && code <= 77 || code == 85 || code == 86:
		return "icon_snow"
	default:
		return "icon_rain"
	}
}
func (r *Renderer) state(s model.Sample, x, y, w int) bool {
	if s.Value == nil {
		message := "Waiting for data"
		if s.Error != "" {
			message = "Unavailable — check logs"
		}
		r.text(x, y, 20, w, message)
		return false
	}
	return true
}
func (r *Renderer) title(x, y, size, w int, label string, s model.Sample) {
	if s.Error != "" && s.Value != nil {
		label = "STALE · " + label
	}
	r.text(x, y, size, w, label)
}
func (r *Renderer) usage(x, y, w int, title string, s model.Sample, now time.Time) {
	r.title(x, y, 26, w, title, s)
	if !r.state(s, x, y+36, w) {
		return
	}
	u := model.Decode[model.Usage](s)
	windows := u.Windows
	if len(windows) > 2 {
		start := (now.Minute() / 2 * 2) % len(windows)
		windows = []model.Window{windows[start], windows[(start+1)%len(windows)]}
	}
	for i, v := range windows {
		yy := y + 35 + i*47
		label := fmt.Sprintf("%s  %.0f%%", v.Name, v.Used)
		if !v.Reset.IsZero() {
			d := v.Reset.Sub(now)
			reset := "resetting"
			if d > 0 {
				if d >= 24*time.Hour {
					reset = fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
				} else {
					reset = fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
				}
			}
			label += " · " + reset
		}
		r.text(x, yy, 18, w, label)
		r.bar(x, yy+25, w, 14, v.Used)
	}
}
func (r *Renderer) Render(cfg config.Config, s map[string]model.Sample, now time.Time) *image.Gray {
	r.img = image.NewGray(image.Rect(0, 0, Width, Height))
	r.rect(0, 0, Width, Height, color.Gray{255})
	r.ink = color.Gray{0}
	r.line(453, 10, 453, 470)
	r.line(906, 10, 906, 470)
	// Left column: original widget priorities, with all collectors independent.
	if cfg.Strava {
		r.icon("icon_strava", 20, 20, 50)
		r.title(80, 20, 28, 350, "STRAVA STATS", s["strava"])
		if r.state(s["strava"], 80, 60, 350) {
			v := model.Decode[model.Strava](s["strava"])
			r.text(80, 59, 20, 350, fmt.Sprintf("%d: %.1f km · %d: %.1f km", now.Year(), v.CurrentDistance, now.Year()-1, v.PreviousDistance))
			r.text(80, 84, 20, 350, fmt.Sprintf("Total %.1f km · %d rides", v.Distance, v.Rides))
			r.text(80, 110, 18, 350, fmt.Sprintf("Bike %.0f km  /  Hike %.0f km", v.Bike, v.Hike))
		}
	} else {
		r.icon("icon_cpu", 20, 20, 50)
		r.title(80, 20, 28, 350, "SYSTEM LOAD", s["system"])
		if r.state(s["system"], 80, 60, 350) {
			v := model.Decode[model.System](s["system"])
			r.text(80, 57, 20, 350, fmt.Sprintf("1m: %.2f · Available RAM: %d MB", v.Load, v.FreeMB))
			r.spark(80, 90, 350, 40, v.History)
		}
	}
	r.line(20, 150, 433, 150)
	if cfg.Bambu.Enabled {
		r.icon("icon_3d", 20, 170, 50)
		r.title(80, 170, 28, 350, "BAMBU LAB", s["bambu"])
		if r.state(s["bambu"], 80, 210, 350) {
			v := model.Decode[model.Printer](s["bambu"])
			r.text(80, 208, 24, 350, v.Status)
			r.bar(80, 245, 350, 20, float64(v.Percent))
			r.text(80, 280, 20, 350, fmt.Sprintf("%d%% · %dm · %d/%d layers", v.Percent, v.Remaining, v.Layer, v.Layers))
		}
	} else if cfg.Crypto {
		v := model.Decode[model.Crypto](s["crypto"])
		for i, coin := range []struct {
			name, icon string
			values     []float64
		}{{"BTC", "icon_btc", v.BTC}, {"ETH", "icon_eth", v.ETH}} {
			y := 170 + 78*i
			r.icon(coin.icon, 20, y, 45)
			value := "—"
			if len(coin.values) > 0 {
				value = fmt.Sprintf("$%.0f", coin.values[len(coin.values)-1])
			}
			r.title(80, y, 28, 350, coin.name+": "+value, s["crypto"])
			r.spark(80, y+37, 350, 30, coin.values)
		}
	} else {
		r.text(30, 190, 28, 390, "DAILY DASHBOARD")
		r.text(30, 235, 20, 390, "Weather, time and connected devices")
	}
	r.line(20, 320, 433, 320)
	switch {
	case cfg.Roborock.Enabled:
		r.icon("icon_roborock", 20, 340, 45)
		r.title(80, 337, 26, 350, "ROBOROCK", s["roborock"])
		if r.state(s["roborock"], 80, 375, 350) {
			v := model.Decode[model.Vacuum](s["roborock"])
			r.text(80, 372, 22, 350, fmt.Sprintf("%s · Battery %d%%", v.Status, v.Battery))
			if v.Cleaning {
				r.text(80, 403, 20, 350, fmt.Sprintf("Cleaned %.1f m² · %.0f%%", v.Area, v.Percent))
				r.bar(80, 440, 350, 18, v.Percent)
			} else {
				r.text(80, 408, 20, 350, fmt.Sprintf("Last %.1f m²", v.Reference))
				r.text(80, 437, 20, 350, v.Last)
			}
		}
	case cfg.Antigravity:
		r.usage(30, 338, 400, "ANTIGRAVITY", s["antigravity"], now)
	case cfg.Codex:
		r.usage(30, 338, 400, "CODEX USAGE", s["codex"], now)
	default:
		r.icon("icon_wifi", 20, 340, 45)
		r.title(80, 340, 26, 350, "INTERNET QUALITY", s["ping"])
		if r.state(s["ping"], 80, 375, 350) {
			v := model.Decode[model.Ping](s["ping"])
			r.text(80, 375, 20, 350, fmt.Sprintf("TCP latency: %.0f ms", v.Milliseconds))
			r.spark(30, 410, 400, 40, v.History)
		}
	}
	// Weather and AQI. Hourly timestamps are absolute; rendering uses configured timezone.
	if r.state(s["weather"], 480, 40, 400) {
		v := model.Decode[model.Weather](s["weather"])
		r.icon(weatherIcon(v.Code, v.Day), 473, 20, 85)
		r.text(568, 12, 76, 220, fmt.Sprintf("%.0f°C", v.Temperature))
		r.text(796, 20, 25, 95, "UV")
		if v.UV >= 6 {
			r.rect(790, 53, 95, 52, color.Gray{0})
			r.ink = color.Gray{255}
		}
		r.text(797, 50, 44, 90, fmt.Sprintf("%.0f", v.UV))
		r.ink = color.Gray{0}
		r.text(568, 100, 20, 320, fmt.Sprintf("Humidity %.0f%%", v.Humidity))
		r.text(568, 125, 18, 320, fmt.Sprintf("Pressure %.0f hPa", v.Pressure))
		if s["weather"].Error != "" {
			r.text(480, 130, 16, 85, "STALE")
		}
		r.line(473, 155, 886, 155)
		cx, cy, radius := 550, 231, 45
		for angle := 0; angle < 360; angle += 2 {
			rad := float64(angle) * math.Pi / 180
			r.rect(cx+int(float64(radius)*math.Cos(rad)), cy+int(float64(radius)*math.Sin(rad)), 2, 2, r.ink)
		}
		r.text(542, 166, 18, 25, "N")
		r.text(542, 279, 18, 25, "S")
		r.text(608, 230, 18, 25, "E")
		r.text(477, 230, 18, 25, "W")
		rad := (v.WindDirection - 90) * math.Pi / 180
		tx, ty := cx+int(43*math.Cos(rad)), cy+int(43*math.Sin(rad))
		r.line(cx, cy, tx, ty)
		for _, angle := range []float64{rad + 2.5, rad - 2.5} {
			r.line(tx, ty, tx+int(14*math.Cos(angle)), ty+int(14*math.Sin(angle)))
		}
		r.text(501, 301, 17, 105, fmt.Sprintf("%.0f km/h", v.WindSpeed))
		r.text(660, 173, 22, 220, "AIR QUALITY")
		aqi := s["aqi"]
		if aqi.Value != nil {
			n := model.Decode[float64](aqi)
			if n >= 50 {
				r.rect(662, 213, 220, 88, color.Gray{0})
				r.ink = color.Gray{255}
			}
			r.text(675, 215, 68, 200, fmt.Sprintf("%.0f", n))
			r.ink = color.Gray{0}
			if aqi.Error != "" {
				r.text(675, 300, 16, 200, "STALE")
			}
		} else {
			r.text(675, 237, 22, 200, "Unavailable")
		}
		r.line(473, 325, 886, 325)
		for i, f := range v.Forecast {
			if i >= 4 {
				break
			}
			x := 478 + i*103
			r.text(x+5, 340, 23, 98, f.Time.In(now.Location()).Format("15:04"))
			r.icon(weatherIcon(f.Code, true), x+15, 372, 58)
			r.text(x+10, 435, 23, 95, fmt.Sprintf("%.0f°C", f.Temperature))
		}
	}
	// Clock, usage/music/time progress, and unread inbox.
	r.drawText(934, 15, 170, 405, now.Format("15:04"), true)
	r.text(934, 175, 27, 405, now.Format("02 January 2006 · Mon"))
	r.line(934, 220, 1340, 220)
	if cfg.Claude {
		r.usage(934, 239, 402, "CLAUDE USAGE", s["claude"], now)
	} else if cfg.Spotify.Enabled {
		r.icon("icon_spotify", 934, 245, 115)
		if r.state(s["spotify"], 1065, 280, 275) {
			v := model.Decode[model.Music](s["spotify"])
			if v.Playing {
				if len(v.Cover) > 0 {
					if meta, _, e := image.DecodeConfig(bytes.NewReader(v.Cover)); e == nil && meta.Width > 0 && meta.Height > 0 && meta.Width <= 4096 && meta.Height <= 4096 {
						im, _, e := image.Decode(bytes.NewReader(v.Cover))
						if e == nil {
							xdraw.CatmullRom.Scale(r.img, image.Rect(934, 245, 1049, 360), im, im.Bounds(), draw.Src, nil)
						}
					}
				}
				r.title(1065, 245, 20, 275, "NOW PLAYING", s["spotify"])
				r.text(1065, 282, 25, 275, v.Artist)
				r.text(1065, 321, 22, 275, v.Track)
			} else {
				r.title(1065, 280, 28, 275, "PAUSED", s["spotify"])
			}
		}
	} else {
		r.text(934, 237, 26, 405, "TIME PROGRESS")
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		for i, v := range []struct {
			label      string
			start, end time.Time
		}{{"DAY", dayStart, dayStart.AddDate(0, 0, 1)}, {"MONTH", monthStart, monthStart.AddDate(0, 1, 0)}, {"YEAR", yearStart, yearStart.AddDate(1, 0, 0)}} {
			y := 277 + i*32
			p := 100 * float64(now.Sub(v.start)) / float64(v.end.Sub(v.start))
			r.text(934, y, 21, 100, v.label)
			r.bar(1040, y+3, 230, 20, p)
			r.text(1283, y, 21, 60, fmt.Sprintf("%.0f%%", p))
		}
	}
	r.line(934, 385, 1340, 385)
	r.icon("icon_mail", 934, 406, 55)
	if cfg.Gmail {
		v := s["gmail"]
		label := "Inbox: —"
		if v.Value != nil {
			label = fmt.Sprintf("Unread inbox: %d", model.Decode[int](v))
		}
		r.text(1003, 416, 32, 335, label)
		if v.Error != "" {
			r.text(1003, 450, 16, 335, "STALE / UNAVAILABLE")
		}
	} else {
		r.text(1003, 410, 26, 335, "Gmail disabled")
		r.text(1003, 446, 16, 335, "Enable in dashboard.json")
	}
	// E-paper framebuffer is strictly black/white, including text and album covers.
	for i, p := range r.img.Pix {
		if p < 128 {
			r.img.Pix[i] = 0
		} else {
			r.img.Pix[i] = 255
		}
	}
	return r.img
}
