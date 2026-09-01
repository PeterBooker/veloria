//go:build unix

package main

import (
	"path/filepath"
	"testing"
)

func TestAcquireSlugLockExclusive(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "security-ninja.lock")

	// flock conflicts across open file descriptions, so a second open in the
	// same process is enough to prove exclusivity.
	release, err := acquireSlugLock(path)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}

	if _, err := acquireSlugLock(path); err == nil {
		t.Fatal("expected second lock on held slug to fail")
	}

	release()

	release2, err := acquireSlugLock(path)
	if err != nil {
		t.Fatalf("lock after release failed: %v", err)
	}
	release2()
}
