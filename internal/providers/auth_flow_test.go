package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
)

// The simulated user reads the printed URL and pastes its callback. No browser,
// third-party authorization, or real token is involved.
type authInput struct {
	out           *bytes.Buffer
	clientPrompts int
	callback      string
	authorization url.Values
}

func (in *authInput) Read(b []byte) (int, error) {
	if in.clientPrompts > 0 {
		in.clientPrompts--
		return copy(b, "test-client\n"), nil
	}
	if in.callback == "" {
		for _, line := range strings.Split(in.out.String(), "\n") {
			if strings.HasPrefix(line, "https://") {
				u, e := url.Parse(line)
				if e != nil {
					return 0, e
				}
				in.authorization = u.Query()
			}
		}
		if in.authorization == nil {
			return 0, io.EOF
		}
		in.callback = in.authorization.Get("redirect_uri") + "?" + url.Values{"code": {"authorized-code"}, "state": {in.authorization.Get("state")}}.Encode() + "\n"
	}
	n := copy(b, in.callback)
	in.callback = in.callback[n:]
	return n, nil
}
func TestOAuthAuthorizationFlows(t *testing.T) {
	for _, provider := range []string{"gmail", "strava", "claude", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			var out bytes.Buffer
			input := &authInput{out: &out}
			if provider == "gmail" || provider == "strava" {
				input.clientPrompts = 2
			}
			dir := t.TempDir()
			calls := 0
			c := mock(func(r *http.Request) (*http.Response, error) {
				calls++
				form := url.Values{}
				if spec(provider).JSON {
					var body map[string]string
					if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
						t.Fatal(e)
					}
					for k, v := range body {
						form.Set(k, v)
					}
				} else {
					if e := r.ParseForm(); e != nil {
						t.Fatal(e)
					}
					form = r.Form
				}
				if form.Get("grant_type") != "authorization_code" || form.Get("code") != "authorized-code" {
					t.Fatal("wrong token exchange")
				}
				if provider == "gmail" || provider == "claude" {
					hash := sha256.Sum256([]byte(form.Get("code_verifier")))
					if form.Get("code_verifier") == "" || base64.RawURLEncoding.EncodeToString(hash[:]) != input.authorization.Get("code_challenge") {
						t.Fatal("PKCE mismatch")
					}
				}
				return response(200, `{"access_token":"new","refresh_token":"refresh","expires_in":3600}`), nil
			})
			if e := c.Auth(context.Background(), dir, provider, input, &out); e != nil {
				t.Fatal(e)
			}
			var saved map[string]any
			if e := readJSON(filepath.Join(dir, spec(provider).File), &saved); e != nil {
				t.Fatal(e)
			}
			if str(saved, "access_token", "accessToken", "token") != "new" || str(saved, "refresh_token", "refreshToken") != "refresh" || calls != 1 {
				t.Fatal("incomplete authorization")
			}
		})
	}
}
func TestRoborockLoginDiscovery(t *testing.T) {
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	cfg.Roborock.Email = "test@example.invalid"
	c := mock(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/getUrlByEmail":
			if r.URL.Host == "euiot.roborock.com" {
				return response(200, `{"code":2008}`), nil
			}
			return response(200, `{"code":200,"data":{"url":"https://region.example.invalid"}}`), nil
		case "/api/v1/sendEmailCode":
			if r.URL.Query().Get("username") != cfg.Roborock.Email || r.Header.Get("header_clientid") == "" {
				t.Fatal("bad code request")
			}
			return response(200, `{"code":200}`), nil
		case "/api/v1/loginWithCode":
			if r.URL.Query().Get("verifycode") != "123456" {
				t.Fatal("missing OTP")
			}
			return response(200, `{"code":200,"data":{"token":"token","rriot":{"u":"user","s":"secret","h":"hawk","k":"key","r":{"a":"https://home.example.invalid","m":"ssl://broker.example.invalid:8883"}}}}`), nil
		case "/api/v1/getHomeDetail":
			if r.Header.Get("Authorization") != "token" {
				t.Fatal("missing cloud authorization")
			}
			return response(200, `{"code":200,"data":{"rrHomeId":123}}`), nil
		case "/user/homes/123":
			if !strings.HasPrefix(r.Header.Get("Authorization"), `Hawk id="user"`) {
				t.Fatal("missing Hawk signature")
			}
			return response(200, `{"success":true,"result":{"devices":[{"duid":"vacuum","localKey":"0123456789abcdef","pv":"1.0"}]}}`), nil
		}
		return nil, fmt.Errorf("unexpected endpoint")
	})
	var out bytes.Buffer
	if e := c.RoborockAuth(context.Background(), cfg, strings.NewReader("123456\n"), &out); e != nil {
		t.Fatal(e)
	}
	var saved rrSession
	if e := readJSON(filepath.Join(cfg.StateDir, "roborock_session.json"), &saved); e != nil {
		t.Fatal(e)
	}
	if saved.Token != "token" || len(saved.Devices) != 1 || saved.Devices[0].LocalKey != "0123456789abcdef" {
		t.Fatal("incomplete saved session")
	}
}
