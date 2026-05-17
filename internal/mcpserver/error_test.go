package mcpserver

import (
	"errors"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestToolErrorJSONPreservesRouteTraceAndRedactsMessage(t *testing.T) {
	payload := toolErrorJSON("search", errors.New("provider failed token=SECRET Authorization: Bearer SECRET"), []string{"brave", "ddgs"}, []core.RouteAttempt{
		{Provider: "brave", Status: "failed", Reason: "provider_error"},
		{Provider: "ddgs", Status: "failed", Reason: "empty_results"},
	})
	text := string(payload)
	for _, want := range []string{`"operation": "search"`, `"route_trace"`, `"provider": "ddgs"`, `"reason": "empty_results"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool error payload missing %s: %s", want, text)
		}
	}
	for _, forbidden := range []string{"SECRET", "Authorization", "Bearer"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("tool error payload leaked %q: %s", forbidden, text)
		}
	}
}
