package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultIssuer        = "https://auth.openai.com"
	defaultAuthorizeURL  = defaultIssuer + "/api/accounts/authorize"
	defaultTokenURL      = defaultIssuer + "/api/accounts/oauth/token"
	defaultJWKSURL       = defaultIssuer + "/.well-known/jwks.json"
	defaultModelsURL     = "https://api.openai.com/v1/models"
	defaultResource      = "https://api.openai.com/v1"
	dynamicClientID      = "dynamic_agent_client"
	requestedScope       = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
	callbackPath         = "/auth/callback"
	defaultCallbackPort  = 1455
	authorizationTimeout = 10 * time.Minute
	refreshSkew          = 5 * time.Minute
	maxHTTPJSON          = 2 << 20
)

var issuedClientIDPattern = regexp.MustCompile(`^oaiapp_[A-Za-z0-9_-]{1,200}$`)

type managerConfig struct {
	issuer       string
	authorizeURL string
	tokenURL     string
	jwksURL      string
	modelsURL    string
	resource     string
	callbackBind string
	callbackPort int
	httpClient   *http.Client
	now          func() time.Time
}

type Manager struct {
	storageDir string
	host       hostRecord
	config     managerConfig
	httpClient *http.Client

	mu       sync.Mutex
	pending  map[string]*attempt
	userGate [64]sync.Mutex

	jwksMu       sync.Mutex
	jwks         map[string]*rsa.PublicKey
	jwksLoadedAt time.Time
}

type attempt struct {
	id          string
	state       string
	nonce       string
	verifier    string
	redirectURI string
	clientID    string
	expectedSub string
	userHash    string
	expiresAt   time.Time
	status      AttemptStatus
	publicError string
	consumed    bool
	listener    net.Listener
	server      *http.Server
	expiryTimer *time.Timer
}

func NewManager(storageDir string) (*Manager, error) {
	port, err := callbackPortFromEnvironment()
	if err != nil {
		return nil, err
	}
	bind, err := callbackBindFromEnvironment()
	if err != nil {
		return nil, err
	}
	return newManager(storageDir, managerConfig{
		issuer:       defaultIssuer,
		authorizeURL: defaultAuthorizeURL,
		tokenURL:     defaultTokenURL,
		jwksURL:      defaultJWKSURL,
		modelsURL:    defaultModelsURL,
		resource:     defaultResource,
		callbackBind: bind,
		callbackPort: port,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		now:          time.Now,
	})
}

func newManager(storageDir string, config managerConfig) (*Manager, error) {
	if strings.TrimSpace(storageDir) == "" {
		return nil, errors.New("ChatGPT credential storage directory is required")
	}
	abs, err := filepath.Abs(storageDir)
	if err != nil {
		return nil, fmt.Errorf("resolve ChatGPT credential storage: %w", err)
	}
	if err := ensurePrivateDir(abs); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(filepath.Join(abs, "users")); err != nil {
		return nil, err
	}
	if config.now == nil {
		config.now = time.Now
	}
	if config.httpClient == nil {
		config.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if config.callbackBind == "" {
		config.callbackBind = "127.0.0.1"
	}
	if config.callbackBind != "127.0.0.1" && config.callbackBind != "0.0.0.0" {
		return nil, errors.New("invalid ChatGPT callback bind address")
	}
	client := *config.httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout <= 0 {
		client.Timeout = 30 * time.Second
	}
	m := &Manager{
		storageDir: abs,
		config:     config,
		httpClient: &client,
		pending:    make(map[string]*attempt),
		jwks:       make(map[string]*rsa.PublicKey),
	}
	host, err := m.loadOrCreateHost()
	if err != nil {
		return nil, err
	}
	m.host = host
	return m, nil
}

func callbackPortFromEnvironment() (int, error) {
	raw := strings.TrimSpace(os.Getenv("CHATGPT_CALLBACK_PORT"))
	if raw == "" {
		return defaultCallbackPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 0 || port > 65535 {
		return 0, errors.New("CHATGPT_CALLBACK_PORT must be an integer from 0 to 65535")
	}
	return port, nil
}

func callbackBindFromEnvironment() (string, error) {
	value := strings.TrimSpace(os.Getenv("CHATGPT_CALLBACK_BIND"))
	if value == "" {
		return "127.0.0.1", nil
	}
	if value != "127.0.0.1" && value != "0.0.0.0" {
		return "", errors.New("CHATGPT_CALLBACK_BIND must be 127.0.0.1 or 0.0.0.0")
	}
	return value, nil
}

func (m *Manager) userLock(userHash string) *sync.Mutex {
	index, _ := strconv.ParseUint(userHash[:2], 16, 8)
	return &m.userGate[index%uint64(len(m.userGate))]
}

func (m *Manager) Start(ctx context.Context, userID string) (StartResult, error) {
	if err := ctx.Err(); err != nil {
		return StartResult{}, err
	}
	userHash, err := validateUserID(userID)
	if err != nil {
		return StartResult{}, err
	}
	gate := m.userLock(userHash)
	gate.Lock()
	defer gate.Unlock()
	credential, err := m.loadCredential(userHash)
	if err != nil {
		return StartResult{}, err
	}
	clientID := dynamicClientID
	if credential != nil {
		clientID = credential.ClientID
	}

	m.mu.Lock()
	if previous := m.pending[userHash]; previous != nil && previous.status == AttemptPending {
		m.finishAttemptLocked(previous, AttemptCancelled, "Authorization was cancelled.")
	}
	m.mu.Unlock()

	listener, err := net.Listen("tcp4", net.JoinHostPort(m.config.callbackBind, strconv.Itoa(m.config.callbackPort)))
	if err != nil {
		return StartResult{}, errors.New("cannot start the local ChatGPT callback listener")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := "http://127.0.0.1:" + strconv.Itoa(port) + callbackPath
	state, err := randomURLToken(32)
	if err != nil {
		_ = listener.Close()
		return StartResult{}, err
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		_ = listener.Close()
		return StartResult{}, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		_ = listener.Close()
		return StartResult{}, err
	}
	attemptID, err := randomURLToken(18)
	if err != nil {
		_ = listener.Close()
		return StartResult{}, err
	}
	now := m.config.now().UTC()
	a := &attempt{
		id:          attemptID,
		state:       state,
		nonce:       nonce,
		verifier:    verifier,
		redirectURI: redirectURI,
		clientID:    clientID,
		userHash:    userHash,
		expiresAt:   now.Add(authorizationTimeout),
		status:      AttemptPending,
		listener:    listener,
	}
	if credential != nil {
		a.expectedSub = credential.Subject
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		m.handleCallback(userHash, a, w, r)
	})
	a.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	m.mu.Lock()
	m.pending[userHash] = a
	a.expiryTimer = time.AfterFunc(a.expiresAt.Sub(now), func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.pending[userHash] == a && a.status == AttemptPending {
			m.finishAttemptLocked(a, AttemptExpired, "Authorization expired. Start again.")
		}
	})
	m.mu.Unlock()
	go func() { _ = a.server.Serve(listener) }()

	challenge := sha256.Sum256([]byte(verifier))
	values := url.Values{
		"client_id":             {clientID},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"ext_agent_host_id":     {m.host.HostID},
		"nonce":                 {nonce},
		"redirect_uri":          {redirectURI},
		"resource":              {m.config.resource},
		"response_type":         {"code"},
		"scope":                 {requestedScope},
		"state":                 {state},
	}
	if clientID == dynamicClientID {
		values.Set("agent_name_hint", "WeKnora")
	} else if credential != nil {
		if credential.Email != "" {
			values.Set("login_hint", credential.Email)
		}
	}
	authorizationURL := m.config.authorizeURL + "?" + values.Encode()
	return StartResult{AuthorizationURL: authorizationURL, AttemptID: attemptID, ExpiresAt: a.expiresAt}, nil
}

func (m *Manager) Status(ctx context.Context, userID string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	userHash, err := validateUserID(userID)
	if err != nil {
		return Status{}, err
	}
	gate := m.userLock(userHash)
	gate.Lock()
	value, err := m.loadCredential(userHash)
	gate.Unlock()
	if err != nil {
		return Status{}, err
	}
	status := Status{}
	if value != nil {
		status.Connected = true
		status.Account = &AccountStatus{
			SubjectHint: redactSubject(value.Subject),
			EmailHint:   value.Email,
			Plan:        value.Plan,
			PlanEnabled: hasRequiredScopes(value.GrantedScope),
			ExpiresAt:   value.ExpiresAt,
			ConnectedAt: value.ConnectedAt,
		}
	}
	m.mu.Lock()
	if a := m.pending[userHash]; a != nil {
		if a.status == AttemptPending && !m.config.now().Before(a.expiresAt) {
			m.finishAttemptLocked(a, AttemptExpired, "Authorization expired. Start again.")
		}
		status.Pending = &PendingStatus{AttemptID: a.id, Status: a.status, ExpiresAt: a.expiresAt, Error: a.publicError}
	}
	m.mu.Unlock()
	return status, nil
}

func (m *Manager) AccessToken(ctx context.Context, userID string) (string, error) {
	userHash, err := validateUserID(userID)
	if err != nil {
		return "", err
	}
	gate := m.userLock(userHash)
	gate.Lock()
	defer gate.Unlock()
	value, err := m.loadCredential(userHash)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", ErrNotConnected
	}
	if m.config.now().Add(refreshSkew).Before(value.ExpiresAt) {
		return value.AccessToken, nil
	}
	refreshed, err := m.refresh(ctx, value)
	if err != nil {
		return "", err
	}
	if err := m.saveCredential(refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (m *Manager) Models(ctx context.Context, userID string) ([]Model, error) {
	token, err := m.AccessToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.config.modelsURL, nil)
	if err != nil {
		return nil, errors.New("cannot create the ChatGPT model catalog request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	var response struct {
		Models json.RawMessage `json:"models"`
	}
	if err := m.doJSON(req, &response); err != nil || response.Models == nil {
		return nil, errors.New("ChatGPT model catalog is unavailable")
	}
	var rows []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
		Visibility  string `json:"visibility"`
	}
	if err := json.Unmarshal(response.Models, &rows); err != nil {
		return nil, errors.New("ChatGPT model catalog is unavailable")
	}
	seen := make(map[string]struct{})
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		slug := strings.TrimSpace(row.Slug)
		if row.Visibility != "list" || slug == "" || len(slug) > 512 {
			continue
		}
		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}
		name := strings.TrimSpace(row.DisplayName)
		if name == "" || len(name) > 512 {
			name = slug
		}
		models = append(models, Model{Slug: slug, DisplayName: name})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].DisplayName == models[j].DisplayName {
			return models[i].Slug < models[j].Slug
		}
		return models[i].DisplayName < models[j].DisplayName
	})
	return models, nil
}

func (m *Manager) Disconnect(ctx context.Context, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	userHash, err := validateUserID(userID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if a := m.pending[userHash]; a != nil {
		if a.status == AttemptPending {
			m.finishAttemptLocked(a, AttemptCancelled, "Authorization was cancelled.")
		} else {
			delete(m.pending, userHash)
		}
	}
	m.mu.Unlock()
	gate := m.userLock(userHash)
	gate.Lock()
	defer gate.Unlock()
	return m.deleteCredential(userHash)
}

func (m *Manager) finishAttemptLocked(a *attempt, status AttemptStatus, publicError string) {
	if a.status != AttemptPending {
		return
	}
	a.status = status
	a.publicError = publicError
	if a.expiryTimer != nil {
		a.expiryTimer.Stop()
	}
	if a.listener != nil {
		_ = a.listener.Close()
	}
	clearAttemptSecrets(a)
}

func clearAttemptSecrets(a *attempt) {
	a.state = ""
	a.nonce = ""
	a.verifier = ""
	a.expectedSub = ""
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", errors.New("cannot create secure OAuth state")
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func validIssuedClientID(value string) bool {
	return issuedClientIDPattern.MatchString(value)
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func hasRequiredScopes(scope string) bool {
	granted := make(map[string]bool)
	for _, value := range strings.Fields(scope) {
		granted[value] = true
	}
	return granted["resource.invoke"] && granted["chatgpt.tokens.use.direct"]
}

func redactSubject(subject string) string {
	if len(subject) <= 10 {
		return "***"
	}
	return subject[:6] + "…" + subject[len(subject)-4:]
}

func (m *Manager) doJSON(req *http.Request, dst any) error {
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxHTTPJSON))
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxHTTPJSON {
		return errors.New("response too large")
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxHTTPJSON+1))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return ensureJSONEOF(dec)
}
