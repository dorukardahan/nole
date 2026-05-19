//go:build windows

package core

import (
	"os"
	"syscall"
	"unsafe"
)

const ledgerLockExclusive = 0x2

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procLockLedgerFile   = kernel32.NewProc("LockFileEx")
	procUnlockLedgerFile = kernel32.NewProc("UnlockFileEx")
)

func lockLedgerFile(file *os.File) error {
	var overlapped syscall.Overlapped
	r1, _, err := procLockLedgerFile.Call(
		uintptr(file.Fd()),
		uintptr(ledgerLockExclusive),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

func unlockLedgerFile(file *os.File) error {
	var overlapped syscall.Overlapped
	r1, _, err := procUnlockLedgerFile.Call(
		uintptr(file.Fd()),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}
