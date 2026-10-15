package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MigrateCredentials imports only known JSON files. It never loads a pickle,
// executes Python, alters the source, or replaces existing destination files.
func MigrateCredentials(from, to string) (names []string, err error) {
	type candidate struct {
		name string
		data []byte
	}
	var files []candidate
	for _, name := range []string{"auth.json", "token.json", "strava_token.json", "claude_creds.json", "antigravity_creds.json", "roborock_session.json", "roborock_stats.json"} {
		path := filepath.Join(from, name)
		info, e := os.Lstat(path)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s must be a regular JSON file", name)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		var obj map[string]any
		if json.Unmarshal(b, &obj) != nil || len(obj) == 0 {
			return nil, fmt.Errorf("%s must contain a nonempty JSON object", name)
		}
		if _, e = os.Lstat(filepath.Join(to, name)); !errors.Is(e, os.ErrNotExist) {
			return nil, fmt.Errorf("destination %s already exists or is inaccessible", name)
		}
		files = append(files, candidate{name, b})
	}
	if len(files) == 0 {
		return nil, errors.New("no supported credential JSON files found")
	}
	if e := os.MkdirAll(to, 0700); e != nil {
		return nil, e
	}
	// Roll back only files created by this invocation if a later import fails.
	defer func() {
		if err != nil {
			for _, name := range names {
				_ = os.Remove(filepath.Join(to, name))
			}
			names = nil
		}
	}()
	for _, f := range files {
		if err = importExclusive(filepath.Join(to, f.name), f.data); err != nil {
			return names, err
		}
		names = append(names, f.name)
	}
	return names, nil
}
func importExclusive(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".migration-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	// Hard-linking the completed temporary file makes publication atomic and
	// fails if the destination appeared after preflight. Both are on one volume.
	return os.Link(f.Name(), path)
}
