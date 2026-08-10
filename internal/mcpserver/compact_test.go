package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/mark3labs/mcp-go/server"
)

func TestNewCompactRegistersOnlyWebEvidence(t *testing.T) {
	srv := newCompactTestServer(t)
	tools := srv.ListTools()
	if len(tools) != 1 {
		t.Fatalf("compact server registered %d tools, want 1: %v", len(tools), tools)
	}
	tool, ok := tools["web_evidence"]
	if !ok || tool == nil || tool.Handler == nil {
		t.Fatalf("compact server did not register a usable web_evidence tool: %v", tools)
	}
}

func TestWebEvidenceSelectsDirectExtractForExactURL(t *testing.T) {
	payload := callWebEvidence(t, newCompactTestServer(t), map[string]any{
		"input": "https://example.com/page",
	})
	if got := stringField(t, payload, "operation"); got != "extract" {
		t.Fatalf("operation = %q, want extract", got)
	}
	if _, ok := payload["extract"]; !ok {
		t.Fatalf("extract payload missing: %v", payload)
	}
}

func TestWebEvidenceSelectsSearchAndExtractForQuickText(t *testing.T) {
	payload := callWebEvidence(t, newCompactTestServer(t), map[string]any{
		"input": "Go net/http timeout documentation",
		"depth": "quick",
	})
	if got := stringField(t, payload, "operation"); got != "search_and_extract" {
		t.Fatalf("operation = %q, want search_and_extract", got)
	}
	if _, ok := payload["search_and_extract"]; !ok {
		t.Fatalf("search_and_extract payload missing: %v", payload)
	}
}

func TestWebEvidenceSelectsResearchForDeepText(t *testing.T) {
	payload := callWebEvidence(t, newCompactTestServer(t), map[string]any{
		"input": "Compare public web evidence routing approaches",
		"depth": "deep",
	})
	if got := stringField(t, payload, "operation"); got != "research" {
		t.Fatalf("operation = %q, want research", got)
	}
	if _, ok := payload["research"]; !ok {
		t.Fatalf("research payload missing: %v", payload)
	}
}

func TestWebEvidenceRejectsUnknownDepth(t *testing.T) {
	result := callWebEvidenceResult(t, newCompactTestServer(t), map[string]any{
		"input": "query",
		"depth": "huge",
	})
	if !result.IsError {
		t.Fatalf("unknown depth returned success: %#v", result)
	}
}

func TestWebEvidenceRejectsURLWithEmbeddedCredentials(t *testing.T) {
	result := callWebEvidenceResult(t, newCompactTestServer(t), map[string]any{
		"input": "https://user:password@example.com/private",
	})
	if !result.IsError {
		t.Fatalf("credentialed URL returned success: %#v", result)
	}
}

func TestWebEvidenceRejectsURLWithQueryOrFragment(t *testing.T) {
	for _, input := range []string{
		"https://example.com/private?token=not-a-real-secret",
		"https://example.com/private#token=not-a-real-secret",
	} {
		result := callWebEvidenceResult(t, newCompactTestServer(t), map[string]any{"input": input})
		if !result.IsError {
			t.Fatalf("private URL %q returned success: %#v", input, result)
		}
	}
}

func newCompactTestServer(t *testing.T) *server.MCPServer {
	t.Helper()
	provider := mock.New("mock")
	registry := core.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: provider.Name(), CostClass: core.CostClassKeylessFree, KeylessFree: true})
	matrix := core.RouteMatrix{}
	for _, task := range core.TaskTypes() {
		matrix[task] = []string{"mock"}
	}
	return NewCompact(core.NewService(registry, ledger, matrix))
}

type compactCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func callWebEvidence(t *testing.T, srv *server.MCPServer, arguments map[string]any) map[string]json.RawMessage {
	t.Helper()
	result := callWebEvidenceResult(t, srv, arguments)
	if result.IsError {
		t.Fatalf("web_evidence returned a tool error: %#v", result)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("unexpected tool content: %#v", result)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal web_evidence payload: %v", err)
	}
	return payload
}

func callWebEvidenceResult(t *testing.T, srv *server.MCPServer, arguments map[string]any) compactCallResult {
	t.Helper()
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "web_evidence",
			"arguments": arguments,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := srv.HandleMessage(context.Background(), json.RawMessage(msg))
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result compactCallResult `json:"result"`
		Error  json.RawMessage   `json:"error"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("unmarshal JSON-RPC envelope: %v", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		t.Fatalf("JSON-RPC error: %s", envelope.Error)
	}
	return envelope.Result
}

func stringField(t *testing.T, payload map[string]json.RawMessage, key string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(payload[key], &value); err != nil {
		t.Fatalf("unmarshal %s: %v", key, err)
	}
	return value
}
