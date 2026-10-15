// Package builds self-contained Linux deployment archives; it requires only Go.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if e := os.MkdirAll("dist", 0755); e != nil {
		return e
	}
	var checksums strings.Builder
	for _, arch := range []string{"armv6", "arm64"} {
		temp, e := os.MkdirTemp("", "dashboard-package-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(temp)
		binary := filepath.Join(temp, "dashboard")
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", binary, "./cmd/dashboard")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
		if arch == "armv6" {
			cmd.Env = append(cmd.Env, "GOARCH=arm", "GOARM=6")
		} else {
			cmd.Env = append(cmd.Env, "GOARCH=arm64")
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if e = cmd.Run(); e != nil {
			return e
		}
		name := "dashboard-linux-" + arch + ".tar.gz"
		dest := filepath.Join("dist", name)
		if e = archive(dest, binary); e != nil {
			return e
		}
		f, e := os.Open(dest)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(hash, f)
		f.Close()
		if e != nil {
			return e
		}
		fmt.Fprintf(&checksums, "%s  %s\n", hex.EncodeToString(hash.Sum(nil)), name)
		fmt.Println(dest)
	}
	return os.WriteFile("dist/SHA256SUMS", []byte(checksums.String()), 0644)
}
func archive(dest, binary string) (err error) {
	f, e := os.Create(dest)
	if e != nil {
		return e
	}
	defer f.Close()
	defer func() {
		if err != nil {
			_ = os.Remove(dest)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	files := map[string]string{"dashboard": binary, "README.md": "README.md", "NOTICE.md": "NOTICE.md", "dashboard.example.json": "dashboard.example.json", "deploy/dashboard.service": "deploy/dashboard.service"}
	for _, dir := range []string{"third_party", "examples", "docs"} {
		if e = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() {
				files[filepath.ToSlash(path)] = path
			}
			return nil
		}); e != nil {
			return e
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, name := range names {
		if e = addFile(tw, name, files[name]); e != nil {
			return e
		}
	}
	if e = tw.Close(); e != nil {
		return e
	}
	if e = gz.Close(); e != nil {
		return e
	}
	return f.Close()
}
func addFile(tw *tar.Writer, name, path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return e
	}
	mode := int64(0644)
	if name == "dashboard" {
		mode = 0755
	}
	if e = tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: info.Size(), Typeflag: tar.TypeReg}); e != nil {
		return e
	}
	_, e = io.Copy(tw, f)
	return e
}
