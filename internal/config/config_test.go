package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRejectsTyposAndInvalidValues(t *testing.T) {
	for _, s := range []string{`null`, `{"state_dir":""}`, `{"panel":{"busy":17}}`, `{"panel":{"spi_slave":"/dev/spidev0.0"}}`, `{"refresh_seconds":86401}`, `{"weathre":true}`, `{"refresh_seconds":0}`, `{"latitude":100}`, `{"timezone":"Not/AZone"}`, `{} {}`, `{"spotify":{"enabled":true}}`} {
		p := filepath.Join(t.TempDir(), "dashboard.json")
		_ = os.WriteFile(p, []byte(s), 0600)
		if _, e := Load(p); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestConfigPathsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "dashboard.json")
	_ = os.WriteFile(p, []byte(`{"state_dir":"credentials"}`), 0600)
	c, e := Load(p)
	if e != nil {
		t.Fatal(e)
	}
	if c.StateDir != filepath.Join(dir, "credentials") || c.Panel.Reset != 17 || c.RefreshSeconds != 60 {
		t.Fatal(c)
	}
}
