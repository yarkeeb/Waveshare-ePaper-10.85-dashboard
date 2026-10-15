package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"

	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
)

type CheckResult struct {
	Widget  string     `json:"widget"`
	OK      bool       `json:"ok"`
	Updated *time.Time `json:"updated,omitempty"`
	Error   string     `json:"error,omitempty"`
}

// Check probes selected providers and returns status metadata, never account
// contents or raw credentials. It performs no display/GPIO operations.
func Check(ctx context.Context, jobs []Job, out io.Writer) error {
	store := model.NewStore()
	Once(ctx, store, jobs)
	snapshot := store.Snapshot()
	report := make([]CheckResult, 0, len(jobs))
	failed := false
	for _, j := range jobs {
		s := snapshot[j.Name]
		ok := s.Error == "" && !s.Updated.IsZero()
		failed = failed || !ok
		result := CheckResult{Widget: j.Name, OK: ok, Error: s.Error}
		if !s.Updated.IsZero() {
			result.Updated = &s.Updated
		}
		report = append(report, result)
	}
	sort.Slice(report, func(i, j int) bool { return report[i].Widget < report[j].Widget })
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if e := encoder.Encode(report); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if failed {
		return errors.New("one or more provider checks failed")
	}
	return nil
}
