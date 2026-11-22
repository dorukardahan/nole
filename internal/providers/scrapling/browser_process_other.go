//go:build !unix

package scrapling

import (
	"errors"
	"os/exec"
)

const browserProcessSupported = false

func runBrowserCommand(*exec.Cmd) error {
	return errors.New("scrapling: browser process cleanup unavailable on this platform")
}
