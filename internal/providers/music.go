package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type track struct {
	Name   string
	Artist struct {
		Text string `json:"#text"`
	}
	Attr struct {
		NowPlaying string `json:"nowplaying"`
	} `json:"@attr"`
	Image []struct {
		Size string
		Text string `json:"#text"`
	}
}
type jsonTracks []track

func (t *jsonTracks) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		var one track
		if e := json.Unmarshal(b, &one); e != nil {
			return e
		}
		*t = []track{one}
		return nil
	}
	type tracks []track
	return json.Unmarshal(b, (*tracks)(t))
}
func readLimited(r io.Reader, n int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, n+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > n {
		return nil, errors.New("image too large")
	}
	return b, nil
}
