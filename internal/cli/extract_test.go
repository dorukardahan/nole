package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/safeerr"
)

const extractTestURL = "http://93.184.216.34/"

type extractCLIProvider struct {
	response core.ExtractResponse
	err      error
	lastReq  core.ExtractRequest
}

func (p *extractCLIProvider) Name() string { return "extract-test" }

func (p *extractCLIProvider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilityExtract, core.CapabilityStatus}
}

func (p *extractCLIProvider) Search(context.Context, core.SearchRequest) (core.SearchResponse, error) {
	return core.SearchResponse{}, errors.New("search is not supported")
}

func (p *extractCLIProvider) Extract(_ context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	p.lastReq = req
	return p.response, p.err
}

func (p *extractCLIProvider) Status(context.Context) core.ProviderStatus {
	return core.ProviderStatus{Name: p.Name(), Available: true, Capabilities: p.Capabilities()}
}

func extractTestService(t *testing.T, provider core.Provider) func() *core.Service {
	t.Helper()
	registry := core.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register extract provider: %v", err)
	}
	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: provider.Name(), CostClass: core.CostClassKeylessFree, KeylessFree: true})
	service := core.NewService(registry, ledger, core.RouteMatrix{core.TaskExtract: {provider.Name()}})
	return func() *core.Service { return service }
}

func decodeJSONMap(t *testing.T, output []byte, requiredKeys ...string) map[string]json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(output, &raw); err != nil {
		t.Fatalf("decode JSON object: %v\n%s", err, output)
	}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON output missing %q: %s", key, output)
		}
	}
	return raw
}

func TestExtractCommandRequiresExactlyOneURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "missing", args: nil},
		{name: "multiple", args: []string{extractTestURL, "http://93.184.216.35/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serviceCalled := false
			cmd := newExtractCommandWithService(func() *core.Service {
				serviceCalled = true
				return nil
			})
			cmd.SetArgs(tc.args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			if err := cmd.Execute(); err == nil {
				t.Fatal("extract should reject any argument count other than one URL")
			}
			if serviceCalled {
				t.Fatal("extract service should not be built when argument validation fails")
			}
		})
	}
}

func TestExtractCommandJSONSuccessIncludesRouteShapeAndPassesFormat(t *testing.T) {
	provider := &extractCLIProvider{response: core.ExtractResponse{
		Content:  "deterministic extracted content",
		Metadata: map[string]string{"title": "Example"},
	}}

	var out bytes.Buffer
	cmd := newExtractCommandWithService(extractTestService(t, provider))
	cmd.SetArgs([]string{extractTestURL, "--format", "html", "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute extract command: %v", err)
	}
	if provider.lastReq.URL != extractTestURL || provider.lastReq.Format != "html" {
		t.Fatalf("extract request = %#v, want URL %q and format html", provider.lastReq, extractTestURL)
	}

	decodeJSONMap(t, out.Bytes(), "url", "provider", "content", "metadata", "route", "routing_insight", "route_trace")
	var got core.ExtractResponse
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode extract JSON: %v\n%s", err, out.String())
	}
	if got.URL != extractTestURL || got.Provider != provider.Name() || got.Content != provider.response.Content {
		t.Fatalf("unexpected extract response: %#v", got)
	}
	if got.RoutingInsight == "" {
		t.Fatal("extract JSON should include routing_insight")
	}
	if len(got.RouteTrace) != 1 || got.RouteTrace[0].Provider != provider.Name() || got.RouteTrace[0].Status != "success" {
		t.Fatalf("unexpected route_trace: %#v", got.RouteTrace)
	}
}

func TestExtractCommandJSONFailureIsSanitizedAndKeepsTrace(t *testing.T) {
	provider := &extractCLIProvider{err: errors.New("provider echoed raw private response Authorization: Bearer REDACT_ME https://private.example/path")}

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd := newExtractCommandWithService(extractTestService(t, provider))
	cmd.SetArgs([]string{extractTestURL, "--json"})
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	// Match the root command: Cobra stays quiet and main renders the returned
	// error through safeerr.Message.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("extract provider failure should be returned")
	}
	visible := strings.ToLower(out.String() + "\n" + stderr.String() + "\n" + safeerr.Message(err))
	for _, forbidden := range []string{"authorization", "bearer", "redact_me", "private.example"} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("normal CLI error surface leaked %q: %s", forbidden, visible)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("extract command should leave error rendering to main, got stderr: %s", stderr.String())
	}

	decodeJSONMap(t, out.Bytes(), "operation", "error", "route", "routing_insight", "route_trace")
	var got cliErrorEnvelope
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode extract error JSON: %v\n%s", err, out.String())
	}
	if got.Operation != "extract" || got.Error == "" || got.RoutingInsight == "" {
		t.Fatalf("unexpected extract error envelope: %#v", got)
	}
	if len(got.RouteTrace) != 1 || got.RouteTrace[0].Provider != provider.Name() || got.RouteTrace[0].Status != "failed" {
		t.Fatalf("unexpected failure route_trace: %#v", got.RouteTrace)
	}
}
