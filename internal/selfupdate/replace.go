package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// installBinary atomically replaces the running executable at exePath with
// newBytes. It stages a NEW file in exePath's own directory (so the final rename
// is a same-filesystem, atomic operation) and renames it into place — it NEVER
// truncates/overwrites the running inode, which is what keeps it Apple-Silicon
// codesign-safe (overwriting a mapped, signed Mach-O in place SIGKILLs the next
// exec). A failure before the rename leaves the existing binary untouched.
//
// Staging uses os.CreateTemp (O_CREATE|O_EXCL + a random name), so a pre-planted
// symlink or a recycled-PID collision in a writable install dir cannot redirect
// or hijack the staged write.
func installBinary(newBytes []byte, exePath string) error {
	dir := filepath.Dir(exePath)

	f, err := os.CreateTemp(dir, ".nole.update-*")
	if err != nil {
		return fmt.Errorf("stage new binary in %s (existing install left untouched): %w", dir, err)
	}
	staged := f.Name()
	cleanup := func() { _ = os.Remove(staged) }
	if _, werr := f.Write(newBytes); werr != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("write staged binary (existing install left untouched): %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		cleanup()
		return fmt.Errorf("flush staged binary (existing install left untouched): %w", cerr)
	}
	// os.CreateTemp makes a 0600 file; a binary must be executable.
	if cherr := os.Chmod(staged, 0o755); cherr != nil {
		cleanup()
		return fmt.Errorf("chmod staged binary (existing install left untouched): %w", cherr)
	}

	if runtime.GOOS == "windows" {
		// Windows holds an image-section lock on a running .exe: it cannot be
		// deleted or overwritten, but it CAN be renamed away. Move the running
		// binary aside, move the new one into place, then leave the old behind —
		// it cannot be deleted while this process runs. The NEXT self-update
		// removes it (the os.Remove(old) below clears a prior update's leftover,
		// whose process has since exited).
		old := exePath + ".old"
		_ = os.Remove(old) // clear a stale .old from a PRIOR self-update
		if rerr := os.Rename(exePath, old); rerr != nil {
			cleanup()
			return fmt.Errorf("move running binary aside (existing install left untouched): %w", rerr)
		}
		if rerr := os.Rename(staged, exePath); rerr != nil {
			_ = os.Rename(old, exePath) // roll back: never leave the user without a binary
			cleanup()
			return fmt.Errorf("move new binary into place (original restored): %w", rerr)
		}
		return nil
	}

	// Unix (Linux + macOS): rename(2) atomically swaps the directory entry. The
	// running process keeps executing its open image; the new binary takes effect
	// on the next exec.
	if rerr := os.Rename(staged, exePath); rerr != nil {
		cleanup()
		return fmt.Errorf("install new binary (existing install left untouched): %w", rerr)
	}
	return nil
}
