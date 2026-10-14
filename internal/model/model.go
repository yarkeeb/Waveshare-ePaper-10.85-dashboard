package model

import (
	"encoding/json"
	"sync"
	"time"
)

type Sample struct {
	Value   any
	Updated time.Time
	Error   string
}
type Store struct {
	mu   sync.RWMutex
	data map[string]Sample
}

func NewStore() *Store { return &Store{data: map[string]Sample{}} }

// Values are immutable after insertion. A failed update preserves the last good value.
func (s *Store) Put(key string, v any, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data[key]
	if err != nil {
		old.Error = err.Error()
	} else {
		old = Sample{Value: v, Updated: time.Now()}
	}
	s.data[key] = old
}
func (s *Store) Snapshot() map[string]Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := make(map[string]Sample, len(s.data))
	for k, v := range s.data {
		m[k] = v
	}
	return m
}
func Decode[T any](s Sample) T {
	var v T
	b, _ := json.Marshal(s.Value)
	_ = json.Unmarshal(b, &v)
	return v
}

type Forecast struct {
	Time        time.Time
	Temperature float64
	Code        int
	Rain        float64
}
type Weather struct {
	Temperature, Humidity, Pressure, WindSpeed, WindDirection, UV float64
	Code                                                          int
	Day                                                           bool
	Forecast                                                      []Forecast
}
type Crypto struct{ BTC, ETH []float64 }
type System struct {
	Load    float64
	FreeMB  int
	History []float64
}
type Ping struct {
	Milliseconds float64
	History      []float64
}
type Strava struct {
	Rides, CurrentRides, PreviousRides                      int
	Distance, CurrentDistance, PreviousDistance, Bike, Hike float64
}
type Printer struct {
	Status                            string
	Percent, Remaining, Layer, Layers int
}
type Vacuum struct {
	Status                   string
	Battery                  int
	Cleaning                 bool
	Area, Reference, Percent float64
	Last                     string
}
type Music struct {
	Playing       bool
	Artist, Track string
	Cover         []byte
}
type Window struct {
	Name  string
	Used  float64
	Reset time.Time
}
type Usage struct{ Windows []Window }
