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

	"github.com/dorukardahan/nole/internal/mcpserver"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	var checkMCP bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check nole configuration and provider health",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "nole doctor")
			fmt.Fprintln(cmd.OutOrStdout(), "- binary: ok")
			fmt.Fprintln(cmd.OutOrStdout(), "- stdio: logs must go to stderr; stdout reserved for MCP protocol")

			svc := defaultService()
			statuses := svc.ProviderStatus(context.Background())

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
			keys := []struct {
				env  string
				name string
			}{
				{"JINA_API_KEY", "jina"},
				{"FIRECRAWL_API_KEY", "firecrawl"},
				{"BRAVE_API_KEY", "brave"},
				{"BRAVE_SEARCH_API_KEY", "brave (alt)"},
				{"TAVILY_API_KEY", "tavily"},
			}
			for _, k := range keys {
				set := "not set"
				if os.Getenv(k.env) != "" {
					set = "set"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-22s %s\n", k.env, set)
			}

			budget := svc.BudgetStatus()
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintf(cmd.OutOrStdout(), "- budget: policy=%s hard_cap=$%d.%02d spent=$%d.%02d no_hidden_paid_spend=%t\n", budget.Policy, budget.HardCapCents/100, budget.HardCapCents%100, budget.SpentCents/100, budget.SpentCents%100, budget.NoHiddenPaidSpend)
			for _, e := range budget.Entries {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %s free_remaining=%d estimated_cost_cents=%d spent_cents=%d\n", e.Provider, e.CostClass, e.FreeRemaining, e.EstimatedCostCents, e.SpentCents)
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
	return cmd
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
		result.Reason = fmt.Sprintf("start MCP subprocess: %v", err)
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
	if missing := missingTools(tools, []string{"budget_status", "extract", "provider_status", "search"}); len(missing) > 0 {
		return finish(fmt.Sprintf("missing tools: %v", missing))
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
