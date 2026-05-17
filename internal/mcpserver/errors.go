package mcpserver

import (
	"encoding/json"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/safeerr"
)

type toolErrorEnvelope struct {
	Operation  string              `json:"operation"`
	Error      string              `json:"error"`
	Route      []string            `json:"route,omitempty"`
	RouteTrace []core.RouteAttempt `json:"route_trace,omitempty"`
}

func toolErrorJSON(operation string, err error, route []string, trace []core.RouteAttempt) []byte {
	payload := toolErrorEnvelope{
		Operation:  operation,
		Error:      safeerr.Message(err),
		Route:      append([]string(nil), route...),
		RouteTrace: append([]core.RouteAttempt(nil), trace...),
	}
	b, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return []byte(`{"operation":"` + operation + `","error":"failed to marshal sanitized tool error"}`)
	}
	return b
}
