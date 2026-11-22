//go:build unix

package scrapling

import (
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

const browserProcessSupported = true

func runBrowserCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var canceled atomic.Bool
	cmd.Cancel = func() error {
		canceled.Store(true)
		// Patchright owns Chromium in a separate process group. Give its Node
		// driver the supported SIGTERM cleanup path before killing the driver.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
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
	err := cmd.Wait()
	if canceled.Load() {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return err
}
