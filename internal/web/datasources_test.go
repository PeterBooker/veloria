package web

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"veloria/internal/ui/partial"
)

// storedIndexErrors are index_events.error_message values of the shapes the
// indexer produces, each with the category the public page may show for it.
var storedIndexErrors = []struct {
	stored string
	want   string
}{
	{"", ""},
	{"download not found", failureDownloadNotFound},
	{"download not found, extension skipped", failureDownloadNotFound},
	{
		"download unavailable: failed to download zip: download failed on all 3 attempts: unexpected HTTP status: 503 Service Unavailable url: http://127.0.0.1:37773/busy",
		failureDownloadUnavailable,
	},
	{
		`download unavailable: failed to download zip: download failed on all 3 attempts: Get "http://10.0.0.7:8080/pkg.zip": dial tcp 10.0.0.7:8080: connect: connection refused url: http://10.0.0.7:8080/pkg.zip (fallback: download not found)`,
		failureDownloadUnavailable,
	},
	{
		`download unavailable: failed to download zip: Get "http://169.254.169.254/latest/meta-data/": download refused: non-public address 169.254.169.254 url: https://mirror.example/pkg.zip`,
		failureDownloadUnavailable,
	},
	{
		"failed to download zip: unexpected HTTP status: 401 Unauthorized url: http://127.0.0.1:41189/internal-only/status",
		failureDownloadUnavailable,
	},
	{
		`failed to unzip files into "/etc/veloria/data/plugins/source/akismet.staging": zip: not a valid zip file`,
		failureInvalidArchive,
	},
	{
		`failed to unzip files into "/etc/veloria/data/plugins/source/akismet.staging": archive exceeds maximum total extracted size of 4294967296 bytes`,
		failureInvalidArchive,
	},
	{`failed to lock slug "akismet": open /etc/veloria/data/plugins/source/akismet.lock: permission denied`, failureIndexing},
	{`failed to compress source files in "/etc/veloria/data/plugins/source/akismet.staging": no space left on device`, failureIndexing},
	{"index not ready", failureIndexing},
	{"signal: killed", failureIndexing},
	{"<script>alert(1)</script> https://attacker.example/x", failureIndexing},
}

func TestFailureCategory(t *testing.T) {
	categories := map[string]bool{
		"":                         true,
		failureDownloadNotFound:    true,
		failureDownloadUnavailable: true,
		failureInvalidArchive:      true,
		failureIndexing:            true,
	}
	for _, tt := range storedIndexErrors {
		got := failureCategory(tt.stored)
		if got != tt.want {
			t.Errorf("failureCategory(%q) = %q, want %q", tt.stored, got, tt.want)
		}
		if !categories[got] {
			t.Errorf("failureCategory(%q) = %q, which is not one of the fixed categories", tt.stored, got)
		}
	}
}

func TestFailedIndexPartial_RendersOnlyCategories(t *testing.T) {
	rows := make([]failedIndexRow, len(storedIndexErrors))
	for i, tt := range storedIndexErrors {
		rows[i] = failedIndexRow{Slug: "akismet", ErrorMessage: tt.stored, CreatedAt: time.Now()}
	}

	var buf bytes.Buffer
	data := FailedIndexData{DataSource: "plugins", Events: failedIndexEvents(rows), Page: 1, TotalPages: 1, TotalCount: len(rows)}
	if err := partial.FailedIndex(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	// Nothing from the stored text may reach the page: no URL, host, address,
	// upstream status line or filesystem path.
	for _, leak := range []string{
		"http://", "https://", "127.0.0.1", "10.0.0.7", "169.254.169.254",
		"mirror.example", "attacker.example", "41189", "37773",
		"401", "503", "Unauthorized", "Service Unavailable", "connection refused",
		"/etc/veloria", ".staging", ".lock", "internal-only", "meta-data",
		"permission denied", "no space left", "script",
	} {
		if strings.Contains(html, leak) {
			t.Errorf("failed-indexing partial exposes %q from a stored error", leak)
		}
	}
	// The only link on the page is the slug's own data-source page.
	for _, href := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		if href[1] != "/data-sources/plugins/akismet" {
			t.Errorf("unexpected link target %q", href[1])
		}
	}
	for _, want := range []string{failureDownloadNotFound, failureDownloadUnavailable, failureInvalidArchive, failureIndexing} {
		if !strings.Contains(html, ">"+want+"<") {
			t.Errorf("failed-indexing partial does not show the category %q", want)
		}
	}
}
