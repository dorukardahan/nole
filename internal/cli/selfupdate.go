package cli

import (
	"github.com/dorukardahan/nole/internal/selfupdate"
	"github.com/dorukardahan/nole/internal/version"
	"github.com/spf13/cobra"
)

// newSelfUpdateCommand downloads, verifies, and installs the latest published
// nole release in place. It is the natural continuation of `doctor
// --check-updates` (which only advises): same fail-soft outbound posture, but it
// applies the update. SHA256 is the mandatory integrity floor (verified
// in-process); a GitHub build-provenance attestation is an additive, best-effort
// second gate done via `gh attestation verify` — mirroring scripts/install.sh's
// contract exactly. It NEVER auto-runs; the user invokes it explicitly.
//
// Unlike `nole mcp`/`serve` (whose stdout is a protocol channel), this is an
// interactive command, so progress on stdout is expected and fine.
func newSelfUpdateCommand() *cobra.Command {
	var checkOnly bool
	var targetVersion string
	var verifyFlag string
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Download, verify, and install the latest nole release in place",
		Long: "Download the latest published nole release, verify it (mandatory SHA256 " +
			"floor plus an additive, best-effort GitHub build-provenance attestation via " +
			"the gh CLI), and atomically replace the running binary.\n\n" +
			"SHA256 always gates the install and fails closed on a mismatch. The attestation " +
			"is verified only when a usable gh (>= " + selfupdate.GhMinVersion + ") is present; " +
			"--verify require makes it mandatory, --verify off skips it (SHA256 stays " +
			"mandatory). The outbound check is anonymous (no token) and the update never " +
			"runs unless you invoke it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := selfupdate.ParseVerifyMode(verifyFlag)
			if err != nil {
				return err
			}
			_, err = selfupdate.Apply(cmd.Context(), selfupdate.Options{
				Current:   version.Version,
				Target:    targetVersion,
				Mode:      mode,
				CheckOnly: checkOnly,
				Out:       cmd.OutOrStdout(),
			})
			return err
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check-only", false, "report whether a newer release exists; download and install nothing")
	cmd.Flags().StringVar(&targetVersion, "version", "", "install a specific release tag (e.g. v0.11.0) instead of the latest; may install an OLDER release (a downgrade) — the named tag's integrity/attestation is still verified")
	cmd.Flags().StringVar(&verifyFlag, "verify", "auto", "attestation verification policy: auto|require|off")
	return cmd
}
