package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/mock"
)

// --- Item A: honest quota metadata seeding ---

func TestProviderQuotaEntrySeedsMeteringAndEstimateOnly(t *testing.T) {
	t.Setenv("NOLE_BRAVE_PAID", "") // force free-tier mode regardless of ambient env
	e := providerQuotaEntry("brave", true)
	if e.CostClass != core.CostClassFreeTierBYOK {
		t.Fatalf("expected free-tier BYOK, got %s", e.CostClass)
	}
	if e.MeteringModel == "" {
		t.Fatal("expected MeteringModel to be seeded from BYOK metadata")
	}
	if !e.EstimateOnly {
		t.Fatal("expected EstimateOnly=true for a BYOK free-tier entry")
	}
	if e.FreeQuota != 1000 {
		t.Fatalf("FreeQuota = %d, want 1000 (verified fail-safe floor)", e.FreeQuota)
	}
}

// --- Item C: cost-cap source resolution (fail-closed + observable) ---

func TestDefaultQuotaPolicyCostCappedNoCapSourceUnset(t *testing.T) {
	t.Setenv("NOLE_COST_POLICY", "cost-capped")
	t.Setenv("NOLE_HARD_CAP_CENTS", "")
	pol := defaultQuotaPolicyFromEnv()
	if pol.HardCapSource != "unset" {
		t.Fatalf("HardCapSource = %q, want unset (cost-capped, no cap)", pol.HardCapSource)
	}
	if pol.HardCapCents != 0 {
		t.Fatalf("no auto-default spend: HardCapCents = %d, want 0", pol.HardCapCents)
	}
}

func TestDefaultQuotaPolicyCostCappedExplicitSource(t *testing.T) {
	t.Setenv("NOLE_COST_POLICY", "cost-capped")
	t.Setenv("NOLE_HARD_CAP_CENTS", "500")
	pol := defaultQuotaPolicyFromEnv()
	if pol.HardCapSource != "explicit" || pol.HardCapCents != 500 {
		t.Fatalf("explicit cap = (%q, %d), want (explicit, 500)", pol.HardCapSource, pol.HardCapCents)
	}
}

func TestDefaultQuotaPolicyFreeFirstNoSource(t *testing.T) {
	t.Setenv("NOLE_COST_POLICY", "free-first")
	t.Setenv("NOLE_HARD_CAP_CENTS", "")
	if pol := defaultQuotaPolicyFromEnv(); pol.HardCapSource != "" {
		t.Fatalf("free-first needs no cap; HardCapSource = %q, want empty", pol.HardCapSource)
	}
}

func TestDoctorCostCappedNoCapLoudMessage(t *testing.T) {
	t.Setenv("NOLE_COST_POLICY", "cost-capped")
	t.Setenv("NOLE_HARD_CAP_CENTS", "")
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory") // never touch the real state dir
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")

	cmd := newDoctorCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(buf.String(), "cost_cap_note") || !strings.Contains(buf.String(), "NOLE_HARD_CAP_CENTS") {
		t.Fatalf("expected a loud cost-cap note naming NOLE_HARD_CAP_CENTS:\n%s", buf.String())
	}
}

// --- Item E: real /health readiness ---

func TestHealthReadyWithKeylessProvider(t *testing.T) {
	h := newTestHTTPHandler(t) // mock provider: search-capable, available, keyless-free
	rec := doREST(t, h, http.MethodGet, "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", rec.Code)
	}
	var body struct {
		Status             string   `json:"status"`
		AvailableProviders []string `json:"available_providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if body.Status != "ready" || len(body.AvailableProviders) == 0 {
		t.Fatalf("/health = %+v, want ready with >=1 provider", body)
	}
}

func TestHealthExcludesAvailableProviderOutsideActiveRoutes(t *testing.T) {
	registry := core.NewRegistry()
	for _, p := range []core.Provider{mock.New("routed"), mock.New("heldout")} {
		if err := registry.Register(p); err != nil {
			t.Fatalf("register %s: %v", p.Name(), err)
		}
	}
	ledger := core.NewMemoryQuotaLedger()
	for _, name := range []string{"routed", "heldout"} {
		ledger.Set(core.QuotaEntry{Provider: name, CostClass: core.CostClassKeylessFree, KeylessFree: true})
	}
	svc := core.NewService(registry, ledger, core.RouteMatrix{core.TaskGeneral: {"routed"}})
	h := &httpHandler{svc: svc, mcp: buildMCPServer(svc)}

	rec := doREST(t, h, http.MethodGet, "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", rec.Code)
	}
	var body struct {
		AvailableProviders []string `json:"available_providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if len(body.AvailableProviders) != 1 || body.AvailableProviders[0] != "routed" {
		t.Fatalf("/health advertised non-routable provider: %#v", body.AvailableProviders)
	}
}

func TestHealth503WhenNoReadyProvider(t *testing.T) {
	registry := core.NewRegistry()
	if err := registry.Register(mock.NewUnavailable("mock")); err != nil {
		t.Fatalf("register: %v", err)
	}
	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: "mock", CostClass: core.CostClassKeylessFree, KeylessFree: true})
	svc := core.NewService(registry, ledger, core.RouteMatrix{core.TaskGeneral: {"mock"}})
	h := &httpHandler{svc: svc, mcp: buildMCPServer(svc)}

	rec := doREST(t, h, http.MethodGet, "/health", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /health = %d, want 503 when no provider is ready", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if body.Status != "not_ready" || body.Reason == "" {
		t.Fatalf("/health = %+v, want not_ready with a reason", body)
	}
}
