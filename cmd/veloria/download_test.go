package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"veloria/internal/repo"
)

var zipMagic = []byte{'P', 'K', 0x03, 0x04}

// shortRetryWait makes the default wait between attempts negligible for the
// duration of one test. Tests using it must not run in parallel.
func shortRetryWait(t *testing.T) {
	t.Helper()
	old := downloadDefaultRetryWait
	downloadDefaultRetryWait = 10 * time.Millisecond
	t.Cleanup(func() { downloadDefaultRetryWait = old })
}

func exitCodeOf(err error) (int, bool) {
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

func TestDownloadZip_503ThenSuccess(t *testing.T) {
	shortRetryWait(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(zipMagic)
	}))
	defer srv.Close()

	path, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
	if err != nil {
		t.Fatalf("expected success after a 503, got: %v", err)
	}
	defer cleanup()
	if hits.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", hits.Load())
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, zipMagic) {
		t.Errorf("downloaded content mismatch: %q err=%v", data, err)
	}
}

func TestDownloadZip_503OnEveryAttemptSignalsUnavailable(t *testing.T) {
	shortRetryWait(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, _, err := downloadZip(srv.URL + "/download/plugin/dynacat.1.3.zip")
	if err == nil {
		t.Fatal("expected error")
	}
	if code, ok := exitCodeOf(err); !ok || code != repo.ExitDownloadUnavailable {
		t.Errorf("expected exit code %d for persistent 503, got: %v", repo.ExitDownloadUnavailable, err)
	}
	if hits.Load() != downloadMaxRetries {
		t.Errorf("expected %d attempts, got %d", downloadMaxRetries, hits.Load())
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should carry the upstream status, got: %v", err)
	}
	if strings.Contains(err.Error(), "url:") {
		t.Errorf("IndexCmd.Run appends the URL; downloadZip must not, got: %v", err)
	}
}

func TestDownloadZip_5xxHonoursRetryAfter(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write(zipMagic)
	}))
	defer srv.Close()

	start := time.Now()
	_, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
	if err != nil {
		t.Fatalf("expected success after a 502, got: %v", err)
	}
	defer cleanup()
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("expected to wait for Retry-After, only waited %s", elapsed)
	}
}

func TestDownloadZip_TransportErrorRetried(t *testing.T) {
	shortRetryWait(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			// Drop the connection without a response.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack failed: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write(zipMagic)
	}))
	defer srv.Close()

	_, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
	if err != nil {
		t.Fatalf("expected success after a dropped connection, got: %v", err)
	}
	defer cleanup()
	if hits.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", hits.Load())
	}
}

func TestDownloadZip_TruncatedBodyRetriedFromScratch(t *testing.T) {
	shortRetryWait(t)
	full := append(append([]byte{}, zipMagic...), []byte("complete archive")...)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			// Promise more bytes than are sent: the client sees an unexpected EOF mid-body.
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write(full[:8])
			return
		}
		_, _ = w.Write(full)
	}))
	defer srv.Close()

	path, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
	if err != nil {
		t.Fatalf("expected success after a truncated body, got: %v", err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, full) {
		t.Errorf("partial first attempt leaked into the file: got %q err=%v", data, err)
	}
}

func TestDownloadZip_TransportErrorOnEveryAttemptSignalsUnavailable(t *testing.T) {
	shortRetryWait(t)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/plugin.zip"
	srv.Close() // nothing is listening any more

	_, _, err := downloadZip(url)
	if code, ok := exitCodeOf(err); !ok || code != repo.ExitDownloadUnavailable {
		t.Errorf("expected exit code %d for a persistent transport failure, got: %v", repo.ExitDownloadUnavailable, err)
	}
}

func TestDownloadZip_429OnEveryAttemptIsPlainError(t *testing.T) {
	shortRetryWait(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, _, err := downloadZip(srv.URL + "/plugin.zip")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := exitCodeOf(err); ok {
		t.Errorf("rate limiting must stay a plain retryable error, got exit-coded: %v", err)
	}
	if hits.Load() != downloadMaxRetries {
		t.Errorf("expected %d attempts, got %d", downloadMaxRetries, hits.Load())
	}
}
