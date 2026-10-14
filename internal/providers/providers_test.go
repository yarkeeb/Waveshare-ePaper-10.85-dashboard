package providers

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/model"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mock(fn roundTrip) *Client                                       { return &Client{HTTP: &http.Client{Transport: fn}} }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestWeatherForecastAndUV(t *testing.T) {
	c := mock(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if q.Get("timeformat") != "unixtime" || strings.Contains(q.Get("current"), "uv_index") {
			t.Fatal("UV must come from hourly data; timestamps must be unambiguous")
		}
		return response(200, `{"current":{"time":1800,"temperature_2m":-2,"is_day":0},"hourly":{"time":[0,3600,7200,10800,14400,18000],"temperature_2m":[1,2,3,4,5,6],"weather_code":[0,1,2,3,4,5],"uv_index":[3,4,5,6,7,8]}}`), nil
	})
	v, e := c.Weather(context.Background(), config.Default())
	if e != nil {
		t.Fatal(e)
	}
	w := v.(model.Weather)
	if len(w.Forecast) != 4 || w.Forecast[0].Time.Unix() != 3600 || w.UV != 3 || w.Day {
		t.Fatalf("wrong forecast: %+v", w)
	}
}
func TestShortCryptoHistory(t *testing.T) {
	c := mock(func(r *http.Request) (*http.Response, error) {
		return response(200, `{"prices":[[1,10],[2,20],[3,30]]}`), nil
	})
	v, e := c.Crypto(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if len(v.(model.Crypto).BTC) != 3 {
		t.Fatal(v)
	}
	v50 := downsample(make([]float64, 99), 50)
	if len(v50) != 50 {
		t.Fatal(len(v50))
	}
}
func TestMusicSingletonAndPause(t *testing.T) {
	c := mock(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" {
			t.Fatal("Last.fm must use TLS")
		}
		return response(200, `{"recenttracks":{"track":{"name":"A - B","artist":{"#text":"Artist"},"@attr":{"nowplaying":"true"}}}}`), nil
	})
	v, e := c.Music(context.Background(), config.Default())
	if e != nil {
		t.Fatal(e)
	}
	m := v.(model.Music)
	if !m.Playing || m.Track != "A - B" || m.Artist != "Artist" {
		t.Fatal(m)
	}
}
func TestOAuth401RefreshPreservesAccountAndRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	original := map[string]any{"other": "keep", "tokens": map[string]any{"access_token": "old", "refresh_token": "refresh-old", "account_id": "acct"}}
	if e := writeJSON(path, original); e != nil {
		t.Fatal(e)
	}
	calls := 0
	c := mock(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.Contains(r.URL.Path, "oauth/token") {
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "refresh-old" {
				t.Fatal(r.Form)
			}
			return response(200, `{"access_token":"new","refresh_token":"refresh-new","expires_in":3600}`), nil
		}
		if r.Header.Get("ChatGPT-Account-Id") != "acct" {
			t.Fatal("missing account ID")
		}
		if r.Header.Get("Authorization") == "Bearer old" {
			return response(401, `{}`), nil
		}
		if r.Header.Get("Authorization") != "Bearer new" {
			t.Fatal("stale access token")
		}
		return response(200, `{"rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":604800,"reset_at":1800000000}}}`), nil
	})
	v, e := c.Codex(context.Background(), dir)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 3 || v.(model.Usage).Windows[0].Name != "7 days" {
		t.Fatal(v, calls)
	}
	var saved map[string]any
	_ = readJSON(path, &saved)
	if saved["other"] != "keep" || str(tokenObject(saved), "refresh_token") != "refresh-new" {
		t.Fatal("lost credential metadata")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credentials permissions %o", info.Mode().Perm())
	}
}
func TestOAuthRefreshFailureDoesNotClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_creds.json")
	_ = writeJSON(path, map[string]any{"accessToken": "old", "refreshToken": "oldrefresh", "expiresAt": 1})
	before, _ := os.ReadFile(path)
	c := mock(func(r *http.Request) (*http.Response, error) { return response(429, `{"error":"secret"}`), nil })
	_, _, e := c.access(context.Background(), dir, "claude", false)
	if e == nil {
		t.Fatal("expected failure")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("credentials changed after failure")
	}
	if strings.Contains(e.Error(), "secret") {
		t.Fatal("leaked response body")
	}
}
func TestOAuthState(t *testing.T) {
	for _, s := range []string{"http://localhost/?code=a&state=wrong", "http://localhost/?error=denied&state=expected", "http://localhost/?code=a"} {
		if _, e := callbackCode(s, "expected"); e == nil {
			t.Fatal("accepted bad callback")
		}
	}
	if s, e := callbackCode("http://localhost/?code=a%2Bb&state=expected", "expected"); e != nil || s != "a+b" {
		t.Fatal(s, e)
	}
}
func TestCodexSwappedWindowsAndZero(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal([]byte(`{"rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":604800},"secondary_window":{"used_percent":150,"limit_window_seconds":18000}}}`), &raw)
	v, e := normalizeCodex(raw)
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Windows) != 2 || v.Windows[0].Name != "7 days" || v.Windows[0].Used != 0 || v.Windows[1].Used != 100 {
		t.Fatal(v)
	}
	if _, e = normalizeCodex(map[string]any{}); e == nil {
		t.Fatal("missing data is not zero usage")
	}
}
func TestStravaTimezoneAndFailure(t *testing.T) {
	dir := t.TempDir()
	_ = writeJSON(filepath.Join(dir, "strava_token.json"), map[string]any{"access_token": "test", "expires_at": time.Now().Add(time.Hour).Unix()})
	year := time.Now().Year()
	loc := time.FixedZone("east", 2*3600)
	c := mock(func(r *http.Request) (*http.Response, error) {
		return response(200, fmt.Sprintf(`[{"type":"Ride","distance":10000,"start_date":"%d-12-31T23:00:00Z"},{"type":"Hike","distance":5000,"start_date":"%d-01-01T00:00:00Z"}]`, year-1, year)), nil
	})
	v, e := c.Strava(context.Background(), dir, loc)
	if e != nil {
		t.Fatal(e)
	}
	s := v.(model.Strava)
	if s.CurrentDistance != 10 || s.PreviousDistance != 0 || s.Hike != 5 {
		t.Fatal(s)
	}
}
func TestHTTPErrorRedactsURLCredentials(t *testing.T) {
	c := mock(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("request ?api_key=secret failed")
	})
	var out any
	e := c.Do(context.Background(), "GET", "https://example.com/?api_key=secret", nil, nil, &out)
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
}
func TestFailedPollKeepsLastGoodValue(t *testing.T) {
	s := model.NewStore()
	s.Put("test", 42, nil)
	poll(context.Background(), s, Job{Name: "test", Poll: func(context.Context) (any, error) { return nil, errors.New("offline") }})
	v := s.Snapshot()["test"]
	if v.Value != 42 || v.Error == "" || v.Updated.IsZero() {
		t.Fatal(v)
	}
}
func TestRoborockFrameValidation(t *testing.T) {
	payload := []byte(`{"dps":{"102":"test"}}`)
	b := rrEncode("local-key", 1234567890, 42, 102, payload)
	plain, e := rrDecode("local-key", b)
	if e != nil || string(plain) != string(payload) {
		t.Fatal(string(plain), e)
	}
	for i := 0; i < len(b); i++ {
		if _, e := rrDecode("local-key", b[:i]); e == nil {
			t.Fatalf("accepted truncation %d", i)
		}
	}
	b[25] ^= 1
	if _, e := rrDecode("local-key", b); e == nil {
		t.Fatal("accepted corrupted packet")
	}
}
func FuzzRoborockDecode(f *testing.F) {
	f.Add([]byte{})
	f.Add(rrEncode("test", 1234, 1, 102, []byte(`{}`)))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = rrDecode("test", b) })
}

// Fixture generated independently with Python struct/hashlib and OpenSSL AES-ECB.
func TestRoborockWireFixture(t *testing.T) {
	hexData, e := os.ReadFile("testdata/roborock-v1.hex")
	if e != nil {
		t.Fatal(e)
	}
	wire, e := hex.DecodeString(strings.TrimSpace(string(hexData)))
	if e != nil {
		t.Fatal(e)
	}
	want, e := os.ReadFile("testdata/roborock-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	got, e := rrDecode("0123456789abcdef", wire)
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("decode does not match independent fixture", e)
	}
	encoded := rrEncode("0123456789abcdef", 1700000000, 12345, 102, want)
	if !bytes.Equal(encoded, wire) {
		t.Fatal("encode does not match independent fixture")
	}
}
func TestClaudeSnakeCaseImportRefresh(t *testing.T) {
	dir := t.TempDir()
	_ = writeJSON(filepath.Join(dir, "claude_creds.json"), map[string]any{"access_token": "old", "refresh_token": "old-refresh", "expires_at": 1})
	c := mock(func(r *http.Request) (*http.Response, error) {
		return response(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600}`), nil
	})
	token, root, e := c.access(context.Background(), dir, "claude", false)
	if e != nil || token != "new" || str(root, "refresh_token") != "rotated" {
		t.Fatal("legacy token aliases were not rotated", e)
	}
}
func TestRoborockHawk(t *testing.T) {
	got := rrHawk(rrIOT{U: "user", S: "secret", H: "hawk-key"}, "/user/homes/1", time.Unix(1700000000, 0), "nonce")
	want := `Hawk id="user",s="secret",ts="1700000000",nonce="nonce",mac="bRsYO3cMKL6lBo9YQk/PYvznP1GoInQzMH1c+GE0TTI="`
	if got != want {
		t.Fatalf("Hawk mismatch: %s", got)
	}
}
