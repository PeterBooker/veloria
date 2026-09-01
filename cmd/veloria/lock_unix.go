//go:build unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

// acquireSlugLock takes an exclusive non-blocking flock on path. The kernel
// releases it even on SIGKILL, so a dead indexer can never leave a slug stuck.
func acquireSlugLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640) // #nosec G304 -- path is built from a validated slug
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another indexer is already running for this slug: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
