package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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

// allowLoopbackDownloads swaps the ZIP client for one without the
// public-address rule, for the duration of one test, so downloadZip can reach
// an httptest server. Tests using it must not run in parallel.
func allowLoopbackDownloads(t *testing.T) {
	t.Helper()
	old := zipHTTPClient
	c := &http.Client{Timeout: 30 * time.Second}
	zipHTTPClient = func() *http.Client { return c }
	t.Cleanup(func() { zipHTTPClient = old })
}

// shrinkDownloadMaxSize lowers the download size limit for the duration of
// one test. Tests using it must not run in parallel.
func shrinkDownloadMaxSize(t *testing.T, limit int64) {
	t.Helper()
	old := downloadMaxSize
	downloadMaxSize = limit
	t.Cleanup(func() { downloadMaxSize = old })
}

// emptyTempDir points os.CreateTemp at a fresh directory and returns a check
// that no download was left behind in it.
func emptyTempDir(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return func() {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read temp dir: %v", err)
		}
		for _, e := range entries {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func exitCodeOf(err error) (int, bool) {
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

func TestDownloadZip_503ThenSuccess(t *testing.T) {
	allowLoopbackDownloads(t)
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
	allowLoopbackDownloads(t)
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
	allowLoopbackDownloads(t)
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
	allowLoopbackDownloads(t)
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
	allowLoopbackDownloads(t)
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
	allowLoopbackDownloads(t)
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
	allowLoopbackDownloads(t)
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

func TestDownloadZip_LoopbackURLRefused(t *testing.T) {
	// No allowLoopbackDownloads: this is the production client.
	assertNoTempFile := emptyTempDir(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(zipMagic)
	}))
	defer srv.Close()

	start := time.Now()
	_, _, err := downloadZip(srv.URL + "/latest/meta-data/")
	if err == nil {
		t.Fatal("expected the loopback download to be refused")
	}
	if hits.Load() != 0 {
		t.Errorf("loopback server received %d requests, want 0", hits.Load())
	}
	if code, ok := exitCodeOf(err); !ok || code != repo.ExitDownloadUnavailable {
		t.Errorf("expected exit code %d for a refused destination, got: %v", repo.ExitDownloadUnavailable, err)
	}
	if !strings.Contains(err.Error(), "download refused") || strings.Contains(err.Error(), "attempts") {
		t.Errorf("expected a single refused attempt, got: %v", err)
	}
	// downloadDefaultRetryWait is not shortened here: a retried refusal would take 20s.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("refused download was retried: took %s", elapsed)
	}
	assertNoTempFile()
}

func TestDownloadZip_NonHTTPSchemeRefused(t *testing.T) {
	assertNoTempFile := emptyTempDir(t)
	for _, u := range []string{"file:///etc/hostname", "ftp://203.0.113.1/plugin.zip"} {
		_, _, err := downloadZip(u)
		if err == nil {
			t.Fatalf("%s: expected error", u)
		}
		if code, ok := exitCodeOf(err); !ok || code != repo.ExitDownloadUnavailable {
			t.Errorf("%s: expected exit code %d, got: %v", u, repo.ExitDownloadUnavailable, err)
		}
		if !strings.Contains(err.Error(), "unsupported URL scheme") || strings.Contains(err.Error(), "attempts") {
			t.Errorf("%s: expected a single refused attempt, got: %v", u, err)
		}
	}
	assertNoTempFile()
}

func TestDownloadZip_BodyOverLimitIsPermanentFailure(t *testing.T) {
	const limit = 1024
	oversized := append(append([]byte{}, zipMagic...), make([]byte, 4*limit)...)
	var gzipped bytes.Buffer
	zw := gzip.NewWriter(&gzipped)
	_, _ = zw.Write(oversized)
	_ = zw.Close()
	if gzipped.Len() >= limit {
		t.Fatalf("gzip fixture is %d bytes, want it under the %d byte limit", gzipped.Len(), limit)
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "declared length",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(oversized)))
				_, _ = w.Write(oversized)
			},
		},
		{
			name: "streamed without length",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.(http.Flusher).Flush()
				_, _ = w.Write(oversized)
			},
		},
		{
			// The wire body is under the limit; what the transport inflates
			// and downloadZip would write to disk is not.
			name: "inflated from a small gzip body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = w.Write(gzipped.Bytes())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortRetryWait(t)
			allowLoopbackDownloads(t)
			shrinkDownloadMaxSize(t, limit)
			assertNoTempFile := emptyTempDir(t)
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				tt.handler(w, r)
			}))
			defer srv.Close()

			_, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
			if err == nil {
				cleanup()
				t.Fatal("expected an oversized body to fail")
			}
			if code, ok := exitCodeOf(err); !ok || code != repo.ExitDownloadNotFound {
				t.Errorf("expected permanent exit code %d, got: %v", repo.ExitDownloadNotFound, err)
			}
			if !strings.Contains(err.Error(), "exceeds maximum size of 1024 bytes") {
				t.Errorf("error should state the limit, got: %v", err)
			}
			if hits.Load() != 1 {
				t.Errorf("expected 1 attempt for a permanent failure, got %d", hits.Load())
			}
			assertNoTempFile()
		})
	}
}

func TestDownloadZip_BodyAtLimitSucceeds(t *testing.T) {
	const limit = 1024
	allowLoopbackDownloads(t)
	shrinkDownloadMaxSize(t, limit)
	body := append(append([]byte{}, zipMagic...), make([]byte, limit-len(zipMagic))...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	path, cleanup, err := downloadZip(srv.URL + "/plugin.zip")
	if err != nil {
		t.Fatalf("a body of exactly the limit must be accepted, got: %v", err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, body) {
		t.Errorf("downloaded content mismatch: %d bytes err=%v", len(data), err)
	}
}

func TestDownloadMaxSizeDefault(t *testing.T) {
	if downloadMaxSize != 1<<30 {
		t.Errorf("downloadMaxSize = %d, want 1 GiB", downloadMaxSize)
	}
}
