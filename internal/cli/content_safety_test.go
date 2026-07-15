package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestWriteHumanContentSafetySilentWithoutIndicators(t *testing.T) {
	var out bytes.Buffer
	writeHumanContentSafety(&out, core.ContentSafetyReport{Untrusted: true, Risk: core.ContentRiskNoIndicators})
	if out.Len() != 0 {
		t.Fatalf("clean report should not add CLI noise: %q", out.String())
	}
}

func TestWriteHumanContentSafetyIsPayloadFreeAndActionable(t *testing.T) {
	var out bytes.Buffer
	writeHumanContentSafety(&out, core.ContentSafetyReport{
		Untrusted: true,
		Risk:      core.ContentRiskHigh,
		Sanitized: true,
		Signals: []core.ContentSafetySignal{
			{Type: "instruction_override", Severity: core.ContentRiskHigh, Count: 1},
			{Type: "bidi_control", Severity: core.ContentRiskHigh, Count: 1},
		},
	})
	got := out.String()
	for _, want := range []string{"Content safety: high", "untrusted", "invisible controls sanitized", "bidi_control,instruction_override"} {
		if !strings.Contains(got, want) {
			t.Fatalf("warning missing %q: %q", want, got)
		}
	}
	if strings.Contains(strings.ToLower(got), "ignore previous") {
		t.Fatalf("warning must never repeat a payload: %q", got)
	}
}
