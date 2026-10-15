package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationPreservesSourceAndNeverOverwrites(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	b := []byte(`{"access_token":"example"}`)
	src := filepath.Join(from, "auth.json")
	_ = os.WriteFile(src, b, 0644)
	_ = os.WriteFile(filepath.Join(from, "roborock_session.pkl"), []byte("must not deserialize"), 0600)
	names, e := MigrateCredentials(from, to)
	if e != nil || len(names) != 1 {
		t.Fatal(names, e)
	}
	after, _ := os.ReadFile(src)
	if !bytes.Equal(after, b) {
		t.Fatal("source mutated")
	}
	info, _ := os.Stat(filepath.Join(to, "auth.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if _, e = MigrateCredentials(from, to); e == nil {
		t.Fatal("overwrote existing credentials")
	}
}
func TestMigrationPreflight(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(from, "auth.json"), []byte(`{"access_token":"example"}`), 0600)
	_ = os.WriteFile(filepath.Join(from, "token.json"), []byte(`null`), 0600)
	if _, e := MigrateCredentials(from, to); e == nil {
		t.Fatal("invalid JSON imported")
	}
	files, _ := os.ReadDir(to)
	if len(files) != 0 {
		t.Fatal("partially imported before validation")
	}
}
func TestCheckReportsFailuresWithoutValues(t *testing.T) {
	jobs := []Job{{Name: "good", Poll: func(context.Context) (any, error) { return map[string]string{"access_token": "do-not-print"}, nil }}, {Name: "bad", Poll: func(context.Context) (any, error) { return nil, errors.New("offline") }}}
	var out bytes.Buffer
	if e := Check(context.Background(), jobs, &out); e == nil {
		t.Fatal("check should fail")
	}
	if strings.Contains(out.String(), "do-not-print") {
		t.Fatal("leaked data")
	}
	var report []CheckResult
	if e := json.Unmarshal(out.Bytes(), &report); e != nil {
		t.Fatal(e)
	}
	if len(report) != 2 || report[0].Widget != "bad" || report[0].OK || !report[1].OK {
		t.Fatal(report)
	}
	if report[0].Updated != nil || report[1].Updated == nil {
		t.Fatal("only successful readings should have a timestamp", report)
	}
}
