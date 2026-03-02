package repo

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// svnHrefRe matches href attributes in SVN directory listing anchor tags.
// Works with both <li> and <tr>-based SVN listing formats.
var svnHrefRe = regexp.MustCompile(`<a[^>]+href="([^"]+)">`)

const (
	svnCoreTagsURL = "https://core.svn.wordpress.org/tags/"

	// svnResponseMaxBytes is the maximum allowed response size for SVN listings.
	// The plugins listing is ~6MB with ~88k entries; 50MB provides headroom.
	svnResponseMaxBytes = 50 << 20 // 50 MiB

	svnFetchTimeout = 2 * time.Minute
)

// svnClient is an HTTP client that mimics a Chrome browser's TLS fingerprint
// to avoid being blocked by anti-bot systems on WordPress SVN servers.
// It forces HTTP/1.1 ALPN because Go's http.Transport cannot handle HTTP/2
// over a custom DialTLSContext.
var svnClient = &http.Client{
	Timeout: svnFetchTimeout,
	Transport: &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 30 * time.Second}
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}
			// Get Chrome's ClientHello spec and override ALPN to HTTP/1.1 only.
			// The rest of the fingerprint (cipher suites, extensions, curves)
			// stays Chrome-like, which is what anti-bot systems check.
			spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
			if err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("utls spec: %w", err)
			}
			for _, ext := range spec.Extensions {
				if alpn, ok := ext.(*utls.ALPNExtension); ok {
					alpn.AlpnProtocols = []string{"http/1.1"}
					break
				}
			}
			tlsConn := utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloCustom)
			if err := tlsConn.ApplyPreset(&spec); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("utls apply preset: %w", err)
			}
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	},
}

// setBrowserHeaders sets standard Chrome browser headers on the request.
func setBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// Accept-Encoding is intentionally omitted so that Go's transport
	// handles gzip decompression transparently.
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
}

// fetchSVNSlugs fetches a WordPress SVN directory listing and returns all
// directory names (slugs/versions). It parses the HTML produced by SVN's
// directory index: <li><a href="slug/">slug/</a></li>.
func fetchSVNSlugs(ctx context.Context, svnURL string) ([]string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, svnFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, svnURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SVN request: %w", err)
	}
	setBrowserHeaders(req)

	resp, err := svnClient.Do(req) // #nosec G704 -- URL from internal constant svnCoreTagsURL
	if err != nil {
		return nil, fmt.Errorf("failed to fetch SVN listing from %s: %w", svnURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), 30*time.Second)
		return nil, fmt.Errorf("SVN server returned 429 (retry after %s) from %s", retryAfter, svnURL)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unexpected status %s from SVN listing %s", resp.Status, svnURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, svnResponseMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read SVN HTML from %s: %w", svnURL, err)
	}

	matches := svnHrefRe.FindAllSubmatch(body, -1)
	var slugs []string
	for _, m := range matches {
		slug := strings.TrimSuffix(string(m[1]), "/")
		if slug == "" || slug == ".." {
			continue
		}
		slugs = append(slugs, slug)
	}

	if len(slugs) == 0 {
		return nil, fmt.Errorf("no entries found in SVN listing at %s (page structure may have changed)", svnURL)
	}

	return slugs, nil
}
