package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	maxCodexImageCount = 20
	maxCodexImageBytes = 20 << 20
	maxCodexImageTotal = 50 << 20
)

func (r *Runtime) Chat(name, id string) chat.Chat {
	return r.ChatAuthorized(name, id, nil)
}

// ChatAuthorized rechecks application permissions after any wait for the
// runtime gate. A queued request must not use a grant revoked while it waited.
func (r *Runtime) ChatAuthorized(name, id string, authorize func(context.Context) error) chat.Chat {
	return &modelChat{r: r, name: name, id: id, authorize: authorize}
}

type modelChat struct {
	r         *Runtime
	name, id  string
	authorize func(context.Context) error
}

func (c *modelChat) GetModelName() string { return c.name }
func (c *modelChat) GetModelID() string   { return c.id }
func (c *modelChat) Chat(ctx context.Context, ms []chat.Message, o *chat.ChatOptions) (*types.ChatResponse, error) {
	ch, e := c.ChatStream(ctx, ms, o)
	if e != nil {
		return nil, e
	}
	var r types.ChatResponse
	finished := false
	calledTool := false
	for v := range ch {
		if v.ResponseType == types.ResponseTypeError {
			return nil, fmt.Errorf("Codex: %s", v.Content)
		}
		if v.ResponseType == types.ResponseTypeAnswer {
			r.Content += v.Content
			r.FinishReason = v.FinishReason
			finished = finished || v.Done
		}
		if v.ResponseType == types.ResponseTypeToolCall {
			r.ToolCalls = append(r.ToolCalls, v.ToolCalls...)
			r.FinishReason = "tool_calls"
			calledTool = len(v.ToolCalls) > 0
		}
	}
	if !finished && !calledTool {
		return nil, fmt.Errorf("Codex stream ended before completion")
	}
	if r.FinishReason == "" {
		r.FinishReason = "stop"
	}
	return &r, nil
}
func (c *modelChat) ChatStream(ctx context.Context, ms []chat.Message, o *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	history, input, ins, visionBytes, e := buildTurn(ms)
	if e != nil {
		return nil, e
	}
	toolDefs, allowed, e := validateTools(o)
	if e != nil {
		return nil, e
	}
	// Queue waiting is bounded by the caller, not the model execution budget.
	// Wiki fans out several requests against the same account gate.
	if e = c.r.acquire(ctx); e != nil {
		return nil, e
	}
	callCtx, cancel := context.WithTimeout(ctx, c.r.cfg.Timeout)
	if c.authorize != nil {
		if e = c.authorize(callCtx); e != nil {
			c.r.release()
			cancel()
			return nil, e
		}
	}
	out := make(chan types.StreamResponse, 32)
	go func() {
		defer cancel()
		defer c.r.release()
		defer close(out)
		x, e := c.r.start(callCtx, visionBytes)
		if e != nil {
			emitError(callCtx, out, e)
			return
		}
		defer x.close()
		p := map[string]any{"cwd": c.r.cfg.WorkDir, "ephemeral": true, "model": c.name, "sandbox": "read-only", "approvalPolicy": "untrusted", "developerInstructions": ins, "config": map[string]any{"tools": map[string]any{"shell": false, "apply_patch": false, "web_search": false, "mcp": false}}}
		if len(toolDefs) > 0 {
			p["dynamicTools"] = toolDefs
		}
		var s struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if e = x.call(callCtx, "thread/start", p, &s); e != nil {
			emitError(callCtx, out, e)
			return
		}
		if s.Thread.ID == "" {
			emitError(callCtx, out, fmt.Errorf("Codex app-server did not return a thread id"))
			return
		}
		if len(history) > 0 {
			if e = x.call(callCtx, "thread/inject_items", map[string]any{"threadId": s.Thread.ID, "items": history}, nil); e != nil {
				emitError(callCtx, out, e)
				return
			}
		}
		if e = x.turn(callCtx, s.Thread.ID, input, o, allowed, out); e != nil {
			emitError(callCtx, out, e)
		}
	}()
	return out, nil
}
func tools(o *chat.ChatOptions) []chat.Tool {
	if o == nil {
		return nil
	}
	return o.Tools
}
func dynamicTools(ts []chat.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(ts))
	for _, t := range ts {
		if t.Type != "function" || t.Function.Name == "" {
			continue
		}
		var s any = map[string]any{"type": "object"}
		if len(t.Function.Parameters) > 0 {
			_ = json.Unmarshal(t.Function.Parameters, &s)
		}
		out = append(out, map[string]any{"type": "function", "name": t.Function.Name, "description": t.Function.Description, "inputSchema": s})
	}
	return out
}

func validateTools(o *chat.ChatOptions) ([]map[string]any, map[string]struct{}, error) {
	if o == nil || len(o.Tools) == 0 {
		return nil, nil, nil
	}
	if o.ToolChoice == "none" {
		return nil, nil, nil
	}
	if o.ToolChoice == "required" || (o.ToolChoice != "" && o.ToolChoice != "auto") {
		return nil, nil, fmt.Errorf("Codex app-server adapter does not support forced tool choice")
	}
	allowed := make(map[string]struct{}, len(o.Tools))
	for _, t := range o.Tools {
		if t.Type != "function" || t.Function.Name == "" || len(t.Function.Parameters) == 0 {
			return nil, nil, fmt.Errorf("invalid dynamic function tool definition")
		}
		var schema any
		if err := json.Unmarshal(t.Function.Parameters, &schema); err != nil {
			return nil, nil, fmt.Errorf("invalid dynamic function schema for %s", t.Function.Name)
		}
		allowed[t.Function.Name] = struct{}{}
	}
	return dynamicTools(o.Tools), allowed, nil
}

// buildTurn converts WeKnora's protocol-neutral messages into the two shapes
// expected by the Codex app-server: Turn input uses {type:"image",url:...},
// while injected Responses history uses input_image parts. Only data image URIs
// are accepted here. The application layer turns trusted upload bytes into data
// URIs, so this adapter never reads a local path or fetches a remote URL.
func buildTurn(ms []chat.Message) ([]any, []any, string, int, error) {
	var history []any
	var sy []string
	imageBudget := &codexImageBudget{}
	last := -1
	for i, m := range ms {
		if m.Role != "system" {
			last = i
		}
	}
	if last < 0 {
		return nil, nil, "", 0, fmt.Errorf("Codex chat needs a message")
	}
	input := []any{map[string]any{"type": "text", "text": "Continue from the supplied conversation."}}
	for i, m := range ms {
		text, images, err := codexMessageContent(m, imageBudget)
		if err != nil {
			return nil, nil, "", 0, err
		}
		if m.Role == "system" {
			if len(images) > 0 {
				return nil, nil, "", 0, fmt.Errorf("Codex system messages cannot contain images")
			}
			sy = append(sy, text)
			continue
		}
		if i == last && m.Role == "user" && len(m.ToolCalls) == 0 {
			input = []any{map[string]any{"type": "text", "text": text}}
			for _, image := range images {
				input = append(input, map[string]any{"type": "image", "url": image})
			}
			continue
		}
		switch m.Role {
		case "user", "assistant":
			if len(images) > 0 && m.Role != "user" {
				return nil, nil, "", 0, fmt.Errorf("Codex assistant messages cannot contain images")
			}
			if text != "" || len(images) > 0 {
				kind := "input_text"
				if m.Role == "assistant" {
					kind = "output_text"
				}
				content := make([]any, 0, 1+len(images))
				if text != "" {
					content = append(content, map[string]any{"type": kind, "text": text})
				}
				for _, image := range images {
					content = append(content, map[string]any{"type": "input_image", "image_url": image})
				}
				history = append(history, map[string]any{"type": "message", "role": m.Role, "content": content})
			}
			for _, tc := range m.ToolCalls {
				history = append(history, map[string]any{"type": "function_call", "call_id": tc.ID, "name": tc.Function.Name, "arguments": tc.Function.Arguments})
			}
		case "tool":
			if m.ToolCallID == "" {
				return nil, nil, "", 0, fmt.Errorf("tool result has no tool_call_id")
			}
			if len(images) > 0 {
				return nil, nil, "", 0, fmt.Errorf("Codex tool messages cannot contain images")
			}
			history = append(history, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": text})
		default:
			return nil, nil, "", 0, fmt.Errorf("unsupported Codex message role %q", m.Role)
		}
	}
	ins := ""
	if len(sy) > 0 {
		ins = "Application system instructions:\n" + strings.Join(sy, "\n") + "\n\n"
	}
	// This is explanatory only: the process configuration removes the native
	// tools, and WeKnora executes only its own declared dynamic tools.
	ins += `You are a WeKnora assistant. You may reason about only the supplied conversation text, images, and tool results. Never execute commands, inspect files, use built-in tools, browse, access MCP, create subagents, or request permissions. You may request only explicitly provided dynamic tools; WeKnora will execute them.`
	return history, input, ins, imageBudget.total, nil
}

type codexImageBudget struct {
	count int
	total int
}

func codexMessageContent(m chat.Message, budget *codexImageBudget) (string, []string, error) {
	text := m.Content
	images := append([]string(nil), m.Images...)
	for _, part := range m.MultiContent {
		switch part.Type {
		case "text":
			text += part.Text
		case "image_url":
			if part.ImageURL == nil {
				return "", nil, fmt.Errorf("Codex image content is missing its URL")
			}
			images = append(images, part.ImageURL.URL)
		default:
			return "", nil, fmt.Errorf("unsupported Codex content part %q", part.Type)
		}
	}
	for _, image := range images {
		if err := budget.add(image); err != nil {
			return "", nil, err
		}
	}
	return text, images, nil
}

func (b *codexImageBudget) add(raw string) error {
	const prefix = "data:image/"
	if !strings.HasPrefix(raw, prefix) {
		return fmt.Errorf("Codex images must be data:image base64 URIs")
	}
	comma := strings.IndexByte(raw, ',')
	if comma < len(prefix)+1 || !strings.HasSuffix(strings.ToLower(raw[:comma]), ";base64") {
		return fmt.Errorf("Codex images must use base64 data URIs")
	}
	encoded := raw[comma+1:]
	if encoded == "" || base64.StdEncoding.DecodedLen(len(encoded)) > maxCodexImageBytes {
		return fmt.Errorf("Codex image exceeds the 20 MiB limit")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return fmt.Errorf("Codex image data URI is invalid")
	}
	if len(decoded) > maxCodexImageBytes {
		return fmt.Errorf("Codex image exceeds the 20 MiB limit")
	}
	declaredMIME := strings.ToLower(raw[len("data:"):strings.IndexByte(raw, ';')])
	if !isCodexImageMIME(declaredMIME) || http.DetectContentType(decoded) != declaredMIME {
		return fmt.Errorf("Codex images must be valid PNG, JPEG, GIF, or WebP data")
	}
	b.count++
	b.total += len(decoded)
	if b.count > maxCodexImageCount {
		return fmt.Errorf("Codex accepts at most %d images per request", maxCodexImageCount)
	}
	if b.total > maxCodexImageTotal {
		return fmt.Errorf("Codex images exceed the 50 MiB total limit")
	}
	return nil
}

func isCodexImageMIME(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func (c *rpcClient) turn(ctx context.Context, tid string, input []any, o *chat.ChatOptions, allowed map[string]struct{}, out chan<- types.StreamResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	p := map[string]any{"threadId": tid, "input": input, "approvalPolicy": "untrusted", "cwd": c.cfg.WorkDir}
	if o != nil {
		if len(o.Format) > 0 {
			var schema map[string]any
			if err := json.Unmarshal(o.Format, &schema); err != nil || schema == nil {
				return fmt.Errorf("invalid Codex structured output schema")
			}
			p["outputSchema"] = schema
		}
		if x, ok := o.Reasoning(); ok {
			if effort, send := codexEffort(string(x)); send {
				p["effort"] = effort
			}
		}
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": "turn/start", "params": p})
	if _, e := c.in.Write(append(b, '\n')); e != nil {
		return c.err("write turn/start", e)
	}
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		if !c.out.Scan() {
			if e := c.out.Err(); e != nil {
				return c.err("read turn", e)
			}
			return c.err("app-server closed", fmt.Errorf("unexpected EOF"))
		}
		var m rpcEnvelope
		if e := json.Unmarshal(c.out.Bytes(), &m); e != nil {
			return c.err("decode turn", e)
		}
		if m.Method != "" {
			switch m.Method {
			case "item/agentMessage/delta":
				var q struct {
					Delta string `json:"delta"`
				}
				_ = json.Unmarshal(m.Params, &q)
				if q.Delta != "" && !send(ctx, out, types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: q.Delta}) {
					return ctx.Err()
				}
			case "item/tool/call":
				var requestID any
				if json.Unmarshal(m.ID, &requestID) != nil || requestID == nil {
					return fmt.Errorf("Codex dynamic tool call has no RPC request id")
				}
				switch requestID.(type) {
				case string, float64:
				default:
					return fmt.Errorf("Codex dynamic tool call has an invalid RPC request id")
				}
				var q struct {
					CallID    string          `json:"callId"`
					Tool      string          `json:"tool"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if e := json.Unmarshal(m.Params, &q); e != nil {
					return e
				}
				if q.CallID == "" || q.Tool == "" {
					return fmt.Errorf("Codex returned an invalid dynamic tool call")
				}
				if _, ok := allowed[q.Tool]; !ok {
					return fmt.Errorf("Codex requested undeclared dynamic tool %q", q.Tool)
				}
				a := string(q.Arguments)
				if a == "" {
					a = "{}"
				}
				var args any
				if e := json.Unmarshal([]byte(a), &args); e != nil {
					return fmt.Errorf("Codex returned invalid JSON arguments for %s", q.Tool)
				}
				if e := c.toolReply(m.ID); e != nil {
					return e
				}
				if !send(ctx, out, types.StreamResponse{ResponseType: types.ResponseTypeToolCall, ToolCalls: []types.LLMToolCall{{ID: q.CallID, Type: "function", Function: types.FunctionCall{Name: q.Tool, Arguments: a}}}, FinishReason: "tool_calls"}) {
					return ctx.Err()
				}
				return nil
			case "turn/completed":
				var q struct {
					Turn struct {
						Status string `json:"status"`
					} `json:"turn"`
				}
				_ = json.Unmarshal(m.Params, &q)
				if q.Turn.Status != "completed" {
					return fmt.Errorf("Codex turn %s", q.Turn.Status)
				}
				send(ctx, out, types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"})
				return nil
			}
			if e := c.rejectServerRequest(m); e != nil {
				return c.err("reject unsafe request", e)
			}
			continue
		}
		if string(m.ID) != fmt.Sprintf("%d", c.nextID) {
			continue
		}
		if m.Error != nil {
			return fmt.Errorf("Codex app-server turn/start rejected the request")
		}
	}
}
func codexEffort(value string) (string, bool) {
	switch value {
	case "", "off", "auto":
		return "", false
	default:
		return value, true
	}
}
func (c *rpcClient) toolReply(id json.RawMessage) error {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": map[string]any{"success": false, "contentItems": []any{map[string]any{"type": "inputText", "text": "WeKnora will execute this tool and continue the conversation."}}}})
	_, e := c.in.Write(append(b, '\n'))
	return e
}
func send(ctx context.Context, out chan<- types.StreamResponse, v types.StreamResponse) bool {
	select {
	case out <- v:
		return true
	case <-ctx.Done():
		return false
	}
}
func emitError(ctx context.Context, out chan<- types.StreamResponse, e error) {
	response := types.StreamResponse{ResponseType: types.ResponseTypeError, Content: sanitize(e.Error()), Done: true, FinishReason: types.FinishReasonIncomplete}
	// Preserve a timeout diagnostic even when the call context just expired.
	// Never block an abandoned consumer whose buffer is already full.
	select {
	case out <- response:
		return
	default:
	}
	_ = send(ctx, out, response)
}
