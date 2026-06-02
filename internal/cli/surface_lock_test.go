package cli

import "testing"

// stableCommands is the v1.0.0-frozen set of top-level `nole` commands. Under the
// stability commitment (docs/STABILITY.md), adding, removing, or renaming a
// top-level command is a surface change: a removal/rename is breaking (major
// bump), an addition is a minor bump. Either way it must be a CONSCIOUS decision,
// so this lock fails until the set below is updated to match — preventing a silent
// surface drift. (cobra auto-adds `completion` and `help`; they are not part of
// Nólë's committed surface and are excluded.)
var stableCommands = map[string]bool{
	"search":      true,
	"classify":    true,
	"route-plan":  true,
	"extract":     true,
	"research":    true,
	"bench":       true,
	"providers":   true,
	"doctor":      true,
	"config":      true,
	"mcp":         true,
	"serve":       true,
	"setup":       true,
	"version":     true,
	"self-update": true,
}

func TestStableCLICommandSurface(t *testing.T) {
	got := map[string]bool{}
	for _, c := range NewRootCommand().Commands() {
		name := c.Name()
		if name == "completion" || name == "help" {
			continue // cobra built-ins, not part of the committed surface
		}
		got[name] = true
	}
	for name := range stableCommands {
		if !got[name] {
			t.Errorf("frozen command %q is MISSING — removing/renaming a v1.0.0 command is a BREAKING change (major bump); if intentional, update docs/STABILITY.md and this lock", name)
		}
	}
	for name := range got {
		if !stableCommands[name] {
			t.Errorf("UNEXPECTED command %q — adding a command is a surface change (minor bump); if intentional, document it in docs/STABILITY.md and add it to this lock", name)
		}
	}
}
