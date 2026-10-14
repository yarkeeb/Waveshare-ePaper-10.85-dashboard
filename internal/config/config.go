package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	Timezone         string  `json:"timezone"`
	StateDir         string  `json:"state_dir"`
	RefreshSeconds   int     `json:"refresh_seconds"`
	FullRefreshEvery int     `json:"full_refresh_every"`
	Weather          bool    `json:"weather"`
	Crypto           bool    `json:"crypto"`
	Gmail            bool    `json:"gmail"`
	Strava           bool    `json:"strava"`
	Claude           bool    `json:"claude"`
	Codex            bool    `json:"codex"`
	Antigravity      bool    `json:"antigravity"`
	Spotify          struct {
		Enabled  bool   `json:"enabled"`
		APIKey   string `json:"api_key"`
		Username string `json:"username"`
	} `json:"spotify"`
	Bambu struct {
		Enabled     bool   `json:"enabled"`
		Host        string `json:"host"`
		Serial      string `json:"serial"`
		AccessCode  string `json:"access_code"`
		InsecureTLS bool   `json:"insecure_tls"`
	} `json:"bambu"`
	Roborock struct {
		Enabled  bool   `json:"enabled"`
		Email    string `json:"email"`
		DeviceID string `json:"device_id"`
	} `json:"roborock"`
	Panel struct {
		GPIOChip        string `json:"gpio_chip"`
		SPIMaster       string `json:"spi_master"`
		SPISlave        string `json:"spi_slave"`
		Reset           int    `json:"reset"`
		DC              int    `json:"dc"`
		Busy            int    `json:"busy"`
		Power           int    `json:"power"`
		ReinitEachFrame *bool  `json:"reinit_each_frame"`
	} `json:"panel"`
}

func Default() Config {
	c := Config{Latitude: 44.8140857, Longitude: 20.3934271, Timezone: "Europe/Belgrade", StateDir: "state", RefreshSeconds: 60, FullRefreshEvery: 600, Weather: true, Crypto: true}
	c.Panel.GPIOChip = "gpiochip0"
	c.Panel.SPIMaster = "/dev/spidev0.0"
	c.Panel.SPISlave = "/dev/spidev0.1"
	c.Panel.Reset = 17
	c.Panel.DC = 25
	c.Panel.Busy = 24
	c.Panel.Power = 18
	return c
}
func Load(path string) (Config, error) {
	c := Default()
	if path != "" {
		f, e := os.Open(path)
		if e != nil {
			return c, e
		}
		defer f.Close()
		d := json.NewDecoder(f)
		d.DisallowUnknownFields()
		if e = d.Decode(&c); e != nil {
			return c, e
		}
		if e = d.Decode(new(any)); e != io.EOF {
			return c, errors.New("config must contain one JSON object")
		}
		if !filepath.IsAbs(c.StateDir) {
			c.StateDir = filepath.Join(filepath.Dir(path), c.StateDir)
		}
	}
	if c.Latitude < -90 || c.Latitude > 90 || c.Longitude < -180 || c.Longitude > 180 {
		return c, errors.New("invalid coordinates")
	}
	if c.RefreshSeconds < 5 || c.FullRefreshEvery < 1 {
		return c, errors.New("refresh_seconds must be >=5 and full_refresh_every >=1")
	}
	if _, e := time.LoadLocation(c.Timezone); e != nil {
		return c, fmt.Errorf("timezone: %w", e)
	}
	if c.StateDir == "" {
		return c, errors.New("state_dir is required")
	}
	if c.Spotify.Enabled && (c.Spotify.APIKey == "" || c.Spotify.Username == "") {
		return c, errors.New("Spotify requires Last.fm api_key and username")
	}
	if c.Bambu.Enabled && (c.Bambu.Host == "" || c.Bambu.Serial == "" || c.Bambu.AccessCode == "") {
		return c, errors.New("Bambu requires host, serial and access_code")
	}
	if c.Roborock.Enabled && c.Roborock.Email == "" {
		return c, errors.New("Roborock requires email")
	}
	return c, nil
}
