package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"strings"
)

func (r *Runtime) Chat(name, id string) chat.Chat { return &modelChat{r, name, id} }

type modelChat struct {
	r        *Runtime
	name, id string
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
	history, input, ins, e := buildTurn(ms)
	if e != nil {
		return nil, e
	}
	toolDefs, allowed, e := validateTools(o)
	if e != nil {
		return nil, e
	}
	callCtx, cancel := context.WithTimeout(ctx, c.r.cfg.Timeout)
	if e = c.r.acquire(callCtx); e != nil {
		cancel()
		return nil, e
	}
	out := make(chan types.StreamResponse, 32)
	go func() {
		defer cancel()
		defer c.r.release()
		defer close(out)
		x, e := c.r.start(callCtx)
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
func buildTurn(ms []chat.Message) ([]any, string, string, error) {
	var history []any
	var sy []string
	last := -1
	for i, m := range ms {
		if m.Role != "system" {
			last = i
		}
	}
	if last < 0 {
		return nil, "", "", fmt.Errorf("Codex chat needs a message")
	}
	input := "Continue from the supplied conversation."
	for i, m := range ms {
		if len(m.Images) > 0 {
			return nil, "", "", fmt.Errorf("Codex app-server adapter does not support image inputs")
		}
		for _, p := range m.MultiContent {
			if p.Type != "text" {
				return nil, "", "", fmt.Errorf("Codex app-server adapter supports only text content parts")
			}
			m.Content += p.Text
		}
		if m.Role == "system" {
			sy = append(sy, m.Content)
			continue
		}
		if i == last && m.Role == "user" && len(m.ToolCalls) == 0 {
			input = m.Content
			continue
		}
		switch m.Role {
		case "user", "assistant":
			if m.Content != "" {
				kind := "input_text"
				if m.Role == "assistant" {
					kind = "output_text"
				}
				history = append(history, map[string]any{"type": "message", "role": m.Role, "content": []any{map[string]any{"type": kind, "text": m.Content}}})
			}
			for _, tc := range m.ToolCalls {
				history = append(history, map[string]any{"type": "function_call", "call_id": tc.ID, "name": tc.Function.Name, "arguments": tc.Function.Arguments})
			}
		case "tool":
			if m.ToolCallID == "" {
				return nil, "", "", fmt.Errorf("tool result has no tool_call_id")
			}
			history = append(history, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
		default:
			return nil, "", "", fmt.Errorf("unsupported Codex message role %q", m.Role)
		}
	}
	ins := ""
	if len(sy) > 0 {
		ins = "Application system instructions:\n" + strings.Join(sy, "\n") + "\n\n"
	}
	// This is explanatory only: the process configuration removes the native
	// tools, and WeKnora executes only its own declared dynamic tools.
	ins += `You are a text-only model for WeKnora. Never execute commands, inspect files, use built-in tools, browse, access MCP, create subagents, or request permissions. You may request the explicitly provided dynamic tools; WeKnora will execute them. Answer only from the supplied conversation and tool results.`
	return history, input, ins, nil
}
func (c *rpcClient) turn(ctx context.Context, tid, input string, o *chat.ChatOptions, allowed map[string]struct{}, out chan<- types.StreamResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	p := map[string]any{"threadId": tid, "input": []map[string]any{{"type": "text", "text": input}}, "approvalPolicy": "untrusted", "cwd": c.cfg.WorkDir}
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
	_ = send(ctx, out, types.StreamResponse{ResponseType: types.ResponseTypeError, Content: sanitize(e.Error()), Done: true, FinishReason: types.FinishReasonIncomplete})
}
