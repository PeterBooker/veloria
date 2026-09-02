package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"veloria/internal/config"
	"veloria/internal/index"
)

const (
	mirrorURL        = "https://fastly.api.aspirecloud.net/download/plugin/dynacat.1.3.zip"
	versionedWPURL   = "https://downloads.wordpress.org/plugin/dynacat.1.3.zip"
	versionlessWPURL = "https://downloads.wordpress.org/plugin/dynacat.zip"
)

// scriptedRunner stands in for the index subprocess: it answers each call
// from results in order and records the URLs it was asked for.
type scriptedRunner struct {
	results []func() (*indexerResult, error)
	urls    []string
}

func (s *scriptedRunner) run(_ string, url string) (*indexerResult, error) {
	s.urls = append(s.urls, url)
	i := len(s.urls) - 1
	if i >= len(s.results) {
		return nil, fmt.Errorf("unexpected attempt %d for %s", i+1, url)
	}
	return s.results[i]()
}

func fails(err error) func() (*indexerResult, error) {
	return func() (*indexerResult, error) { return nil, err }
}

func newTaskStore(t *testing.T, runner *scriptedRunner, source string) (*ExtensionStore[*Plugin], *Plugin) {
	t.Helper()
	store := NewExtensionStore(StoreConfig[*Plugin]{
		Ctx:           context.Background(),
		Config:        &config.Config{DataDir: t.TempDir()},
		Logger:        zap.NewNop(),
		ExtensionType: TypePlugins,
	})
	store.indexRunner = runner.run
	p := &Plugin{
		IndexedExtension: NewIndexedExtension(),
		Slug:             "dynacat",
		Source:           source,
		Version:          "1.3",
		DownloadLink:     mirrorURL,
	}
	store.Set(p.Slug, p)
	return store, p
}

// builtIndex writes a real trigram index for a one-file source tree and
// returns the directory index.Open expects.
func builtIndex(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "dynacat.php"), []byte("<?php echo 'hello';"), 0o600))
	slugDir := t.TempDir()
	index.IndexDirToFile(src, filepath.Join(slugDir, "trigrams"), src)
	return slugDir
}

func TestIndexTaskFallsBackToWordPressWhenMirrorUnavailable(t *testing.T) {
	unavailable := fmt.Errorf("%w: download failed on all 3 attempts: 503 Service Unavailable", ErrDownloadUnavailable)
	slugDir := builtIndex(t)
	runner := &scriptedRunner{results: []func() (*indexerResult, error){
		fails(unavailable),
		func() (*indexerResult, error) { return &indexerResult{IndexPath: slugDir}, nil },
	}}
	store, p := newTaskStore(t, runner, SourceWordPress)

	err := store.MakeReindexTask(p).Run()
	require.NoError(t, err)
	assert.Equal(t, []string{mirrorURL, versionedWPURL}, runner.urls)
	assert.True(t, p.HasIndex(), "index from the wordpress.org copy must be swapped in")
}

func TestIndexTaskDownloadFailureClassification(t *testing.T) {
	unavailable := fmt.Errorf("%w: 503 Service Unavailable", ErrDownloadUnavailable)
	all := []string{mirrorURL, versionedWPURL, versionlessWPURL}

	tests := []struct {
		name     string
		errs     []error
		wantURLs []string
		wantIs   error
		wantMsg  string
	}{
		{
			name:     "mirror and wordpress.org both unavailable stays retryable",
			errs:     []error{unavailable, unavailable, unavailable},
			wantURLs: all,
			wantIs:   ErrDownloadUnavailable,
		},
		{
			name:     "mirror unavailable but wordpress.org not found stays retryable, not closed",
			errs:     []error{unavailable, ErrDownloadNotFound, ErrDownloadNotFound},
			wantURLs: all,
			wantIs:   ErrDownloadUnavailable,
			wantMsg:  "fallback: download not found",
		},
		{
			name:     "mirror not found but wordpress.org unavailable stays retryable, not closed",
			errs:     []error{ErrDownloadNotFound, unavailable, unavailable},
			wantURLs: all,
			wantIs:   ErrDownloadUnavailable,
		},
		{
			name:     "a failure after download is not retried elsewhere",
			errs:     []error{errors.New("failed to unzip files")},
			wantURLs: []string{mirrorURL},
			wantMsg:  "failed to unzip files",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := make([]func() (*indexerResult, error), len(tc.errs))
			for i, e := range tc.errs {
				results[i] = fails(e)
			}
			runner := &scriptedRunner{results: results}
			store, p := newTaskStore(t, runner, SourceWordPress)

			err := store.MakeReindexTask(p).Run()
			require.Error(t, err)
			assert.Equal(t, tc.wantURLs, runner.urls)
			assert.NotErrorIs(t, err, ErrDownloadSkipped, "must not be treated as closed")
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs)
			}
			if tc.wantMsg != "" {
				assert.Contains(t, err.Error(), tc.wantMsg)
			}
			_, listed := store.Get(p.Slug)
			assert.True(t, listed, "a retryable failure must not unlist the extension")
		})
	}
}

func TestIndexTaskNoFallbackForNonWordPressSource(t *testing.T) {
	unavailable := fmt.Errorf("%w: 503 Service Unavailable", ErrDownloadUnavailable)
	runner := &scriptedRunner{results: []func() (*indexerResult, error){fails(unavailable)}}
	store, p := newTaskStore(t, runner, "github")

	err := store.MakeReindexTask(p).Run()
	assert.ErrorIs(t, err, ErrDownloadUnavailable)
	assert.Equal(t, []string{mirrorURL}, runner.urls)
}

func TestIndexerErrorMapsExitCodes(t *testing.T) {
	exit := func(code int) error {
		return exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	}

	assert.ErrorIs(t, indexerError(exit(ExitDownloadNotFound), "veloria: error: failed to download zip: download unavailable (404) url: https://m/x.zip"), ErrDownloadNotFound)

	err := indexerError(exit(ExitDownloadUnavailable), "veloria: error: failed to download zip: download failed on all 3 attempts: unexpected HTTP status: 503 Service Unavailable url: https://m/x.zip\n")
	assert.ErrorIs(t, err, ErrDownloadUnavailable)
	assert.Contains(t, err.Error(), "503 Service Unavailable url: https://m/x.zip")
	assert.NotContains(t, err.Error(), "veloria: error:")

	err = indexerError(exit(1), "veloria: error: failed to unzip files")
	assert.False(t, errors.Is(err, ErrDownloadNotFound) || errors.Is(err, ErrDownloadUnavailable))
	assert.Equal(t, "failed to unzip files", err.Error())

	assert.Equal(t, "exit status 1", indexerError(exit(1), "").Error(), "no stderr must not produce an empty message")
}
