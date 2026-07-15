package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
)

// writeHumanContentSafety prints only actionable scanner findings. All remote
// content remains untrusted even when no indicators are found; repeating that
// baseline before every clean search result would bury useful output.
func writeHumanContentSafety(out io.Writer, report core.ContentSafetyReport) {
	if out == nil || report.Risk == "" || report.Risk == core.ContentRiskNoIndicators {
		return
	}
	types := make([]string, 0, len(report.Signals))
	for _, signal := range report.Signals {
		if signal.Type != "" {
			types = append(types, signal.Type)
		}
	}
	sort.Strings(types)
	fmt.Fprintf(out, "Content safety: %s (untrusted", report.Risk)
	if report.Sanitized {
		fmt.Fprint(out, "; invisible controls sanitized")
	}
	if len(types) > 0 {
		fmt.Fprintf(out, "; signals=%s", strings.Join(types, ","))
	}
	fmt.Fprintln(out, ")")
}
