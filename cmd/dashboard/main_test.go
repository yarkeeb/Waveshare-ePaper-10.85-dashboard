package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDemoCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.png")
	var out bytes.Buffer
	if e := run([]string{"-demo", "-once", "-output", path}, bytes.NewReader(nil), &out); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	im, e := png.Decode(f)
	if e != nil {
		t.Fatal(e)
	}
	if im.Bounds().Dx() != 1360 || im.Bounds().Dy() != 480 {
		t.Fatal(im.Bounds())
	}
}
func TestCLIValidation(t *testing.T) {
	for _, args := range [][]string{{"-check", "-display"}, {"-auth", "gmail", "-check"}, {"-migrate-from", "x", "-demo"}, {"unexpected"}, {"-output", ""}} {
		var out bytes.Buffer
		if e := run(args, bytes.NewReader(nil), &out); e == nil {
			t.Fatal(args)
		}
	}
}
func TestPrintConfig(t *testing.T) {
	var out bytes.Buffer
	if e := run([]string{"-print-config"}, bytes.NewReader(nil), &out); e != nil {
		t.Fatal(e)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatal(out.String())
	}
}
