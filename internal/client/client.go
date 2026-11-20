package client

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const UserAgent = "Veloria/1.0"

// zipMaxRedirects is the number of redirects the ZIP client follows before
// giving up.
const zipMaxRedirects = 5

// ErrDownloadRefused is wrapped by every error with which the ZIP client
// refuses a request on policy grounds: a non-public address, a scheme other
// than http or https, or too many redirects. Retrying cannot change the result.
var ErrDownloadRefused = errors.New("download refused")

// httpClients holds HTTP clients for API and ZIP operations.
type httpClients struct {
	api *http.Client
	zip *http.Client
}

func newHTTPClients() *httpClients {
	return &httpClients{
		api: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(newTransport()),
		},
		zip: newZipClient(nil),
	}
}

var defaultClients = newHTTPClients()

// GetAPI returns the default API HTTP client.
func GetAPI() *http.Client {
	return defaultClients.api
}

// GetZip returns the default ZIP download HTTP client. Archive URLs come from
// upstream metadata, so this client only connects to public addresses, only
// speaks http and https, follows at most zipMaxRedirects redirects, and
// ignores the environment proxy.
func GetZip() *http.Client {
	return defaultClients.zip
}

func newTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}
}

// newZipClient builds the ZIP download client. allow exempts individual
// addresses from the public-address rule; it is nil everywhere except in
// tests, which need to reach their own loopback listeners.
func newZipClient(allow func(netip.AddrPort) bool) *http.Client {
	return &http.Client{
		Timeout:       180 * time.Second,
		Transport:     schemeGuard{next: otelhttp.NewTransport(newZipTransport(allow))},
		CheckRedirect: zipCheckRedirect,
	}
}

// newZipTransport is newTransport with the public-address dial hook and
// without the environment proxy: behind a proxy the hook would see the
// proxy's address, not the destination's.
func newZipTransport(allow func(netip.AddrPort) bool) *http.Transport {
	transport := newTransport()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   publicOnlyControl(allow),
	}).DialContext
	return transport
}

// schemeGuard rejects requests whose URL scheme is not http or https. As a
// RoundTripper it sees the initial request and every redirect.
type schemeGuard struct {
	next http.RoundTripper
}

func (g schemeGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if s := req.URL.Scheme; s != "http" && s != "https" {
		return nil, fmt.Errorf("%w: unsupported URL scheme %q", ErrDownloadRefused, s)
	}
	return g.next.RoundTrip(req)
}

// zipCheckRedirect stops a redirect chain after zipMaxRedirects hops.
func zipCheckRedirect(_ *http.Request, via []*http.Request) error {
	if len(via) > zipMaxRedirects {
		return fmt.Errorf("%w: more than %d redirects", ErrDownloadRefused, zipMaxRedirects)
	}
	return nil
}

// publicOnlyControl returns a net.Dialer Control hook that refuses non-public
// addresses. The hook runs after name resolution, on the literal address
// about to be connected, so it holds for every redirect hop and DNS answer.
func publicOnlyControl(allow func(netip.AddrPort) bool) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("%w: unparseable address %q", ErrDownloadRefused, address)
		}
		if allow != nil && allow(ap) {
			return nil
		}
		if !isPublicAddr(ap.Addr()) {
			return fmt.Errorf("%w: non-public address %s", ErrDownloadRefused, ap.Addr())
		}
		return nil
	}
}

var (
	// nonPublicV4 lists the IPv4 special-purpose ranges that are not publicly
	// routable unicast space.
	nonPublicV4 = mustPrefixes(
		"0.0.0.0/8",       // "this network", unspecified
		"10.0.0.0/8",      // private (RFC 1918)
		"100.64.0.0/10",   // carrier-grade NAT
		"127.0.0.0/8",     // loopback
		"169.254.0.0/16",  // link-local, cloud metadata
		"172.16.0.0/12",   // private (RFC 1918)
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // documentation
		"192.88.99.0/24",  // 6to4 relay anycast (deprecated)
		"192.168.0.0/16",  // private (RFC 1918)
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"224.0.0.0/4",     // multicast
		"240.0.0.0/4",     // reserved, broadcast
	)

	// globalV6 is the IPv6 global unicast space. Everything outside it
	// (loopback, unspecified, unique-local, link-local, multicast, ...) is
	// non-public, apart from nat64 below.
	globalV6 = netip.MustParsePrefix("2000::/3")

	// nonPublicV6 lists the special-purpose ranges inside globalV6.
	nonPublicV6 = mustPrefixes(
		"2001::/23",     // IETF protocol assignments, Teredo
		"2001:db8::/32", // documentation
		"2002::/16",     // 6to4, embeds an arbitrary IPv4 address
		"3fff::/20",     // documentation
	)

	// nat64 is the well-known NAT64 prefix. Its last four bytes are the IPv4
	// address the translator connects to, which is what has to be public.
	nat64 = netip.MustParsePrefix("64:ff9b::/96")
)

func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

// isPublicAddr reports whether ip is a publicly routable unicast address.
func isPublicAddr(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	// Unmap turns an IPv4-mapped IPv6 address into the IPv4 address it reaches.
	ip = ip.Unmap().WithZone("")

	if ip.Is6() {
		if nat64.Contains(ip) {
			b := ip.As16()
			return isPublicAddr(netip.AddrFrom4([4]byte(b[12:])))
		}
		if !globalV6.Contains(ip) {
			return false
		}
		return !containsAddr(nonPublicV6, ip)
	}
	return !containsAddr(nonPublicV4, ip)
}

func containsAddr(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
