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

func startOwnedBrowser(*exec.Cmd) (func(), <-chan struct{}, error) {
	return nil, nil, errors.New("scrapling: browser process cleanup unavailable on this platform")
}
