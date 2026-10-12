package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestManagerOAuthRefreshModelsAndReturningIdentityFence(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const (
		kid      = "test-key"
		clientID = "oaiapp_weknora_test"
	)
	now := time.Now().UTC().Truncate(time.Second)
	var state struct {
		sync.Mutex
		nonce       string
		subject     string
		mode        string
		challenge   string
		refreshes   int
		accessToken string
	}
	state.subject = "subject-original-1234567890"
	state.mode = "initial"
	state.accessToken = "access-initial-secret"

	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()
		switch r.URL.Path {
		case "/jwks":
			writeJSON(t, w, map[string]any{"keys": []any{rsaJWK(kid, &key.PublicKey)}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if r.Form.Get("client_id") != clientID || r.Form.Get("resource") != defaultResource {
				t.Errorf("unexpected token binding: %v", r.Form)
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				state.refreshes++
				if r.Form.Get("refresh_token") != "refresh-candidate-secret" {
					t.Errorf("unexpected refresh token")
				}
				state.accessToken = "access-rotated-secret"
				idToken := signedIDToken(t, key, kid, provider.URL, clientID, state.subject, "", now)
				writeJSON(t, w, map[string]any{
					"access_token":  state.accessToken,
					"refresh_token": "refresh-rotated-secret",
					"id_token":      idToken,
					"expires_in":    3600,
					"token_type":    "Bearer",
				})
				return
			}
			verifier := r.Form.Get("code_verifier")
			if pkceChallenge(verifier) != state.challenge {
				t.Errorf("PKCE verifier did not match challenge")
			}
			subject := state.subject
			if state.mode == "different-subject" {
				subject = "subject-attacker-0987654321"
			}
			idToken := signedIDToken(t, key, kid, provider.URL, clientID, subject, state.nonce, now)
			state.accessToken = "access-candidate-secret"
			writeJSON(t, w, map[string]any{
				"access_token":  "access-candidate-secret",
				"refresh_token": "refresh-candidate-secret",
				"id_token":      idToken,
				"expires_in":    3600,
				"scope":         requestedScope,
				"token_type":    "Bearer",
			})
		case "/models":
			if r.Header.Get("Authorization") != "Bearer "+state.accessToken {
				t.Errorf("model catalog received wrong bearer credential")
			}
			writeJSON(t, w, map[string]any{"models": []any{
				map[string]any{"slug": "gpt-z", "display_name": "Zulu", "visibility": "list"},
				map[string]any{"slug": "gpt-a", "display_name": "Alpha", "visibility": "list", "input_modalities": []string{"text", "image"}},
				map[string]any{"slug": "hidden", "display_name": "Hidden", "visibility": "hidden", "input_modalities": []string{"image"}},
				map[string]any{"slug": "gpt-a", "display_name": "Duplicate", "visibility": "list"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer provider.Close()

	clockNow := now
	m, err := newManager(t.TempDir(), managerConfig{
		issuer:       provider.URL,
		authorizeURL: provider.URL + "/authorize",
		tokenURL:     provider.URL + "/token",
		jwksURL:      provider.URL + "/jwks",
		modelsURL:    provider.URL + "/models",
		resource:     defaultResource,
		callbackBind: "127.0.0.1",
		callbackPort: 0,
		httpClient:   provider.Client(),
		now:          func() time.Time { return clockNow },
	})
	if err != nil {
		t.Fatal(err)
	}

	started, err := m.Start(context.Background(), "local-user-1")
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(started.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authURL.Query()
	if query.Get("client_id") != dynamicClientID || query.Get("agent_name_hint") != "WeKnora" ||
		query.Get("ext_agent_host_id") != m.host.HostID || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization request is incomplete: %v", query)
	}
	state.Lock()
	state.nonce = query.Get("nonce")
	state.challenge = query.Get("code_challenge")
	state.Unlock()

	wrong := query.Get("redirect_uri") + "?state=wrong&code=ignored&client_id=" + clientID
	resp, err := http.Get(wrong)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong state returned %d", resp.StatusCode)
	}

	callback := query.Get("redirect_uri") + "?state=" + url.QueryEscape(query.Get("state")) + "&code=code-1&client_id=" + clientID
	resp, err = http.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ChatGPT 已连接") {
		t.Fatalf("callback failed: status=%d body=%s", resp.StatusCode, body)
	}

	status, err := m.Status(context.Background(), "local-user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Connected || status.Pending == nil || status.Pending.Status != AttemptSucceeded ||
		status.Account == nil || status.Account.EmailHint != "owner@example.com" || !status.Account.PlanEnabled {
		t.Fatalf("unexpected status: %#v", status)
	}
	encoded, _ := json.Marshal(status)
	for _, secret := range []string{"access-candidate-secret", "refresh-candidate-secret", query.Get("state"), query.Get("nonce"), started.AuthorizationURL} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("status leaked secret material: %s", secret)
		}
	}

	userHash, _ := validateUserID("local-user-1")
	assertMode(t, m.storageDir, 0o700)
	assertMode(t, m.userDir(userHash), 0o700)
	assertMode(t, m.credentialPath(userHash), 0o600)

	models, err := m.Models(context.Background(), "local-user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Slug != "gpt-a" || models[1].Slug != "gpt-z" {
		t.Fatalf("unexpected models: %#v", models)
	}
	if !models[0].SupportsVision || models[1].SupportsVision {
		t.Fatalf("unexpected vision capabilities: %#v", models)
	}

	state.Lock()
	state.mode = "different-subject"
	state.Unlock()
	reauth, err := m.Start(context.Background(), "local-user-1")
	if err != nil {
		t.Fatal(err)
	}
	reauthURL, _ := url.Parse(reauth.AuthorizationURL)
	reauthQuery := reauthURL.Query()
	if reauthQuery.Get("client_id") != clientID || reauthQuery.Get("agent_name_hint") != "" ||
		reauthQuery.Get("id_token_hint") != "" || reauthQuery.Get("login_hint") != "owner@example.com" {
		t.Fatalf("returning authorization did not reuse its bound registration: %v", reauthQuery)
	}
	state.Lock()
	state.nonce = reauthQuery.Get("nonce")
	state.challenge = reauthQuery.Get("code_challenge")
	state.Unlock()
	resp, err = http.Get(reauthQuery.Get("redirect_uri") + "?state=" + url.QueryEscape(reauthQuery.Get("state")) + "&code=code-2")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("identity change returned %d", resp.StatusCode)
	}
	status, err = m.Status(context.Background(), "local-user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Connected || status.Pending == nil || status.Pending.Status != AttemptFailed {
		t.Fatalf("identity fence replaced the existing connection: %#v", status)
	}
	token, err := m.AccessToken(context.Background(), "local-user-1")
	if err != nil || token != "access-candidate-secret" {
		t.Fatalf("existing credential was not preserved: token=%q err=%v", token, err)
	}

	clockNow = now.Add(2 * time.Hour)
	state.Lock()
	state.mode = "refresh"
	state.accessToken = "access-candidate-secret"
	state.Unlock()
	const callers = 8
	results := make(chan string, callers)
	errorsSeen := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, callErr := m.AccessToken(context.Background(), "local-user-1")
			results <- value
			errorsSeen <- callErr
		}()
	}
	wg.Wait()
	close(results)
	close(errorsSeen)
	for callErr := range errorsSeen {
		if callErr != nil {
			t.Fatal(callErr)
		}
	}
	for value := range results {
		if value != "access-rotated-secret" {
			t.Fatalf("unexpected refreshed access token: %q", value)
		}
	}
	state.Lock()
	refreshes := state.refreshes
	state.Unlock()
	if refreshes != 1 {
		t.Fatalf("rotating refresh was called %d times", refreshes)
	}
	stored, err := m.loadCredential(userHash)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "refresh-rotated-secret" || stored.AccessToken != "access-rotated-secret" {
		t.Fatalf("rotated credentials were not atomically persisted")
	}

	if err := m.Disconnect(context.Background(), "local-user-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.credentialPath(userHash)); !os.IsNotExist(err) {
		t.Fatalf("credential remains after disconnect: %v", err)
	}
}

func TestManagerRejectsMissingPlanScopeAndInvalidIssuedClient(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "scope-test"
	var nonce string
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jwks":
			writeJSON(t, w, map[string]any{"keys": []any{rsaJWK(kid, &key.PublicKey)}})
		case "/token":
			writeJSON(t, w, map[string]any{
				"access_token":  "must-not-persist",
				"refresh_token": "must-not-persist-either",
				"id_token":      signedIDToken(t, key, kid, provider.URL, "oaiapp_scope_test", "subject", nonce, time.Now()),
				"expires_in":    3600,
				"scope":         "openid profile email offline_access resource.invoke",
				"token_type":    "Bearer",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer provider.Close()
	m, err := newManager(t.TempDir(), managerConfig{
		issuer: provider.URL, authorizeURL: provider.URL + "/authorize", tokenURL: provider.URL + "/token",
		jwksURL: provider.URL + "/jwks", modelsURL: provider.URL + "/models", resource: defaultResource,
		callbackBind: "127.0.0.1", callbackPort: 0, httpClient: provider.Client(), now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := m.Start(context.Background(), "scope-user")
	if err != nil {
		t.Fatal(err)
	}
	authURL, _ := url.Parse(started.AuthorizationURL)
	query := authURL.Query()
	nonce = query.Get("nonce")

	invalidClientCallback := query.Get("redirect_uri") + "?state=" + url.QueryEscape(query.Get("state")) + "&code=code&client_id=not-issued"
	resp, err := http.Get(invalidClientCallback)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid issued client returned %d", resp.StatusCode)
	}
	status, _ := m.Status(context.Background(), "scope-user")
	if status.Connected || status.Pending == nil || status.Pending.Status != AttemptFailed {
		t.Fatalf("invalid client registration was accepted: %#v", status)
	}

	started, err = m.Start(context.Background(), "scope-user")
	if err != nil {
		t.Fatal(err)
	}
	authURL, _ = url.Parse(started.AuthorizationURL)
	query = authURL.Query()
	nonce = query.Get("nonce")
	callback := query.Get("redirect_uri") + "?state=" + url.QueryEscape(query.Get("state")) + "&code=code&client_id=oaiapp_scope_test"
	resp, err = http.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("missing plan scope returned %d", resp.StatusCode)
	}
	status, _ = m.Status(context.Background(), "scope-user")
	if status.Connected || status.Pending == nil || status.Pending.Status != AttemptFailed ||
		!strings.Contains(status.Pending.Error, "plan access") {
		t.Fatalf("missing plan scope was accepted: %#v", status)
	}
}

func TestManagerRejectsInvalidCallbackEnvironment(t *testing.T) {
	t.Setenv("CHATGPT_CALLBACK_PORT", "not-a-port")
	if _, err := NewManager(t.TempDir()); err == nil {
		t.Fatal("invalid callback port was accepted")
	}
	t.Setenv("CHATGPT_CALLBACK_PORT", "0")
	t.Setenv("CHATGPT_CALLBACK_BIND", "127.0.0.2")
	if _, err := NewManager(t.TempDir()); err == nil {
		t.Fatal("invalid callback bind address was accepted")
	}
}

func TestVerifyIDTokenRejectsIdentityBindingFailures(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const (
		kid      = "identity-test"
		clientID = "oaiapp_identity_test"
		nonce    = "expected-nonce"
	)
	now := time.Now().UTC().Truncate(time.Second)
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"keys": []any{rsaJWK(kid, &key.PublicKey)}})
	}))
	defer provider.Close()
	m, err := newManager(t.TempDir(), managerConfig{
		issuer: provider.URL, authorizeURL: provider.URL, tokenURL: provider.URL,
		jwksURL: provider.URL, modelsURL: provider.URL, resource: defaultResource,
		callbackBind: "127.0.0.1", callbackPort: 0, httpClient: provider.Client(), now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	base := jwt.MapClaims{
		"iss":   provider.URL,
		"aud":   clientID,
		"sub":   "subject",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"nonce": nonce,
	}
	if _, err := m.verifyIDToken(context.Background(), signClaims(t, key, kid, base), clientID, nonce); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}
	tests := []struct {
		name   string
		change func(jwt.MapClaims)
	}{
		{name: "issuer", change: func(c jwt.MapClaims) { c["iss"] = "https://attacker.invalid" }},
		{name: "audience", change: func(c jwt.MapClaims) { c["aud"] = "oaiapp_other" }},
		{name: "nonce", change: func(c jwt.MapClaims) { c["nonce"] = "wrong" }},
		{name: "expiry", change: func(c jwt.MapClaims) { c["exp"] = now.Add(-time.Minute).Unix() }},
		{name: "missing subject", change: func(c jwt.MapClaims) { delete(c, "sub") }},
		{name: "multiple audiences without azp", change: func(c jwt.MapClaims) { c["aud"] = []string{clientID, "oaiapp_other"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := jwt.MapClaims{}
			for name, value := range base {
				claims[name] = value
			}
			tt.change(claims)
			if _, err := m.verifyIDToken(context.Background(), signClaims(t, key, kid, claims), clientID, nonce); err == nil {
				t.Fatal("invalid identity was accepted")
			}
		})
	}
}

func TestDockerCallbackBindKeepsRedirectHostFence(t *testing.T) {
	a := &attempt{redirectURI: "http://127.0.0.1:1455/auth/callback"}
	r := httptest.NewRequest(http.MethodGet, a.redirectURI, nil)
	r.RemoteAddr = "172.18.0.1:54321"
	m := &Manager{config: managerConfig{callbackBind: "127.0.0.1"}}
	if m.validCallbackPeer(a, r) {
		t.Fatal("default callback bind accepted a non-loopback peer")
	}
	m.config.callbackBind = "0.0.0.0"
	if !m.validCallbackPeer(a, r) {
		t.Fatal("explicit Docker callback bind rejected a bridge peer")
	}
	r.Host = "example.com:1455"
	if m.validCallbackPeer(a, r) {
		t.Fatal("Docker callback bind accepted a non-loopback Host header")
	}
}

func signedIDToken(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience, subject, nonce string, now time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":                         issuer,
		"aud":                         audience,
		"sub":                         subject,
		"iat":                         now.Unix(),
		"exp":                         now.Add(4 * time.Hour).Unix(),
		"email":                       "owner@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro"},
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	return signClaims(t, key, kid, claims)
}

func signClaims(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func rsaJWK(kid string, key *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kid": kid,
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
	}
}

func pkceChallenge(verifier string) string {
	sum := sha256Sum([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum)
}

func sha256Sum(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("write JSON: %v", err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
