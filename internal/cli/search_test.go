package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestSearchCommandTaskHelpIncludesAllSearchTasks(t *testing.T) {
	help := taskHelpText()
	for _, task := range core.TaskTypes() {
		if task == core.TaskExtract {
			continue
		}
		if !strings.Contains(help, string(task)) {
			t.Errorf("task help missing task %q: %s", task, help)
		}
	}
}

func TestSearchCommandTaskHelpExcludesExtract(t *testing.T) {
	help := taskHelpText()
	if strings.Contains(help, "extract") {
		t.Errorf("task help should not include extract: %s", help)
	}
}

func TestParseTaskAcceptsAllValidTasks(t *testing.T) {
	validTasks := []string{
		"general", "news", "docs", "academic", "factcheck",
		"semantic", "code", "social", "people", "pricing",
		"research", "extract",
	}
	for _, raw := range validTasks {
		result := parseTask(raw)
		if string(result) != raw {
			t.Errorf("parseTask(%q) = %q, want %q", raw, result, raw)
		}
	}
}

func TestParseTaskFallbackToGeneral(t *testing.T) {
	result := parseTask("unknown")
	if result != core.TaskGeneral {
		t.Errorf("parseTask(unknown) = %q, want general", result)
	}
}

func TestCLIErrorEnvelopePreservesRouteTraceAndRedactsSecrets(t *testing.T) {
	payload := buildCLIError("search", errors.New("provider failed token=SECRET Authorization: Bearer *** https://private.example/path"), []string{"brave", "ddgs"}, []core.RouteAttempt{
		{Provider: "brave", Status: "failed", Reason: "provider_error"},
		{Provider: "ddgs", Status: "failed", Reason: "empty_results"},
	})
	if payload.Operation != "search" || len(payload.RouteTrace) != 2 || payload.RouteTrace[1].Reason != "empty_results" {
		t.Fatalf("unexpected CLI error payload: %#v", payload)
	}
	for _, forbidden := range []string{"SECRET", "Authorization", "Bearer", "private.example"} {
		if strings.Contains(payload.Error, forbidden) {
			t.Fatalf("CLI error payload leaked %q in %q", forbidden, payload.Error)
		}
	}
}

func TestHTTPJSONErrorEnvelopePreservesTraceAndRedactsSecrets(t *testing.T) {
	payload := buildCLIError("extract", errors.New("provider echoed api_key=SECRET_TOKEN Authorization: Bearer SECRET https://private.example/path"), []string{"tavily", "jina"}, []core.RouteAttempt{
		{Provider: "tavily", Status: "failed", Reason: "provider_error"},
		{Provider: "jina", Status: "failed", Reason: "empty_content"},
	})

	recorder := httptest.NewRecorder()
	writeHTTPJSONError(recorder, http.StatusInternalServerError, payload)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content-type = %q, want application/json", contentType)
	}
	var decoded cliErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode http error envelope: %v", err)
	}
	if decoded.Operation != "extract" || len(decoded.RouteTrace) != 2 || decoded.RouteTrace[1].Reason != "empty_content" {
		t.Fatalf("unexpected decoded HTTP error envelope: %#v", decoded)
	}
	for _, forbidden := range []string{"SECRET_TOKEN", "Authorization", "Bearer", "private.example"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("HTTP error envelope leaked %q in %q", forbidden, recorder.Body.String())
		}
	}
}
