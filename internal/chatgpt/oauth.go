package chatgpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type tokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    int64   `json:"expires_in"`
	IDToken      string  `json:"id_token"`
	Scope        *string `json:"scope"`
	TokenType    string  `json:"token_type"`
}

type tokenRequestError struct {
	status       int
	invalidGrant bool
}

func (e *tokenRequestError) Error() string {
	return fmt.Sprintf("ChatGPT credential service returned HTTP %d", e.status)
}

func (m *Manager) handleCallback(userHash string, a *attempt, w http.ResponseWriter, r *http.Request) {
	setCallbackHeaders(w)
	if r.Method != http.MethodGet || r.URL.Path != callbackPath || !m.validCallbackPeer(a, r) {
		writeCallbackPage(w, http.StatusBadRequest, false)
		return
	}
	state, stateOK := singleQueryValue(r.URL.Query(), "state", true)
	code, codeOK := singleQueryValue(r.URL.Query(), "code", false)
	providerError, errorOK := singleQueryValue(r.URL.Query(), "error", false)
	returnedClientID, clientOK := singleQueryValue(r.URL.Query(), "client_id", false)
	if !stateOK || !codeOK || !errorOK || !clientOK {
		writeCallbackPage(w, http.StatusBadRequest, false)
		return
	}

	m.mu.Lock()
	if m.pending[userHash] != a || a.status != AttemptPending || a.consumed ||
		!m.config.now().Before(a.expiresAt) || !constantTimeEqual(state, a.state) {
		m.mu.Unlock()
		writeCallbackPage(w, http.StatusBadRequest, false)
		return
	}
	if providerError != "" {
		a.consumed = true
		m.finishAttemptLocked(a, AttemptFailed, "Authorization was declined or failed.")
		m.mu.Unlock()
		writeCallbackPage(w, http.StatusBadRequest, false)
		return
	}
	if code == "" {
		m.mu.Unlock()
		writeCallbackPage(w, http.StatusBadRequest, false)
		return
	}
	clientID := a.clientID
	if clientID == dynamicClientID {
		if !validIssuedClientID(returnedClientID) {
			a.consumed = true
			m.finishAttemptLocked(a, AttemptFailed, "ChatGPT returned an invalid client registration.")
			m.mu.Unlock()
			writeCallbackPage(w, http.StatusBadRequest, false)
			return
		}
		clientID = returnedClientID
	} else if returnedClientID != "" && returnedClientID != clientID {
		a.consumed = true
		m.finishAttemptLocked(a, AttemptFailed, "ChatGPT returned a different client registration.")
		m.mu.Unlock()
		writeCallbackPage(w, http.StatusBadRequest, false)
		return
	}
	a.consumed = true
	verifier := a.verifier
	nonce := a.nonce
	expectedSubject := a.expectedSub
	redirectURI := a.redirectURI
	expiresAt := a.expiresAt
	m.mu.Unlock()

	requestTimeout := 30 * time.Second
	if remaining := expiresAt.Sub(m.config.now()); remaining < requestTimeout {
		requestTimeout = remaining
	}
	if requestTimeout <= 0 {
		m.failAttempt(userHash, a, AttemptExpired, "Authorization expired. Start again.")
		writeCallbackPage(w, http.StatusBadRequest, false)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	value, err := m.exchangeCode(ctx, userHash, clientID, code, verifier, redirectURI, nonce, expectedSubject)
	if err != nil {
		publicError := "ChatGPT connection failed. Start again."
		if errors.Is(err, ErrTokenSharingUnavailable) {
			publicError = "ChatGPT plan access was not granted. Start again and allow token sharing."
		}
		m.failAttempt(userHash, a, AttemptFailed, publicError)
		writeCallbackPage(w, http.StatusBadGateway, false)
		return
	}

	gate := m.userLock(userHash)
	gate.Lock()
	m.mu.Lock()
	if m.pending[userHash] != a || a.status != AttemptPending || !a.consumed {
		m.mu.Unlock()
		gate.Unlock()
		writeCallbackPage(w, http.StatusConflict, false)
		return
	}
	if err := m.saveCredential(value); err != nil {
		m.finishAttemptLocked(a, AttemptFailed, "ChatGPT connection could not be saved.")
		m.mu.Unlock()
		gate.Unlock()
		writeCallbackPage(w, http.StatusInternalServerError, false)
		return
	}
	m.finishAttemptLocked(a, AttemptSucceeded, "")
	m.mu.Unlock()
	gate.Unlock()
	writeCallbackPage(w, http.StatusOK, true)
}

func (m *Manager) validCallbackPeer(a *attempt, r *http.Request) bool {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || (m.config.callbackBind == "127.0.0.1" && !net.ParseIP(remoteHost).IsLoopback()) {
		return false
	}
	expected, err := url.Parse(a.redirectURI)
	if err != nil {
		return false
	}
	return r.Host == expected.Host
}

func singleQueryValue(values url.Values, name string, required bool) (string, bool) {
	items, exists := values[name]
	if !exists {
		return "", !required
	}
	if len(items) != 1 || (required && items[0] == "") || len(items[0]) > 8192 {
		return "", false
	}
	return items[0], true
}

func (m *Manager) failAttempt(userHash string, a *attempt, status AttemptStatus, publicError string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending[userHash] == a && a.status == AttemptPending {
		m.finishAttemptLocked(a, status, publicError)
	}
}

func setCallbackHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Connection", "close")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func writeCallbackPage(w http.ResponseWriter, status int, success bool) {
	title := "ChatGPT 连接失败"
	message := "请返回 WeKnora 查看详情后重试。"
	if success {
		title = "ChatGPT 已连接"
		message = "现在可以关闭此窗口并返回 WeKnora。"
	}
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><html><head><meta charset=utf-8><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>", html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

func (m *Manager) exchangeCode(ctx context.Context, userHash, clientID, code, verifier, redirectURI, nonce, expectedSubject string) (*credential, error) {
	response, err := m.requestTokens(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
		"resource":      {m.config.resource},
	})
	if err != nil {
		return nil, err
	}
	if response.RefreshToken == "" || response.IDToken == "" || response.Scope == nil || !hasRequiredScopes(*response.Scope) {
		return nil, ErrTokenSharingUnavailable
	}
	identity, err := m.verifyIDToken(ctx, response.IDToken, clientID, nonce)
	if err != nil {
		return nil, err
	}
	if expectedSubject != "" && identity.subject != expectedSubject {
		return nil, errors.New("ChatGPT account identity changed")
	}
	now := m.config.now().UTC()
	return &credential{
		Version:      credentialVersion,
		UserHash:     userHash,
		HostID:       m.host.HostID,
		ClientID:     clientID,
		Subject:      identity.subject,
		Email:        identity.email,
		Plan:         identity.plan,
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		IDToken:      response.IDToken,
		GrantedScope: *response.Scope,
		ExpiresAt:    now.Add(time.Duration(response.ExpiresIn) * time.Second),
		ConnectedAt:  now,
	}, nil
}

func (m *Manager) refresh(ctx context.Context, previous *credential) (*credential, error) {
	response, err := m.requestTokens(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {previous.ClientID},
		"refresh_token": {previous.RefreshToken},
		"resource":      {m.config.resource},
	})
	if err != nil {
		var requestError *tokenRequestError
		if errors.As(err, &requestError) && requestError.invalidGrant {
			return nil, ErrReauthenticationNeeded
		}
		return nil, errors.New("ChatGPT credential refresh failed")
	}
	next := *previous
	next.AccessToken = response.AccessToken
	if response.RefreshToken != "" {
		next.RefreshToken = response.RefreshToken
	}
	if response.Scope != nil {
		if !hasRequiredScopes(*response.Scope) {
			return nil, ErrTokenSharingUnavailable
		}
		next.GrantedScope = *response.Scope
	}
	if response.IDToken != "" {
		identity, err := m.verifyIDToken(ctx, response.IDToken, previous.ClientID, "")
		if err != nil || identity.subject != previous.Subject {
			return nil, ErrReauthenticationNeeded
		}
		if identity.email != "" {
			next.Email = identity.email
		}
		if identity.plan != "" {
			next.Plan = identity.plan
		}
		next.IDToken = response.IDToken
	}
	next.ExpiresAt = m.config.now().UTC().Add(time.Duration(response.ExpiresIn) * time.Second)
	return &next, nil
}

func (m *Manager) requestTokens(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.config.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("cannot create ChatGPT credential request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("ChatGPT credential service is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
		return nil, &tokenRequestError{status: resp.StatusCode, invalidGrant: body.Error == "invalid_grant"}
	}
	if resp.ContentLength > maxHTTPJSON {
		return nil, errors.New("ChatGPT credential response is too large")
	}
	var result tokenResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxHTTPJSON+1))
	if err := dec.Decode(&result); err != nil {
		return nil, errors.New("ChatGPT returned an invalid credential response")
	}
	if err := ensureJSONEOF(dec); err != nil {
		return nil, errors.New("ChatGPT returned an invalid credential response")
	}
	if result.AccessToken == "" || result.ExpiresIn <= 0 || result.ExpiresIn > int64((365*24*time.Hour)/time.Second) || !strings.EqualFold(result.TokenType, "bearer") {
		return nil, errors.New("ChatGPT returned an incomplete credential")
	}
	return &result, nil
}
