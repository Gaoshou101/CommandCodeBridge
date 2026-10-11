package bridge

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"
)

// CPA's Windows watcher can briefly hold a credential without delete sharing
// after applying its disabled state. Retry that lock, checking that no external
// edit replaced the validated file while we waited.
func removeCredentialFile(path string, original []byte) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, err := os.ReadFile(path)
		if err == nil {
			if !bytes.Equal(current, original) {
				return fail(409, "credential changed while waiting for removal; retry deletion")
			}
			err = os.Remove(path)
		}
		if err == nil {
			return nil
		}
		// Win32 access denied (5), sharing violation (32), and lock violation
		// (33). Do not turn unrelated failures into retries on other systems.
		locked := runtime.GOOS == "windows" && (errors.Is(err, syscall.Errno(5)) || errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33)))
		if !locked || !time.Now().Before(deadline) {
			return fail(500, "credential removal failed")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
