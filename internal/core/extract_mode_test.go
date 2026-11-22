package core

import "testing"

func TestExtractAttemptModeAllowsOnlyFixedScraplingFields(t *testing.T) {
	for _, tc := range []struct{ provider, mode, reason, wantMode, wantReason string }{
		{"scrapling", "stealthy", "bot_blocked", "stealthy", "bot_blocked"},
		{"scrapling", "fetcher", "bot_blocked", "fetcher", ""},
		{"scrapling", "private-runtime-value", "private-runtime-value", "", ""},
		{"scrapling", "stealthy", "private-runtime-value", "stealthy", ""},
		{"other", "stealthy", "bot_blocked", "", ""},
	} {
		got := extractAttemptMode(RouteAttempt{Provider: tc.provider}, map[string]string{"mode": tc.mode, "escalation_reason": tc.reason})
		if got.Mode != tc.wantMode || got.EscalationReason != tc.wantReason {
			t.Fatalf("unexpected trace fields: %#v", got)
		}
	}
}
