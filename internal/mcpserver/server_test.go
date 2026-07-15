package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/dorukardahan/nole/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestNewInitializesMCPServer(t *testing.T) {
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))
	if srv == nil {
		t.Fatal("New returned a nil server")
	}

	session := server.NewInProcessSession("server-test", nil)
	ctx := registerTestSession(t, srv, session)
	request, err := json.Marshal(map[string]any{
		"jsonrpc": mcp.JSONRPC_VERSION,
		"id":      1,
		"method":  string(mcp.MethodInitialize),
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "server-test",
				"version": "1",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal initialize request: %v", err)
	}

	encoded, err := json.Marshal(srv.HandleMessage(ctx, request))
	if err != nil {
		t.Fatalf("marshal initialize response: %v", err)
	}
	var response struct {
		JSONRPC string               `json:"jsonrpc"`
		Result  mcp.InitializeResult `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatalf("unmarshal initialize response: %v", err)
	}
	if response.Error != nil {
		t.Fatalf("initialize returned JSON-RPC error %d: %s", response.Error.Code, response.Error.Message)
	}
	if response.JSONRPC != mcp.JSONRPC_VERSION {
		t.Errorf("JSON-RPC version = %q, want %q", response.JSONRPC, mcp.JSONRPC_VERSION)
	}
	if response.Result.ProtocolVersion != mcp.LATEST_PROTOCOL_VERSION {
		t.Errorf("protocol version = %q, want %q", response.Result.ProtocolVersion, mcp.LATEST_PROTOCOL_VERSION)
	}
	if response.Result.ServerInfo.Name != "nole" {
		t.Errorf("server name = %q, want nole", response.Result.ServerInfo.Name)
	}
	if response.Result.ServerInfo.Version != version.Version {
		t.Errorf("server version = %q, want %q", response.Result.ServerInfo.Version, version.Version)
	}
	if response.Result.Capabilities.Tools == nil {
		t.Fatal("initialize response does not advertise tool capabilities")
	}
	if response.Result.Capabilities.Tools.ListChanged {
		t.Error("tool listChanged capability = true, want false")
	}
	if !session.Initialized() {
		t.Error("initialize request did not mark the session initialized")
	}
}

func TestNewRegistersCompleteToolSet(t *testing.T) {
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))
	registered := srv.ListTools()
	want := []string{
		"search",
		"extract",
		"search_and_extract",
		"research",
		"provider_status",
		"budget_status",
	}

	if len(registered) != len(want) {
		t.Fatalf("registered %d tools, want %d: %v", len(registered), len(want), registered)
	}
	for _, name := range want {
		tool, ok := registered[name]
		if !ok {
			t.Errorf("tool %q is not registered", name)
			continue
		}
		if tool == nil {
			t.Errorf("tool %q has a nil registration", name)
			continue
		}
		if tool.Handler == nil {
			t.Errorf("tool %q has no handler", name)
		}
		if tool.Tool.Description == "" {
			t.Errorf("tool %q has no description", name)
		}
	}
}

func TestNewDispatchesToolCalls(t *testing.T) {
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))
	session := server.NewInProcessSession("server-dispatch-test", nil)
	ctx := registerTestSession(t, srv, session)
	initializeTestSession(t, srv, ctx, session)

	tests := []struct {
		name    string
		tool    string
		wantKey string
	}{
		{name: "budget status", tool: "budget_status", wantKey: "policy"},
		{name: "provider status", tool: "provider_status", wantKey: "providers"},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := json.Marshal(map[string]any{
				"jsonrpc": mcp.JSONRPC_VERSION,
				"id":      i + 1,
				"method":  string(mcp.MethodToolsCall),
				"params": map[string]any{
					"name":      test.tool,
					"arguments": map[string]any{},
				},
			})
			if err != nil {
				t.Fatalf("marshal %s request: %v", test.tool, err)
			}

			encoded, err := json.Marshal(srv.HandleMessage(ctx, request))
			if err != nil {
				t.Fatalf("marshal %s response: %v", test.tool, err)
			}
			var response struct {
				Result struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				} `json:"result"`
				Error *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(encoded, &response); err != nil {
				t.Fatalf("unmarshal %s response: %v", test.tool, err)
			}
			if response.Error != nil {
				t.Fatalf("%s returned JSON-RPC error %d: %s", test.tool, response.Error.Code, response.Error.Message)
			}
			if response.Result.IsError {
				t.Fatalf("%s returned a tool error: %s", test.tool, encoded)
			}
			if len(response.Result.Content) != 1 || response.Result.Content[0].Type != "text" {
				t.Fatalf("%s returned unexpected content: %s", test.tool, encoded)
			}

			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &payload); err != nil {
				t.Fatalf("unmarshal %s tool payload: %v", test.tool, err)
			}
			if _, ok := payload[test.wantKey]; !ok {
				t.Errorf("%s payload is missing %q: %s", test.tool, test.wantKey, response.Result.Content[0].Text)
			}
		})
	}
}

func TestNewStdioLifecycleGuardsDoubleStartAndCleansUp(t *testing.T) {
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))
	stdio := server.NewStdioServer(srv)
	stdio.SetErrorLogger(log.New(io.Discard, "", 0))

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	stdio.SetContextFunc(func(ctx context.Context) context.Context {
		close(started)
		return ctx
	})
	listenErr := make(chan error, 1)
	go func() {
		defer close(listenErr)
		listenErr <- stdio.Listen(ctx, stdinReader, stdoutWriter)
	}()

	t.Cleanup(func() {
		cancel()
		_ = stdinWriter.Close()
		_ = stdinReader.Close()
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		select {
		case <-listenErr:
		case <-time.After(2 * time.Second):
			t.Error("stdio server cleanup timed out")
		}
	})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stdio server did not start")
	}

	scanner := bufio.NewScanner(stdoutReader)
	writeJSONLine(t, stdinWriter, map[string]any{
		"jsonrpc": mcp.JSONRPC_VERSION,
		"id":      1,
		"method":  string(mcp.MethodInitialize),
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "stdio-lifecycle-test",
				"version": "1",
			},
		},
	})
	initializeResponse := scanJSONLine(t, scanner)
	assertJSONRPCSuccess(t, initializeResponse)

	writeJSONLine(t, stdinWriter, map[string]any{
		"jsonrpc": mcp.JSONRPC_VERSION,
		"id":      2,
		"method":  string(mcp.MethodToolsCall),
		"params": map[string]any{
			"name":      "budget_status",
			"arguments": map[string]any{},
		},
	})
	toolResponse := scanJSONLine(t, scanner)
	assertJSONRPCSuccess(t, toolResponse)
	var response struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(toolResponse, &response); err != nil {
		t.Fatalf("unmarshal budget_status response: %v", err)
	}
	if len(response.Result.Content) != 1 || !strings.Contains(response.Result.Content[0].Text, `"policy"`) {
		t.Fatalf("budget_status response is missing policy content: %s", toolResponse)
	}

	second := server.NewStdioServer(srv)
	second.SetErrorLogger(log.New(io.Discard, "", 0))
	err := second.Listen(context.Background(), strings.NewReader(""), io.Discard)
	if !errors.Is(err, server.ErrSessionExists) {
		t.Fatalf("second concurrent Listen error = %v, want %v", err, server.ErrSessionExists)
	}

	cancel()
	_ = stdinWriter.Close()
	select {
	case err := <-listenErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("stdio server shutdown error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdio server did not stop after cancellation")
	}

	restarted := server.NewStdioServer(srv)
	restarted.SetErrorLogger(log.New(io.Discard, "", 0))
	restartCtx, restartCancel := context.WithCancel(context.Background())
	if err := restarted.Listen(restartCtx, strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("Listen after cleanup: %v", err)
	}
	restartCancel()
}

func registerTestSession(t *testing.T, srv *server.MCPServer, session *server.InProcessSession) context.Context {
	t.Helper()
	ctx := context.Background()
	if err := srv.RegisterSession(ctx, session); err != nil {
		t.Fatalf("register session: %v", err)
	}
	t.Cleanup(func() {
		srv.UnregisterSession(ctx, session.SessionID())
	})
	return srv.WithContext(ctx, session)
}

func initializeTestSession(t *testing.T, srv *server.MCPServer, ctx context.Context, session *server.InProcessSession) {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": mcp.JSONRPC_VERSION,
		"id":      1,
		"method":  string(mcp.MethodInitialize),
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "server-dispatch-test",
				"version": "1",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal initialize request: %v", err)
	}
	encoded, err := json.Marshal(srv.HandleMessage(ctx, request))
	if err != nil {
		t.Fatalf("marshal initialize response: %v", err)
	}
	assertJSONRPCSuccess(t, encoded)
	if !session.Initialized() {
		t.Fatal("initialize request did not mark the dispatch session initialized")
	}
}

func writeJSONLine(t *testing.T, writer io.Writer, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON-RPC request: %v", err)
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		t.Fatalf("write JSON-RPC request: %v", err)
	}
}

func scanJSONLine(t *testing.T, scanner *bufio.Scanner) []byte {
	t.Helper()
	type result struct {
		line []byte
		ok   bool
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		ok := scanner.Scan()
		resultCh <- result{
			line: append([]byte(nil), scanner.Bytes()...),
			ok:   ok,
			err:  scanner.Err(),
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("read JSON-RPC response: %v", result.err)
		}
		if !result.ok {
			t.Fatal("JSON-RPC response stream closed")
		}
		return result.line
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for JSON-RPC response")
		return nil
	}
}

func assertJSONRPCSuccess(t *testing.T, encoded []byte) {
	t.Helper()
	var response struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatalf("unmarshal JSON-RPC response: %v", err)
	}
	if response.Error != nil {
		t.Fatalf("JSON-RPC error %d: %s", response.Error.Code, response.Error.Message)
	}
}
