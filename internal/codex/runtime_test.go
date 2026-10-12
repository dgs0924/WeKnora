package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestMain(m *testing.M) {
	switch os.Getenv("GO_WANT_CODEX_HELPER") {
	case "status":
		codexHelperStatus()
		os.Exit(0)
	case "usage":
		codexHelperUsage()
		os.Exit(0)
	case "stream", "flood", "schema":
		codexHelperStream()
		os.Exit(0)
	case "incomplete":
		codexHelperIncomplete()
		os.Exit(0)
	case "dynamic", "dynamic-no-id":
		codexHelperDynamic()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRuntimeStatusUsesOnlyAppServerAccountMetadata(t *testing.T) {
	r := testRuntime(t, "status")
	status, err := r.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.LoggedIn || status.Email != "person@example.test" || status.Plan != "plus" {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestRuntimeUsageReadsRateLimitsRPC(t *testing.T) {
	r := testRuntime(t, "usage")
	got, err := r.Usage(context.Background())
	if err != nil || !got.Available || len(got.Limits) != 2 {
		t.Fatalf("usage=%#v err=%v", got, err)
	}
}

func TestQueuedRequestRechecksAuthorizationAfterGrantRevocation(t *testing.T) {
	r := testRuntime(t, "status")
	if err := r.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	var allowed atomic.Bool
	allowed.Store(true)
	checked := make(chan struct{}, 1)
	revoked := errors.New("grant revoked")
	client := r.ChatAuthorized("gpt-test", "id", func(context.Context) error {
		checked <- struct{}{}
		if !allowed.Load() {
			return revoked
		}
		return nil
	})
	result := make(chan error, 1)
	go func() {
		_, err := client.ChatStream(context.Background(), []chat.Message{{Role: "user", Content: "hello"}}, nil)
		result <- err
	}()
	select {
	case <-checked:
		r.release()
		t.Fatal("authorization was checked before the queued request acquired the runtime")
	case <-time.After(30 * time.Millisecond):
	}
	allowed.Store(false)
	r.release()
	select {
	case err := <-result:
		if !errors.Is(err, revoked) {
			t.Fatalf("queued request ignored revocation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued request did not finish after revocation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.acquire(ctx); err != nil {
		t.Fatalf("denied request retained the runtime gate: %v", err)
	}
	r.release()
}

func TestChatStreamRelaysDeltasAndDeniesUnsafeAppServerRequest(t *testing.T) {
	r := testRuntime(t, "stream")
	stream, err := r.Chat("gpt-test", "id").ChatStream(context.Background(), []chat.Message{{Role: "user", Content: "hello"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	done := false
	for item := range stream {
		if item.ResponseType == types.ResponseTypeError {
			t.Fatal(item.Content)
		}
		got += item.Content
		done = done || item.Done
	}
	if got != "hello" || !done {
		t.Fatalf("stream=%q done=%v", got, done)
	}
}

func TestChatRejectsIncompleteClosedStream(t *testing.T) {
	r := testRuntime(t, "incomplete")
	_, err := r.Chat("gpt-test", "id").Chat(context.Background(), []chat.Message{{Role: "user", Content: "hello"}}, nil)
	if err == nil {
		t.Fatalf("expected incomplete stream error, got %v", err)
	}
}

func TestStructuredOutputSchemaIsSentToCodex(t *testing.T) {
	r := testRuntime(t, "schema")
	options := &chat.ChatOptions{Format: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`)}
	_, err := r.Chat("gpt-test", "id").Chat(context.Background(), []chat.Message{{Role: "user", Content: "hello"}}, options)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAbandonedStreamReleasesRuntimeGateAfterTimeout(t *testing.T) {
	r := testRuntime(t, "flood")
	stream, err := r.Chat("gpt-test", "id").ChatStream(context.Background(), []chat.Message{{Role: "user", Content: "hello"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := <-stream
	if first.ResponseType != types.ResponseTypeAnswer {
		t.Fatalf("helper did not start streaming: %#v", first)
	}
	// Stop consuming: the helper fills stdout and the adapter fills its channel.
	// Its own timeout must terminate the child and release the single-user gate.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := r.acquire(ctx); err != nil {
		t.Fatalf("abandoned stream kept the runtime occupied: %v", err)
	}
	r.release()
	for range stream {
	}
}

func TestDynamicToolRoundTripInjectsFunctionCallAndOutput(t *testing.T) {
	r := testRuntime(t, "dynamic")
	options := &chat.ChatOptions{Tools: []chat.Tool{{Type: "function", Function: chat.FunctionDef{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}}}
	first, err := r.Chat("gpt-test", "id").Chat(context.Background(), []chat.Message{{Role: "user", Content: "lookup x"}}, options)
	if err != nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "call-1" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := r.Chat("gpt-test", "id").Chat(context.Background(), []chat.Message{
		{Role: "user", Content: "lookup x"},
		{Role: "assistant", ToolCalls: []chat.ToolCall{{ID: "call-1", Type: "function", Function: chat.FunctionCall{Name: "lookup", Arguments: `{"q":"x"}`}}}},
		{Role: "tool", ToolCallID: "call-1", Content: "found"},
	}, options)
	if err != nil || second.Content != "answer" {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestDynamicToolsPreserveFunctionSchema(t *testing.T) {
	tools := dynamicTools([]chat.Tool{{Type: "function", Function: chat.FunctionDef{Name: "search", Description: "find", Parameters: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)}}})
	if len(tools) != 1 || tools[0]["name"] != "search" || tools[0]["inputSchema"].(map[string]interface{})["type"] != "object" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}

func TestDynamicToolNotificationIsNotExecutable(t *testing.T) {
	r := testRuntime(t, "dynamic-no-id")
	options := &chat.ChatOptions{Tools: []chat.Tool{{Type: "function", Function: chat.FunctionDef{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}}}
	response, err := r.Chat("gpt-test", "id").Chat(context.Background(), []chat.Message{{Role: "user", Content: "lookup"}}, options)
	if err == nil || response != nil {
		t.Fatalf("notification was accepted as executable: response=%#v error=%v", response, err)
	}
}

func TestMultipartTextSurvivesHistoryAndLatestInput(t *testing.T) {
	history, input, instructions, err := buildTurn([]chat.Message{
		{Role: "system", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "Application policy"}}},
		{Role: "user", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "first"}, {Type: "text", Text: " part"}}},
		{Role: "assistant", Content: "reply"},
		{Role: "user", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "next"}}},
	})
	if err != nil || input != "next" || !strings.Contains(instructions, "Application policy") {
		t.Fatalf("lost multipart content: input=%q instructions=%q error=%v", input, instructions, err)
	}
	raw, _ := json.Marshal(history)
	if !strings.Contains(string(raw), "first part") {
		t.Fatalf("lost multipart history: %s", raw)
	}
}

func TestBuildTurnPreservesToolRoundTrip(t *testing.T) {
	history, input, _, err := buildTurn([]chat.Message{
		{Role: "user", Content: "look this up"},
		{Role: "assistant", ToolCalls: []chat.ToolCall{{ID: "call-1", Type: "function", Function: chat.FunctionCall{Name: "search", Arguments: `{"q":"x"}`}}}},
		{Role: "tool", ToolCallID: "call-1", Name: "search", Content: "result"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if input == "" || len(history) != 3 {
		t.Fatalf("input=%q history=%#v", input, history)
	}
	if history[1].(map[string]interface{})["type"] != "function_call" || history[2].(map[string]interface{})["type"] != "function_call_output" {
		t.Fatalf("tool history lost: %#v", history)
	}
}

func testRuntime(t *testing.T, mode string) *Runtime {
	t.Helper()
	binDir := t.TempDir()
	appServer := filepath.Join(binDir, "app-server")
	if err := os.WriteFile(appServer, []byte(fmt.Sprintf("#!/bin/sh\nGO_WANT_CODEX_HELPER=%q exec %q\n", mode, os.Args[0])), 0700); err != nil {
		t.Fatal(err)
	}
	// The helper is this package's test binary and initializes the model SDKs
	// before entering TestMain; allow startup time on the build server.
	r, err := New(Config{Binary: appServer, Home: "/configured/codex", WorkDir: t.TempDir(), Timeout: 5 * time.Second, MaxOutputBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "must-not-reach-helper")
	return r
}

func codexHelperStatus() {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	if os.Getenv("CODEX_HOME") != "/configured/codex" || os.Getenv("OPENAI_API_KEY") != "" {
		os.Exit(9)
	}
	var req rpcEnvelope
	readRPC(in, &req)
	if req.Method != "initialize" {
		os.Exit(10)
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"codexHome": "/configured/codex", "platformFamily": "unix", "platformOs": "linux", "userAgent": "test"}})
	readRPC(in, &req)
	if req.Method != "initialized" {
		os.Exit(11)
	}
	readRPC(in, &req)
	if req.Method != "account/read" {
		os.Exit(12)
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"requiresOpenaiAuth": true, "account": map[string]any{"type": "chatgpt", "email": "person@example.test", "planType": "plus", "access_token": "must-not-leak"}}})
}

func codexHelperUsage() {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	var req rpcEnvelope
	readRPC(in, &req)
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{}})
	readRPC(in, &req)
	readRPC(in, &req)
	if req.Method != "account/rateLimits/read" {
		os.Exit(13)
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"rateLimits": map[string]any{"limitId": "fallback", "primary": map[string]any{"usedPercent": 20}}, "rateLimitsByLimitId": map[string]any{"five": map[string]any{"limitId": "five", "primary": map[string]any{"usedPercent": 30}}, "week": map[string]any{"limitId": "week", "primary": map[string]any{"usedPercent": 40}}}}})
}

func codexHelperStream() {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	var req rpcEnvelope
	readRPC(in, &req)
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{}})
	readRPC(in, &req) // initialized
	readRPC(in, &req)
	if req.Method != "thread/start" {
		os.Exit(20)
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"thread": map[string]any{"id": "thread"}}})
	readRPC(in, &req)
	if req.Method != "turn/start" {
		os.Exit(21)
	}
	if os.Getenv("GO_WANT_CODEX_HELPER") == "schema" && !strings.Contains(string(req.Params), `"outputSchema":{"properties":{"answer":{"type":"string"}},"type":"object"}`) {
		os.Exit(23)
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": 77, "method": "item/commandExecution/requestApproval", "params": map[string]any{"callId": "unsafe"}})
	readRPC(in, &req)
	var denied map[string]any
	_ = json.Unmarshal(req.Result, &denied)
	if denied["decision"] != "decline" {
		os.Exit(22)
	}
	if os.Getenv("GO_WANT_CODEX_HELPER") == "flood" {
		for i := 0; i < 10000; i++ {
			writeRPC(out, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "chunk"}})
		}
		return
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"delta": "hello"}})
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
}

func codexHelperIncomplete() {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	var req rpcEnvelope
	readRPC(in, &req)
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{}})
	readRPC(in, &req)
	readRPC(in, &req)
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"thread": map[string]any{"id": "thread"}}})
	readRPC(in, &req)
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
}

func codexHelperDynamic() {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	var req rpcEnvelope
	readRPC(in, &req)
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{}})
	readRPC(in, &req) // initialized
	readRPC(in, &req)
	if req.Method != "thread/start" || !strings.Contains(string(req.Params), "dynamicTools") {
		os.Exit(40)
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"thread": map[string]any{"id": "thread"}}})
	readRPC(in, &req)
	if req.Method == "thread/inject_items" {
		if !strings.Contains(string(req.Params), "function_call") || !strings.Contains(string(req.Params), "function_call_output") {
			os.Exit(41)
		}
		writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{}})
		readRPC(in, &req)
		if req.Method != "turn/start" {
			os.Exit(42)
		}
		writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
		writeRPC(out, map[string]any{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"delta": "answer"}})
		writeRPC(out, map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
		return
	}
	if req.Method != "turn/start" {
		os.Exit(43)
	}
	writeRPC(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
	call := map[string]any{"jsonrpc": "2.0", "id": 81, "method": "item/tool/call", "params": map[string]any{"callId": "call-1", "tool": "lookup", "arguments": map[string]any{"q": "x"}}}
	if os.Getenv("GO_WANT_CODEX_HELPER") == "dynamic-no-id" {
		delete(call, "id")
		writeRPC(out, call)
		return
	}
	writeRPC(out, call)
	readRPC(in, &req)
	var reply struct {
		Success bool `json:"success"`
	}
	_ = json.Unmarshal(req.Result, &reply)
	if reply.Success {
		os.Exit(44)
	}
}

func readRPC(in *bufio.Scanner, dst *rpcEnvelope) {
	if !in.Scan() {
		os.Exit(30)
	}
	if json.Unmarshal(in.Bytes(), dst) != nil {
		os.Exit(31)
	}
}
func writeRPC(out *bufio.Writer, v any) {
	b, _ := json.Marshal(v)
	_, _ = out.Write(append(b, '\n'))
	_ = out.Flush()
}
