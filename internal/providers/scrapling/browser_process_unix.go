//go:build unix

package scrapling

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const browserProcessSupported = true

func runBrowserCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	// Bound inherited pipe cleanup after exit as well as after timeout.
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Start(); err != nil {
		return err
	}
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return cmd.Wait()
}
