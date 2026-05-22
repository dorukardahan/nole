package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestClassifyCommandOutputsJSON(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"classify", "Vercel pricing limits", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("classify failed: %v", err)
	}
	var got core.QueryClassification
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("classify output is not JSON: %v\n%s", err, out.String())
	}
	if got.PrimaryTask != core.TaskPricing || len(got.Intents) == 0 {
		t.Fatalf("unexpected classification: %#v", got)
	}
	if got.RoutingInsight == "" {
		t.Fatalf("classification missing routing insight: %#v", got)
	}
}

func TestClassifyCommandCanSuppressRoutingInsight(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"classify", "Vercel pricing limits", "--json", "--insight", "off"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("classify failed: %v", err)
	}
	var got core.QueryClassification
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("classify output is not JSON: %v\n%s", err, out.String())
	}
	if got.RoutingInsight != "" {
		t.Fatalf("classification routing insight = %q, want empty", got.RoutingInsight)
	}
}

func TestRoutePlanCommandSupportsOverridesAndTrace(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"route-plan", "Reddit discussions about vector databases", "--providers", "ddgs,firecrawl", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("route-plan failed: %v", err)
	}
	var got core.RoutePlan
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("route-plan output is not JSON: %v\n%s", err, out.String())
	}
	if got.PrimaryTask != core.TaskSocial {
		t.Fatalf("primary task = %q, want social/community; plan=%#v", got.PrimaryTask, got)
	}
	if len(got.Routes) == 0 || len(got.Routes[0].Route) != 2 || got.Routes[0].Route[0] != "ddgs" || got.Routes[0].Route[1] != "firecrawl" {
		t.Fatalf("expected provider override route, got %#v", got.Routes)
	}
	if len(got.RouteTrace) != 2 || got.RouteTrace[0].Status != "planned" || got.RouteTrace[0].Reason != "provider_override" {
		t.Fatalf("unexpected route trace: %#v", got.RouteTrace)
	}
	if got.RoutingInsight == "" {
		t.Fatalf("route-plan missing routing insight: %#v", got)
	}
}

func TestRoutePlanCommandCanSuppressRoutingInsight(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"route-plan", "Reddit discussions about vector databases", "--providers", "ddgs,firecrawl", "--json", "--insight", "off"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("route-plan failed: %v", err)
	}
	var got core.RoutePlan
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("route-plan output is not JSON: %v\n%s", err, out.String())
	}
	if got.RoutingInsight != "" {
		t.Fatalf("route-plan routing insight = %q, want empty", got.RoutingInsight)
	}
	if len(got.RouteTrace) == 0 {
		t.Fatalf("route_trace should remain available when insight is off: %#v", got)
	}
}

func TestRoutePlanCommandRejectsUnknownProviderOverride(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"route-plan", "docs", "--providers", "ddgs,unknown"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected unknown provider override to fail")
	}
}
