package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/mcpserver"
	"github.com/dorukardahan/nole/internal/selfupdate"
	"github.com/dorukardahan/nole/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
)

// updateReport is the optional staleness result surfaced by doctor
// --check-updates. Present only when the check actually completed; an
// offline/failed check leaves it absent (the check is fail-soft and silent).
type updateReport struct {
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	Stale   bool   `json:"stale"`
}

// checkForUpdate runs the fail-soft staleness check against the latest published
// release. It returns (report, true) only when the check completed; on offline
// or any error it returns (nil, false) so callers print nothing.
func checkForUpdate(ctx context.Context) (*updateReport, bool) {
	res := selfupdate.CheckLatest(ctx, version.Version)
	if !res.Checked {
		return nil, false
	}
	return &updateReport{Current: res.Current, Latest: res.Latest, Stale: res.Stale}, true
}

// mcpReport is the machine-readable form of the --mcp stdio/protocol smoke,
// nested under doctorReport.MCP when both --json and --mcp are set.
type mcpReport struct {
	OK                  bool     `json:"ok"`
	StartupStdoutBytes  int      `json:"startup_stdout_bytes"`
	ProtocolStdoutBytes int      `json:"protocol_stdout_bytes"`
	NonJSONStdoutLines  int      `json:"non_json_stdout_lines"`
	StderrBytes         int      `json:"stderr_bytes"`
	Tools               []string `json:"tools,omitempty"`
	StartupReason       string   `json:"startup_reason,omitempty"`
	ProtocolReason      string   `json:"protocol_reason,omitempty"`
}

// doctorReport is the machine-readable shape of `doctor --json`. It reuses the
// already-snake_case-tagged core.ProviderStatus / core.BudgetStatus rather than
// re-modeling them, and reports secrets as set/unset only.
type doctorReport struct {
	Binary              string                `json:"binary"`
	ProvidersRegistered int                   `json:"providers_registered"`
	ProvidersAvailable  int                   `json:"providers_available"`
	Providers           []core.ProviderStatus `json:"providers"`
	// ProviderKeys reports key set/unset only; Go field avoids the "secret"
	// keyword (secret-scan.sh heuristic), JSON wire key stays "secrets".
	ProviderKeys   []secretStatus    `json:"secrets"`
	PaidModeActive []string          `json:"paid_mode_active,omitempty"`
	Budget         core.BudgetStatus `json:"budget"`
	MCP            *mcpReport        `json:"mcp,omitempty"`
	Update         *updateReport     `json:"update,omitempty"`
}

// writePaidModeWarnings emits any provider-specific safety warnings to the
// doctor output. Two surfaces today:
//
//   - any provider with NOLE_<PROVIDER>_PAID=1 is flagged so the user is
//     reminded the free-tier safety net is off for that provider;
//   - Brave's free tier is itself a $5/month credit on a subscription model
//     with credit card on file. The wording differs by mode: in free mode
//     nole's ledger caps usage at the monthly free quota, but in paid mode
//     the cap is gone and the user's chosen cost policy is the only guard
//     against runaway billing.
func writePaidModeWarnings(w io.Writer) {
	paid := []string{}
	for _, p := range []string{"brave", "tavily", "firecrawl"} {
		if isProviderPaidMode(p) {
			paid = append(paid, p)
		}
	}
	if len(paid) > 0 {
		fmt.Fprintf(w, "  paid_mode_active: %s (free-tier safety off; check provider dashboards)\n", strings.Join(paid, ", "))
	}
	if os.Getenv("BRAVE_API_KEY") != "" || os.Getenv("BRAVE_SEARCH_API_KEY") != "" {
		if isProviderPaidMode("brave") {
			fmt.Fprintln(w, "  brave_note: Brave Search API uses a subscription with CC on file; NOLE_BRAVE_PAID=1 disables nole's monthly free-quota cap, so every call may bill the CC subject to your cost policy.")
		} else {
			fmt.Fprintln(w, "  brave_note: Brave Search API uses a subscription with CC on file; nole caps usage at the monthly free quota but overage outside nole's ledger will bill the CC.")
		}
	}
}

func newDoctorCommand() *cobra.Command {
	var checkMCP bool
	var jsonOut bool
	var checkUpdates bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check nole configuration and provider health",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				// Machine-readable mode mirrors the human checks but emits one JSON
				// document to stdout. On --mcp smoke failure it still returns the same
				// error (exit 1) as the human path, after writing the report.
				return runDoctorJSON(cmd, checkMCP, checkUpdates)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "nole doctor")
			fmt.Fprintln(cmd.OutOrStdout(), "- binary: ok")
			fmt.Fprintln(cmd.OutOrStdout(), "- stdio: logs must go to stderr; stdout reserved for MCP protocol")

			svc := defaultService()
			providerResp := svc.ProviderStatus(context.Background())
			statuses := providerResp.Providers

			available := 0
			for _, s := range statuses {
				if s.Available {
					available++
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "- providers: %d registered (%d available)\n", len(statuses), available)

			fmt.Fprintln(cmd.OutOrStdout(), "")
			for _, s := range statuses {
				status := "unavailable"
				if s.Available {
					status = "available"
				}
				caps := ""
				for i, c := range s.Capabilities {
					if i > 0 {
						caps += ", "
					}
					caps += string(c)
				}
				line := fmt.Sprintf("  %-12s %s  [%s]  cost=%s policy=%s reason=%s", s.Name, status, caps, s.CostClass, s.CostPolicy, s.PolicyReason)
				if s.Reason != "" {
					line += fmt.Sprintf("  (%s)", s.Reason)
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "- secrets: not printed")
			// secretEnvKeys (config.go) is the single source of truth for which env
			// vars are API keys, shared with `config dump` and doctor --json.
			for _, k := range secretEnvKeys {
				set := "not set"
				if os.Getenv(k.Env) != "" {
					set = "set"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-22s %s\n", k.Env, set)
			}

			writePaidModeWarnings(cmd.OutOrStdout())

			budget := svc.BudgetStatus()
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintf(cmd.OutOrStdout(), "- budget: policy=%s hard_cap=$%d.%02d spent=$%d.%02d no_hidden_paid_spend=%t ledger=%s\n", budget.Policy, budget.HardCapCents/100, budget.HardCapCents%100, budget.SpentCents/100, budget.SpentCents%100, budget.NoHiddenPaidSpend, budget.LedgerState)
			// Cost-capped with no hard cap silently blocks every premium provider.
			// Say so loudly rather than leaving the user to wonder why paid
			// providers never fire. We stay fail-closed (no default spend).
			if budget.HardCapSource == "unset" {
				fmt.Fprintln(cmd.OutOrStdout(), "  cost_cap_note: cost-capped policy set but NOLE_HARD_CAP_CENTS is not — premium providers are BLOCKED. Set NOLE_HARD_CAP_CENTS=<cents> to authorize bounded paid spend.")
			}
			if budget.LedgerWarning != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  ledger_warning: %s\n", budget.LedgerWarning)
			}
			for _, e := range budget.Entries {
				line := fmt.Sprintf("  %-12s %s free_remaining=%d estimated_cost_cents=%d spent_cents=%d", e.Provider, e.CostClass, e.FreeRemaining, e.EstimatedCostCents, e.SpentCents)
				if e.MeteringModel != "" {
					line += fmt.Sprintf(" metering=%s", e.MeteringModel)
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "  (free_remaining is a local quota counter; resets monthly)")
			if budget.EstimateNote != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  estimate_note: %s\n", budget.EstimateNote)
			}
			for _, d := range budget.DriftSignals {
				fmt.Fprintf(cmd.OutOrStdout(), "  drift: %s — %s (observed %s)\n", d.Provider, d.Reason, d.ObservedAt)
			}

			if checkUpdates {
				// Fail-soft: prints nothing when offline or on any error.
				if rep, ok := checkForUpdate(cmd.Context()); ok {
					fmt.Fprintln(cmd.OutOrStdout(), "")
					if rep.Stale {
						fmt.Fprintf(cmd.OutOrStdout(), "- update: nole %s is behind the latest release %s — https://github.com/dorukardahan/nole/releases\n", rep.Current, rep.Latest)
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "- update: up to date (%s)\n", rep.Current)
					}
				}
			}

			if checkMCP {
				fmt.Fprintln(cmd.OutOrStdout(), "")
				startup := checkMCPStdioSmoke(cmd.Context())
				protocol := checkMCPProtocolSmoke(cmd.Context(), configuredMCPSmokeBinary())
				status := "ok"
				if !startup.OK || !protocol.OK {
					status = "failed"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "- mcp: %s\n", status)
				fmt.Fprintf(cmd.OutOrStdout(), "  stdout: startup-clean (%d bytes before protocol input)\n", startup.StdoutBytes)
				fmt.Fprintf(cmd.OutOrStdout(), "  protocol: initialize/tools/list (%d stdout bytes, %d non-json stdout lines)\n", protocol.StdoutBytes, protocol.NonJSONStdoutLines)
				if len(protocol.Tools) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "  tools: %v\n", protocol.Tools)
				}
				if protocol.StderrBytes > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "  stderr: %d bytes (not printed)\n", protocol.StderrBytes)
				}
				if startup.Reason != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  startup_reason: %s\n", startup.Reason)
				}
				if protocol.Reason != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  protocol_reason: %s\n", protocol.Reason)
				}
				if status != "ok" {
					return fmt.Errorf("mcp smoke failed")
				}
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&checkMCP, "mcp", false, "also check MCP stdio startup/stdout health")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output the doctor report as JSON")
	cmd.Flags().BoolVar(&checkUpdates, "check-updates", false, "check GitHub for a newer release (fail-soft, silent when offline)")
	return cmd
}

// runDoctorJSON builds the machine-readable doctor report. It runs the same
// checks as the human path (provider status, secret presence, paid-mode, budget,
// and the optional MCP smoke) and writes one JSON document to stdout. When --mcp
// is set and the smoke fails it returns the same "mcp smoke failed" error as the
// human path AFTER writing the report, so exit-code behavior is identical.
func runDoctorJSON(cmd *cobra.Command, checkMCP, checkUpdates bool) error {
	svc := defaultService()
	providerResp := svc.ProviderStatus(cmd.Context())
	statuses := providerResp.Providers

	available := 0
	for _, s := range statuses {
		if s.Available {
			available++
		}
	}

	paid := []string{}
	for _, p := range []string{"brave", "tavily", "firecrawl"} {
		if isProviderPaidMode(p) {
			paid = append(paid, p)
		}
	}

	report := doctorReport{
		Binary:              "ok",
		ProvidersRegistered: len(statuses),
		ProvidersAvailable:  available,
		Providers:           statuses,
		ProviderKeys:        secretEnvStatuses(),
		PaidModeActive:      paid,
		Budget:              svc.BudgetStatus(),
	}

	smokeFailed := false
	if checkMCP {
		startup := checkMCPStdioSmoke(cmd.Context())
		protocol := checkMCPProtocolSmoke(cmd.Context(), configuredMCPSmokeBinary())
		report.MCP = &mcpReport{
			OK:                  startup.OK && protocol.OK,
			StartupStdoutBytes:  startup.StdoutBytes,
			ProtocolStdoutBytes: protocol.StdoutBytes,
			NonJSONStdoutLines:  protocol.NonJSONStdoutLines,
			StderrBytes:         protocol.StderrBytes,
			Tools:               protocol.Tools,
			StartupReason:       startup.Reason,
			ProtocolReason:      protocol.Reason,
		}
		smokeFailed = !startup.OK || !protocol.OK
	}

	if checkUpdates {
		// Fail-soft: absent from the report when offline or on any error.
		if rep, ok := checkForUpdate(cmd.Context()); ok {
			report.Update = rep
		}
	}

	if err := writeJSONTo(cmd.OutOrStdout(), report); err != nil {
		return err
	}
	if smokeFailed {
		return fmt.Errorf("mcp smoke failed")
	}
	return nil
}

type mcpStdioSmokeResult struct {
	OK          bool
	StdoutBytes int
	Reason      string
}

func checkMCPStdioSmoke(ctx context.Context) mcpStdioSmokeResult {
	select {
	case <-ctx.Done():
		return mcpStdioSmokeResult{OK: false, Reason: ctx.Err().Error()}
	default:
	}

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return mcpStdioSmokeResult{OK: false, Reason: fmt.Sprintf("stdout capture: %v", err)}
	}
	oldStdout := os.Stdout
	os.Stdout = writeEnd
	func() {
		defer func() { os.Stdout = oldStdout }()
		_ = mcpserver.New(defaultService())
	}()
	_ = writeEnd.Close()
	captured, err := io.ReadAll(readEnd)
	_ = readEnd.Close()
	if err != nil {
		return mcpStdioSmokeResult{OK: false, Reason: fmt.Sprintf("read captured stdout: %v", err)}
	}
	if len(captured) != 0 {
		return mcpStdioSmokeResult{OK: false, StdoutBytes: len(captured), Reason: "MCP startup wrote non-protocol bytes to stdout"}
	}
	return mcpStdioSmokeResult{OK: true, StdoutBytes: 0}
}

type mcpProtocolSmokeResult struct {
	OK                 bool
	StdoutBytes        int
	StderrBytes        int
	NonJSONStdoutLines int
	Tools              []string
	Reason             string
}

type rpcLine struct {
	text string
	err  error
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func configuredMCPSmokeBinary() string {
	if binary := strings.TrimSpace(os.Getenv("NOLE_MCP_SMOKE_BINARY")); binary != "" {
		return binary
	}
	binary, err := os.Executable()
	if err != nil {
		return "nole"
	}
	return binary
}

func checkMCPProtocolSmoke(parent context.Context, binary string) mcpProtocolSmokeResult {
	result := mcpProtocolSmokeResult{}
	if strings.TrimSpace(binary) == "" {
		result.Reason = "MCP smoke binary is empty"
		return result
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "mcp")
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		result.Reason = fmt.Sprintf("stdin pipe: %v", err)
		return result
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.Reason = fmt.Sprintf("stdout pipe: %v", err)
		return result
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		result.Reason = fmt.Sprintf("stderr pipe: %v", err)
		return result
	}

	stderrDone := make(chan int, 1)
	go func() {
		b, _ := io.ReadAll(stderr)
		stderrDone <- len(b)
	}()

	lines := make(chan rpcLine, 16)
	go scanJSONLines(stdout, lines)

	if err := cmd.Start(); err != nil {
		result.Reason = "start MCP subprocess: executable not found or not runnable"
		return result
	}

	finish := func(reason string) mcpProtocolSmokeResult {
		if reason != "" {
			result.Reason = reason
		}
		_ = stdin.Close()
		_ = cmd.Wait()
		select {
		case result.StderrBytes = <-stderrDone:
		case <-time.After(time.Second):
		}
		return result
	}

	if err := writeRPC(stdin, map[string]any{
		"jsonrpc": mcp.JSONRPC_VERSION,
		"id":      1,
		"method":  string(mcp.MethodInitialize),
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "nole-doctor",
				"version": "0",
			},
		},
	}); err != nil {
		return finish(fmt.Sprintf("write initialize: %v", err))
	}
	if _, err := readRPCResponse(ctx, lines, 1, &result); err != nil {
		return finish(fmt.Sprintf("initialize failed: %v", err))
	}

	if err := writeRPC(stdin, map[string]any{
		"jsonrpc": mcp.JSONRPC_VERSION,
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	}); err != nil {
		return finish(fmt.Sprintf("write initialized notification: %v", err))
	}
	if err := writeRPC(stdin, map[string]any{
		"jsonrpc": mcp.JSONRPC_VERSION,
		"id":      2,
		"method":  string(mcp.MethodToolsList),
		"params":  map[string]any{},
	}); err != nil {
		return finish(fmt.Sprintf("write tools/list: %v", err))
	}
	toolsResp, err := readRPCResponse(ctx, lines, 2, &result)
	if err != nil {
		return finish(fmt.Sprintf("tools/list failed: %v", err))
	}
	tools, err := parseToolNames(toolsResp.Result)
	if err != nil {
		return finish(fmt.Sprintf("parse tools/list: %v", err))
	}
	result.Tools = tools
	// "extract" is conditionally registered only when an extract-capable
	// provider key (TAVILY_API_KEY, FIRECRAWL_API_KEY) or local Scrapling
	// runtime is configured, so it is not checked here. budget_status,
	// provider_status, and search are always registered regardless of key
	// configuration.
	if missing := missingTools(tools, []string{"budget_status", "provider_status", "search"}); len(missing) > 0 {
		return finish(fmt.Sprintf("missing tools: %v", missing))
	}
	// If an extract-capable provider is configured in the running environment,
	// the subprocess inherited the same env (cmd.Env = os.Environ()) and MUST
	// register extract. Catching the inconsistency here prevents a regression
	// where extract is silently absent for users who have extract configured.
	if mcpserver.HasExtractCapableConfigured() {
		if missing := missingTools(tools, []string{"extract"}); len(missing) > 0 {
			return finish(fmt.Sprintf("extract-capable provider is configured but extract tool is missing from MCP surface: %v", missing))
		}
	}
	if result.NonJSONStdoutLines != 0 {
		return finish("MCP subprocess wrote non-JSON-RPC lines to stdout")
	}

	_ = stdin.Close()
	waitErr := cmd.Wait()
	select {
	case result.StderrBytes = <-stderrDone:
	case <-time.After(time.Second):
	}
	if ctx.Err() != nil {
		result.Reason = ctx.Err().Error()
		return result
	}
	if waitErr != nil {
		result.Reason = fmt.Sprintf("MCP subprocess exit: %v", waitErr)
		return result
	}
	result.OK = true
	return result
}

func scanJSONLines(r io.Reader, out chan<- rpcLine) {
	defer close(out)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		out <- rpcLine{text: scanner.Text()}
	}
	if err := scanner.Err(); err != nil {
		out <- rpcLine{err: err}
	}
}

func writeRPC(w io.Writer, msg map[string]any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

func readRPCResponse(ctx context.Context, lines <-chan rpcLine, wantID int, result *mcpProtocolSmokeResult) (rpcEnvelope, error) {
	for {
		select {
		case <-ctx.Done():
			return rpcEnvelope{}, ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return rpcEnvelope{}, io.EOF
			}
			if line.err != nil {
				return rpcEnvelope{}, line.err
			}
			result.StdoutBytes += len(line.text) + 1
			trimmed := strings.TrimSpace(line.text)
			if trimmed == "" || !json.Valid([]byte(trimmed)) {
				result.NonJSONStdoutLines++
				continue
			}
			var env rpcEnvelope
			if err := json.Unmarshal([]byte(trimmed), &env); err != nil || env.JSONRPC != mcp.JSONRPC_VERSION {
				result.NonJSONStdoutLines++
				continue
			}
			if !rpcIDMatches(env.ID, wantID) {
				continue
			}
			if env.Error != nil {
				return rpcEnvelope{}, fmt.Errorf("JSON-RPC error %d: %s", env.Error.Code, env.Error.Message)
			}
			return env, nil
		}
	}
}

func rpcIDMatches(raw json.RawMessage, want int) bool {
	if len(raw) == 0 {
		return false
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number == want
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		parsed, err := strconv.Atoi(text)
		return err == nil && parsed == want
	}
	return false
}

func parseToolNames(raw json.RawMessage) ([]string, error) {
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		if tool.Name != "" {
			names = append(names, tool.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func missingTools(have []string, want []string) []string {
	available := make(map[string]bool, len(have))
	for _, name := range have {
		available[name] = true
	}
	missing := make([]string, 0)
	for _, name := range want {
		if !available[name] {
			missing = append(missing, name)
		}
	}
	return missing
}
