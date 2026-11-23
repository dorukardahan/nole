//go:build unix

package scrapling

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const browserProcessSupported = true

func startOwnedBrowser(cmd *exec.Cmd) (func(), <-chan struct{}, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 500 * time.Millisecond
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
			}
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		})
	}
	return stop, done, nil
}

func runBrowserCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var canceled atomic.Bool
	cmd.Cancel = func() error {
		canceled.Store(true)
		// This group owns Python and its Node driver only. Chromium is launched
		// separately by Nole through startOwnedBrowser and joined by its owner.
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
