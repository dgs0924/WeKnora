// Package codex talks to the official Codex app-server over its stdio JSON-RPC
// protocol. It deliberately never reads Codex credential files: authentication
// remains wholly owned by the installed Codex CLI.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout   = 2 * time.Minute
	defaultMaxOutput = 4 << 20
	maxLineSize      = 1 << 20
	protocolClient   = "weknora"
)

// Config is deployment-owned. Callers cannot override Binary, Home or WorkDir
// per request, which prevents user prompts or model rows from selecting a
// different executable or credential directory.
type Config struct {
	Binary         string
	Home           string
	WorkDir        string
	Timeout        time.Duration
	MaxOutputBytes int
	MaxConcurrent  int
}

// Status contains only account facts returned by account/read. It never
// exposes credentials or the raw app-server response.
type Status struct {
	LoggedIn bool   `json:"logged_in"`
	Email    string `json:"email,omitempty"`
	Plan     string `json:"plan,omitempty"`
}

// Model is the safe subset of model/list exposed to WeKnora.
type Model struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	DisplayName      string   `json:"display_name"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	ReasoningEfforts []string `json:"reasoning_efforts,omitempty"`
}

// Runtime is safe for concurrent requests. Each request receives an isolated
// ephemeral app-server thread and an empty, deployment-configured workdir.
type Runtime struct {
	cfg  Config
	gate chan struct{}
}

func New(cfg Config) (*Runtime, error) {
	if !processGroupsSupported {
		return nil, errors.New("server Codex integration requires a Unix host")
	}
	if strings.TrimSpace(cfg.Binary) == "" || strings.TrimSpace(cfg.Home) == "" || strings.TrimSpace(cfg.WorkDir) == "" {
		return nil, errors.New("codex binary, home and work directory are required")
	}
	if !filepath.IsAbs(cfg.Home) || !filepath.IsAbs(cfg.WorkDir) {
		return nil, errors.New("codex home and work directory must be absolute")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutput
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	return &Runtime{cfg: cfg, gate: make(chan struct{}, cfg.MaxConcurrent)}, nil
}

func (r *Runtime) acquire(ctx context.Context) error {
	select {
	case r.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) release() { <-r.gate }

// Status asks the official app-server for non-secret account metadata.
func (r *Runtime) Status(ctx context.Context) (Status, error) {
	if err := r.acquire(ctx); err != nil {
		return Status{}, err
	}
	defer r.release()
	c, err := r.start(ctx)
	if err != nil {
		return Status{}, err
	}
	defer c.close()
	var out struct {
		RequiresOpenAIAuth bool `json:"requiresOpenaiAuth"`
		Account            *struct {
			Type  string  `json:"type"`
			Email *string `json:"email"`
			Plan  string  `json:"planType"`
		} `json:"account"`
	}
	if err := c.call(ctx, "account/read", map[string]any{"refreshToken": false}, &out); err != nil {
		return Status{}, err
	}
	if out.Account == nil || out.Account.Type != "chatgpt" {
		return Status{LoggedIn: false}, nil
	}
	// requiresOpenaiAuth describes the provider's authentication requirement,
	// not whether this app-server has an authenticated ChatGPT account.
	s := Status{LoggedIn: true, Plan: out.Account.Plan}
	if out.Account.Email != nil {
		s.Email = *out.Account.Email
	}
	return s, nil
}

// Models returns the complete model/list result using pagination.
func (r *Runtime) Models(ctx context.Context) ([]Model, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	defer r.release()
	c, err := r.start(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	var all []Model
	var cursor *string
	for {
		var out struct {
			Data []struct {
				ID                        string   `json:"id"`
				Model                     string   `json:"model"`
				DisplayName               string   `json:"displayName"`
				Hidden                    bool     `json:"hidden"`
				InputModalities           []string `json:"inputModalities"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := c.call(ctx, "model/list", map[string]any{"cursor": cursor, "includeHidden": false, "limit": 100}, &out); err != nil {
			return nil, err
		}
		for _, m := range out.Data {
			if !m.Hidden {
				item := Model{ID: m.ID, Name: m.Model, DisplayName: m.DisplayName, InputModalities: m.InputModalities}
				for _, e := range m.SupportedReasoningEfforts {
					item.ReasoningEfforts = append(item.ReasoningEfforts, e.ReasoningEffort)
				}
				all = append(all, item)
			}
		}
		if out.NextCursor == nil || *out.NextCursor == "" {
			return all, nil
		}
		cursor = out.NextCursor
	}
}

type rpcClient struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Scanner
	stderr *limitedBuffer
	cfg    Config
	cancel context.CancelFunc
	nextID int64
	mu     sync.Mutex
}
type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (r *Runtime) start(parent context.Context) (*rpcClient, error) {
	ctx, cancel := context.WithTimeout(parent, r.cfg.Timeout)
	args := []string{"app-server", "--listen", "stdio://", "-c", "web_search=\"disabled\"", "-c", "tools.experimental_request_user_input.enabled=false", "-c", "features.shell_tool=false", "-c", "features.unified_exec=false", "-c", "features.apps=false", "-c", "features.multi_agent=false", "-c", "features.shell_snapshot=false", "-c", "features.goals=false", "-c", "features.apply_patch_freeform=false", "-c", "features.browser_use=false", "-c", "features.computer_use=false", "-c", "features.plugins=false", "-c", "features.hooks=false", "-c", "features.image_generation=false", "-c", "features.worktrees=false", "-c", "features.memories=false", "-c", "features.skill_search=false", "-c", "features.sleep_tool=false", "-c", "features.view_image=false", "-c", "features.workspace_dependencies=false"}
	cmd := exec.CommandContext(ctx, r.cfg.Binary, args...)
	cmd.Dir = r.cfg.WorkDir
	configureProcessGroup(cmd)
	// Do not inherit API keys. Keep only transport/CA locale variables required
	// by a containerized official CLI plus deployment-owned CODEX_HOME/PATH.
	env := []string{"CODEX_HOME=" + r.cfg.Home, "PATH=" + os.Getenv("PATH")}
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "LANG", "LC_ALL"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	cmd.Env = env
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	buf := &limitedBuffer{limit: r.cfg.MaxOutputBytes}
	go func() { _, _ = io.Copy(buf, io.LimitReader(stderr, int64(r.cfg.MaxOutputBytes)+1)) }()
	s := bufio.NewScanner(io.LimitReader(stdout, int64(r.cfg.MaxOutputBytes)+1))
	s.Buffer(make([]byte, 16<<10), maxLineSize)
	c := &rpcClient{cmd: cmd, in: in, out: s, stderr: buf, cfg: r.cfg, cancel: cancel}
	var init map[string]any
	if err := c.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": protocolClient, "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, &init); err != nil {
		c.close()
		return nil, err
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *rpcClient) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = c.in.Write(append(b, '\n'))
	return err
}

func (c *rpcClient) close() {
	if c.cancel != nil {
		c.cancel()
	}
	killProcessGroup(c.cmd)
	_ = c.in.Close()
	_ = c.cmd.Wait()
}
func (c *rpcClient) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	req := map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params}
	raw, _ := json.Marshal(req)
	if _, err := c.in.Write(append(raw, '\n')); err != nil {
		return c.err("write request", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !c.out.Scan() {
			if err := c.out.Err(); err != nil {
				return c.err("read response", err)
			}
			return c.err("app-server closed", io.EOF)
		}
		var msg rpcEnvelope
		if err := json.Unmarshal(c.out.Bytes(), &msg); err != nil {
			return c.err("decode response", err)
		}
		if msg.Method != "" {
			if err := c.rejectServerRequest(msg); err != nil {
				return err
			}
			continue
		}
		if len(msg.ID) == 0 || string(msg.ID) != fmt.Sprintf("%d", c.nextID) {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("Codex app-server %s rejected the request", method)
		}
		if out != nil {
			if err := json.Unmarshal(msg.Result, out); err != nil {
				return c.err("decode result", err)
			}
		}
		return nil
	}
}
func (c *rpcClient) rejectServerRequest(msg rpcEnvelope) error {
	if len(msg.ID) == 0 {
		return nil
	}
	var result any
	switch msg.Method {
	case "item/tool/call":
		result = map[string]any{"success": false, "contentItems": []any{map[string]any{"type": "inputText", "text": "Dynamic tools are unavailable in this runtime."}}}
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "applyPatchApproval", "permissions/requestApproval":
		result = map[string]any{"decision": "decline"}
	case "tool/requestUserInput", "mcpServer/elicitation/request", "account/chatgptAuthTokens/refresh":
		// Explicit denial: WeKnora never lets app-server execute, read, patch,
		// ask users, refresh credentials, or invoke MCP servers on its behalf.
		result = map[string]any{"cancelled": true}
	default:
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID), "error": map[string]any{"code": -32601, "message": "request disabled by WeKnora Codex runtime"}})
		_, err := c.in.Write(append(b, '\n'))
		return err
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID), "result": result})
	_, err := c.in.Write(append(b, '\n'))
	return err
}

// stderr is intentionally not returned. It can contain local paths or
// provider diagnostics, and neither belongs in a user-visible model error.
func (c *rpcClient) err(action string, cause error) error {
	return fmt.Errorf("Codex app-server %s: %w", action, cause)
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
	mu    sync.Mutex
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remain := b.limit - b.Buffer.Len()
	if remain > 0 {
		if len(p) > remain {
			_, _ = b.Buffer.Write(p[:remain])
		} else {
			_, _ = b.Buffer.Write(p)
		}
	}
	return len(p), nil
}
func (b *limitedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	lower := strings.ToLower(s)
	for _, token := range []string{"access_token", "refresh_token", "authorization", "api_key", "bearer ", "sk-"} {
		if strings.Contains(lower, token) {
			return "Codex request failed (sensitive diagnostic redacted)"
		}
	}
	if len(s) > 512 {
		s = s[:512]
	}
	return strings.TrimSpace(s)
}
