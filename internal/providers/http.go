package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Client struct{ HTTP *http.Client }

func NewClient() *Client { return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}} }

type HTTPError struct{ Status int }

func (e HTTPError) Error() string { return fmt.Sprintf("HTTP %d", e.Status) }
func (c *Client) Do(ctx context.Context, method, endpoint string, headers map[string]string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		if form, ok := body.(url.Values); ok {
			reader = bytes.NewBufferString(form.Encode())
			if headers == nil {
				headers = map[string]string{}
			}
			headers["Content-Type"] = "application/x-www-form-urlencoded"
		} else {
			b, e := json.Marshal(body)
			if e != nil {
				return e
			}
			reader = bytes.NewReader(b)
			if headers == nil {
				headers = map[string]string{}
			}
			headers["Content-Type"] = "application/json"
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if e != nil {
		return errors.New("invalid HTTP request")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return fmt.Errorf("request to %s failed: %w", req.URL.Host, ctxOrNetwork(ctx, e))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HTTPError{resp.StatusCode}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if e != nil {
		return e
	}
	if len(b) > 8<<20 {
		return errors.New("response exceeds 8 MiB")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}
func ctxOrNetwork(ctx context.Context, e error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("network or TLS error")
}
func query(endpoint string, values url.Values) string { return endpoint + "?" + values.Encode() }
func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}
func readJSON(path string, out any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return AtomicWrite(path, b, 0600)
}
func AtomicWrite(path string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".dashboard-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(mode); e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
