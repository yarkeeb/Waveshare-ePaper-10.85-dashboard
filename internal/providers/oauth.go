package providers

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type oauthSpec struct {
	File, ClientID, ClientSecret, AuthorizeURL, TokenURL, Redirect, Scope string
	JSON                                                                  bool
}

func spec(provider string) oauthSpec {
	switch provider {
	case "strava":
		return oauthSpec{File: "strava_token.json", AuthorizeURL: "https://www.strava.com/oauth/authorize", TokenURL: "https://www.strava.com/oauth/token", Redirect: "http://localhost", Scope: "activity:read_all"}
	case "claude":
		return oauthSpec{File: "claude_creds.json", ClientID: "9d1c250a-e61b-44d9-88ed-5944d1962f5e", AuthorizeURL: "https://claude.ai/oauth/authorize", TokenURL: "https://platform.claude.com/v1/oauth/token", Redirect: "http://localhost:18924/callback", Scope: "user:inference user:profile", JSON: true}
	case "codex":
		return oauthSpec{File: "auth.json", ClientID: "app_EMoamEEZ73f0CkXaXp7hrann", TokenURL: "https://auth.openai.com/oauth/token"}
	case "antigravity":
		return oauthSpec{File: "antigravity_creds.json", ClientID: "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com", ClientSecret: "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", AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", Redirect: "http://127.0.0.1:8080/callback", Scope: "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email"}
	case "gmail":
		return oauthSpec{File: "token.json", AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", Redirect: "http://127.0.0.1:8080/callback", Scope: "https://www.googleapis.com/auth/gmail.readonly"}
	}
	return oauthSpec{}
}
func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		_, _ = fmt.Sscan(n, &f)
		return f
	}
	return 0
}
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func tokenObject(m map[string]any) map[string]any {
	if t, ok := m["tokens"].(map[string]any); ok {
		return t
	}
	return m
}
func jwtClaims(token string) map[string]any {
	p := strings.Split(token, ".")
	if len(p) != 3 {
		return nil
	}
	b, e := base64.RawURLEncoding.DecodeString(p[1])
	if e != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
func expiry(t map[string]any) time.Time {
	for _, key := range []string{"expiresAt", "expires_at", "expiry"} {
		if v, ok := t[key]; ok {
			if key == "expiresAt" {
				if n, ok := finiteNumber(v); ok {
					return time.UnixMilli(int64(n))
				}
			}
			if parsed := timestamp(v); !parsed.IsZero() {
				return parsed
			}
		}
	}
	return timestamp(jwtClaims(str(t, "access_token", "accessToken", "token"))["exp"])
}
func (c *Client) access(ctx context.Context, dir, provider string, force bool) (string, map[string]any, error) {
	s := spec(provider)
	var root map[string]any
	path := filepath.Join(dir, s.File)
	if e := readJSON(path, &root); e != nil {
		return "", nil, fmt.Errorf("read %s: %w", s.File, e)
	}
	if root == nil {
		return "", nil, errors.New("empty credentials")
	}
	t := tokenObject(root)
	access := str(t, "access_token", "accessToken", "token")
	exp := expiry(t)
	if !force && access != "" && (exp.IsZero() || time.Now().Add(time.Minute).Before(exp)) {
		return access, root, nil
	}
	refresh := str(t, "refresh_token", "refreshToken")
	if refresh == "" {
		return "", nil, errors.New("missing refresh token; authenticate again")
	}
	if v := str(t, "client_id"); v != "" {
		s.ClientID = v
	}
	if v := str(t, "client_secret"); v != "" {
		s.ClientSecret = v
	}
	if s.ClientID == "" {
		return "", nil, errors.New("missing OAuth client ID")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {s.ClientID}}
	if s.ClientSecret != "" {
		form.Set("client_secret", s.ClientSecret)
	}
	var raw map[string]any
	if e := c.tokenRequest(ctx, s, form, &raw); e != nil {
		return "", nil, e
	}
	if str(raw, "access_token") == "" {
		return "", nil, errors.New("token response missing access token")
	}
	mergeTokens(t, raw, provider)
	if provider == "codex" {
		root["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
	}
	if e := writeJSON(path, root); e != nil {
		return "", nil, e
	}
	return str(t, "access_token", "accessToken", "token"), root, nil
}
func mergeTokens(t, raw map[string]any, provider string) {
	accessKey, refreshKey := "access_token", "refresh_token"
	if provider == "claude" {
		accessKey, refreshKey = "accessToken", "refreshToken"
	}
	if provider == "gmail" {
		accessKey = "token"
	}
	for _, key := range []string{"access_token", "accessToken", "token"} {
		if _, ok := t[key]; ok {
			t[key] = raw["access_token"]
		}
	}
	t[accessKey] = raw["access_token"]
	if v := str(raw, "refresh_token"); v != "" {
		for _, key := range []string{"refresh_token", "refreshToken"} {
			if _, ok := t[key]; ok {
				t[key] = v
			}
		}
		t[refreshKey] = v
	}
	if v := str(raw, "id_token"); v != "" {
		t["id_token"] = v
	}
	seconds := number(raw["expires_in"])
	if seconds == 0 {
		seconds = 3600
	}
	exp := time.Now().Add(time.Duration(seconds) * time.Second)
	// Keep all imported expiry aliases consistent so an old alias cannot win.
	if _, ok := t["expiresAt"]; ok {
		t["expiresAt"] = exp.UnixMilli()
	}
	if _, ok := t["expires_at"]; ok {
		t["expires_at"] = exp.Unix()
	}
	if _, ok := t["expiry"]; ok {
		t["expiry"] = exp.UTC().Format(time.RFC3339)
	}
	if provider == "claude" {
		t["expiresAt"] = exp.UnixMilli()
	} else if provider == "gmail" {
		t["expiry"] = exp.UTC().Format(time.RFC3339)
	} else {
		t["expires_at"] = exp.Unix()
	}
}
func (c *Client) tokenRequest(ctx context.Context, s oauthSpec, form url.Values, out any) error {
	var body any = form
	if s.JSON {
		m := map[string]string{}
		for k := range form {
			m[k] = form.Get(k)
		}
		body = m
	}
	return c.Do(ctx, "POST", s.TokenURL, nil, body, out)
}
func randomString(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Auth is explicitly invoked by the user; the daemon never blocks for input.
func (c *Client) Auth(ctx context.Context, dir, provider string, in io.Reader, out io.Writer) error {
	s := spec(provider)
	if s.File == "" {
		return errors.New("unknown auth provider")
	}
	if provider == "codex" {
		return fmt.Errorf("copy your Codex CLI auth.json to %s; dashboard does not initiate Codex login", filepath.Join(dir, s.File))
	}
	reader := bufio.NewReader(in)
	prompt := func(label string) (string, error) {
		fmt.Fprint(out, label)
		line, e := reader.ReadString('\n')
		if e != nil {
			return "", e
		}
		return strings.TrimSpace(line), nil
	}
	if s.ClientID == "" {
		var e error
		if s.ClientID, e = prompt("OAuth client ID: "); e != nil {
			return e
		}
		if s.ClientSecret, e = prompt("OAuth client secret: "); e != nil {
			return e
		}
		if s.ClientID == "" || s.ClientSecret == "" {
			return errors.New("client credentials required")
		}
	}
	state, verifier := randomString(24), randomString(48)
	hash := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {s.ClientID}, "redirect_uri": {s.Redirect}, "response_type": {"code"}, "scope": {s.Scope}, "state": {state}}
	if provider == "claude" || provider == "gmail" {
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(hash[:]))
		q.Set("code_challenge_method", "S256")
	}
	if provider == "gmail" || provider == "antigravity" {
		q.Set("access_type", "offline")
		q.Set("prompt", "consent")
	}
	if provider == "strava" {
		q.Set("approval_prompt", "force")
	}
	fmt.Fprintf(out, "Open this URL, authorize, then copy the full localhost callback URL:\n%s\n", query(s.AuthorizeURL, q))
	callback, e := prompt("Callback URL: ")
	if e != nil {
		return e
	}
	code, e := callbackCode(callback, state)
	if e != nil {
		return e
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {s.ClientID}, "redirect_uri": {s.Redirect}}
	if s.ClientSecret != "" {
		form.Set("client_secret", s.ClientSecret)
	}
	if provider == "claude" || provider == "gmail" {
		form.Set("code_verifier", verifier)
	}
	if provider == "claude" {
		form.Set("state", state)
	}
	var raw map[string]any
	if e = c.tokenRequest(ctx, s, form, &raw); e != nil {
		return e
	}
	if str(raw, "access_token") == "" {
		return errors.New("authorization returned no access token")
	}
	saved := map[string]any{"client_id": s.ClientID}
	if s.ClientSecret != "" {
		saved["client_secret"] = s.ClientSecret
	}
	if provider == "gmail" {
		saved["token_uri"] = s.TokenURL
		saved["scopes"] = strings.Fields(s.Scope)
	}
	mergeTokens(saved, raw, provider)
	return writeJSON(filepath.Join(dir, s.File), saved)
}
func callbackCode(callback, state string) (string, error) {
	u, e := url.Parse(callback)
	if e != nil {
		return "", errors.New("invalid callback URL")
	}
	q := u.Query()
	if q.Get("state") != state {
		return "", errors.New("OAuth state mismatch")
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		return "", errors.New("authorization cancelled or missing code")
	}
	return q.Get("code"), nil
}
func (c *Client) authorized(ctx context.Context, dir, provider, method, endpoint string, extra map[string]string, body, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, root, e := c.access(ctx, dir, provider, attempt == 1)
		if e != nil {
			return e
		}
		headers := bearer(token)
		for k, v := range extra {
			headers[k] = v
		}
		if provider == "codex" {
			t := tokenObject(root)
			id := str(t, "account_id", "accountId")
			if id == "" {
				id = str(root, "account_id", "accountId")
			}
			if id == "" {
				id = str(object(jwtClaims(token)["https://api.openai.com/auth"]), "chatgpt_account_id", "account_id")
			}
			if id == "" {
				id = str(object(jwtClaims(str(t, "id_token"))["https://api.openai.com/auth"]), "chatgpt_account_id", "account_id")
			}
			if id != "" {
				headers["ChatGPT-Account-Id"] = id
			}
		}
		e = c.Do(ctx, method, endpoint, headers, body, out)
		var status HTTPError
		if attempt == 0 && errors.As(e, &status) && (status.Status == 401 || (provider == "codex" && status.Status == 403)) {
			continue
		}
		return e
	}
	return errors.New("authorization failed")
}
