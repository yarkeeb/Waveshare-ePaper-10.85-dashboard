//go:build !linux

package display

import (
	"errors"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
)

func Open(config.Config) (Bus, error) {
	return nil, errors.New("physical display requires Linux; use -output for PNG preview")
}
