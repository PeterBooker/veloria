//go:build !unix

package main

// acquireSlugLock is a no-op on platforms without flock support.
func acquireSlugLock(string) (func(), error) {
	return func() {}, nil
}
