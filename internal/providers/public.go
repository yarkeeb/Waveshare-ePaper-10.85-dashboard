package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
)

func (c *Client) Weather(ctx context.Context, cfg config.Config) (any, error) {
	q := url.Values{"latitude": {fmt.Sprint(cfg.Latitude)}, "longitude": {fmt.Sprint(cfg.Longitude)}, "timezone": {cfg.Timezone}, "current": {"temperature_2m,relative_humidity_2m,surface_pressure,wind_speed_10m,wind_direction_10m,weather_code,is_day"}, "hourly": {"temperature_2m,precipitation_probability,weather_code,uv_index"}, "forecast_days": {"2"}, "timeformat": {"unixtime"}}
	var raw struct {
		Current struct {
			Temperature   float64 `json:"temperature_2m"`
			Humidity      float64 `json:"relative_humidity_2m"`
			Pressure      float64 `json:"surface_pressure"`
			WindSpeed     float64 `json:"wind_speed_10m"`
			WindDirection float64 `json:"wind_direction_10m"`
			Code          int     `json:"weather_code"`
			Day           int     `json:"is_day"`
			Time          int64
		}
		Hourly struct {
			Time        []int64
			Temperature []float64 `json:"temperature_2m"`
			Rain        []float64 `json:"precipitation_probability"`
			Code        []int     `json:"weather_code"`
			UV          []float64 `json:"uv_index"`
		}
	}
	if e := c.Do(ctx, "GET", query("https://api.open-meteo.com/v1/forecast", q), nil, nil, &raw); e != nil {
		return nil, e
	}
	if raw.Current.Time == 0 {
		return nil, errors.New("weather response missing current conditions")
	}
	w := model.Weather{Temperature: raw.Current.Temperature, Humidity: raw.Current.Humidity, Pressure: raw.Current.Pressure, WindSpeed: raw.Current.WindSpeed, WindDirection: raw.Current.WindDirection, Code: raw.Current.Code, Day: raw.Current.Day == 1}
	for i, t := range raw.Hourly.Time {
		if t <= raw.Current.Time && i < len(raw.Hourly.UV) {
			w.UV = raw.Hourly.UV[i]
		}
		if t > raw.Current.Time && len(w.Forecast) < 4 && i < len(raw.Hourly.Temperature) && i < len(raw.Hourly.Code) {
			f := model.Forecast{Time: time.Unix(t, 0), Temperature: raw.Hourly.Temperature[i], Code: raw.Hourly.Code[i]}
			if i < len(raw.Hourly.Rain) {
				f.Rain = raw.Hourly.Rain[i]
			}
			w.Forecast = append(w.Forecast, f)
		}
	}
	return w, nil
}
func (c *Client) AQI(ctx context.Context, cfg config.Config) (any, error) {
	var raw struct {
		Current struct {
			AQI *float64 `json:"european_aqi"`
		}
	}
	e := c.Do(ctx, "GET", query("https://air-quality-api.open-meteo.com/v1/air-quality", url.Values{"latitude": {fmt.Sprint(cfg.Latitude)}, "longitude": {fmt.Sprint(cfg.Longitude)}, "current": {"european_aqi"}}), nil, nil, &raw)
	if e != nil {
		return nil, e
	}
	if raw.Current.AQI == nil {
		return nil, errors.New("AQI missing")
	}
	return *raw.Current.AQI, nil
}
func (c *Client) Crypto(ctx context.Context) (any, error) {
	var result model.Crypto
	for _, coin := range []string{"bitcoin", "ethereum"} {
		var raw struct{ Prices [][]float64 }
		if e := c.Do(ctx, "GET", "https://api.coingecko.com/api/v3/coins/"+coin+"/market_chart?vs_currency=usd&days=7", nil, nil, &raw); e != nil {
			return nil, e
		}
		var p []float64
		for _, v := range raw.Prices {
			if len(v) > 1 {
				p = append(p, v[1])
			}
		}
		if len(p) == 0 {
			return nil, errors.New("empty price history")
		}
		p = downsample(p, 50)
		if coin == "bitcoin" {
			result.BTC = p
		} else {
			result.ETH = p
		}
	}
	return result, nil
}
func downsample(v []float64, n int) []float64 {
	if len(v) <= n {
		return append([]float64(nil), v...)
	}
	r := make([]float64, n)
	for i := range r {
		r[i] = v[i*(len(v)-1)/(n-1)]
	}
	return r
}
func history(old []float64, v float64, n int) []float64 {
	r := append(append([]float64(nil), old...), v)
	if len(r) > n {
		r = r[len(r)-n:]
	}
	return r
}
func SystemPoll() func(context.Context) (any, error) {
	var hist []float64
	return func(ctx context.Context) (any, error) {
		b, e := os.ReadFile("/proc/loadavg")
		if e != nil {
			return nil, errors.New("system metrics require Linux /proc")
		}
		fields := strings.Fields(string(b))
		if len(fields) == 0 {
			return nil, errors.New("empty loadavg")
		}
		load, e := strconv.ParseFloat(fields[0], 64)
		if e != nil {
			return nil, e
		}
		b, e = os.ReadFile("/proc/meminfo")
		if e != nil {
			return nil, e
		}
		free := 0
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "MemAvailable:" {
				free, _ = strconv.Atoi(f[1])
				free /= 1024
			}
		}
		hist = history(hist, load, 30)
		return model.System{Load: load, FreeMB: free, History: hist}, nil
	}
}

// A TCP handshake avoids requiring raw-ICMP privileges or an external ping process.
func PingPoll() func(context.Context) (any, error) {
	var hist []float64
	return func(ctx context.Context) (any, error) {
		start := time.Now()
		conn, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", "1.1.1.1:443")
		if e != nil {
			return nil, errors.New("internet probe failed")
		}
		_ = conn.Close()
		ms := float64(time.Since(start).Microseconds()) / 1000
		hist = history(hist, ms, 50)
		return model.Ping{Milliseconds: ms, History: hist}, nil
	}
}
func (c *Client) Music(ctx context.Context, cfg config.Config) (any, error) {
	var raw struct {
		Error  int
		Recent struct {
			Track jsonTracks `json:"track"`
		} `json:"recenttracks"`
	}
	e := c.Do(ctx, "GET", query("https://ws.audioscrobbler.com/2.0/", url.Values{"method": {"user.getrecenttracks"}, "user": {cfg.Spotify.Username}, "api_key": {cfg.Spotify.APIKey}, "format": {"json"}, "limit": {"2"}}), nil, nil, &raw)
	if e != nil {
		return nil, e
	}
	if raw.Error != 0 {
		return nil, fmt.Errorf("Last.fm error %d", raw.Error)
	}
	m := model.Music{}
	if len(raw.Recent.Track) == 0 {
		return m, nil
	}
	t := raw.Recent.Track[0]
	m.Playing = t.Attr.NowPlaying == "true"
	if !m.Playing {
		return m, nil
	}
	m.Artist = t.Artist.Text
	m.Track = t.Name
	for _, i := range t.Image {
		if i.Size == "extralarge" && i.Text != "" {
			m.Cover, _ = c.image(ctx, i.Text)
		}
	}
	return m, nil
}
func (c *Client) image(ctx context.Context, endpoint string) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return nil, e
	}
	if req.URL.Scheme != "https" {
		return nil, errors.New("cover must use HTTPS")
	}
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return nil, errors.New("cover download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, HTTPError{resp.StatusCode}
	}
	return readLimited(resp.Body, 2<<20)
}
