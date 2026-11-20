package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

// countingServer is a loopback server that counts the requests reaching it.
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if h != nil {
			h(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// allowOnly exempts exactly the given test servers from the public-address
// rule, so a test can reach its own front server while every other loopback
// address stays refused.
func allowOnly(t *testing.T, servers ...*httptest.Server) func(netip.AddrPort) bool {
	t.Helper()
	allowed := make(map[netip.AddrPort]bool, len(servers))
	for _, srv := range servers {
		ap, err := netip.ParseAddrPort(srv.Listener.Addr().String())
		if err != nil {
			t.Fatalf("parse listener address: %v", err)
		}
		allowed[ap] = true
	}
	return func(ap netip.AddrPort) bool { return allowed[ap] }
}

// get performs a GET and returns the status code, closing the body.
func get(t *testing.T, c *http.Client, url string) (int, error) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func assertRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("request succeeded, want it refused")
	}
	if !errors.Is(err, ErrDownloadRefused) {
		t.Fatalf("error does not wrap ErrDownloadRefused: %v", err)
	}
}

func TestIsPublicAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		// Loopback, unspecified, "this network".
		{"127.0.0.1", false},
		{"127.255.255.254", false},
		{"0.0.0.0", false},
		{"0.1.2.3", false},
		{"::1", false},
		{"::", false},
		// Private (RFC 1918), CGNAT, link-local.
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"192.168.1.1", false},
		{"100.64.0.1", false},
		{"100.127.255.255", false},
		{"169.254.169.254", false},
		// Reserved, documentation, benchmarking, multicast, broadcast.
		{"192.0.0.1", false},
		{"192.0.2.1", false},
		{"192.88.99.1", false},
		{"198.18.0.1", false},
		{"198.51.100.1", false},
		{"203.0.113.1", false},
		{"224.0.0.1", false},
		{"240.0.0.1", false},
		{"255.255.255.255", false},
		// IPv6 unique-local, link-local (with and without zone), multicast.
		{"fd00::1", false},
		{"fc00::1", false},
		{"fe80::1", false},
		{"fe80::1%lo", false},
		{"ff02::1", false},
		// IPv6 reserved ranges inside and outside global unicast.
		{"2001:db8::1", false},
		{"2001::1", false},
		{"2002:7f00:1::1", false},
		{"3fff::1", false},
		{"100::1", false},
		{"64:ff9b:1::1", false},
		{"::7f00:1", false},
		// IPv4-mapped IPv6 follows the IPv4 address it reaches.
		{"::ffff:127.0.0.1", false},
		{"::ffff:10.0.0.1", false},
		{"::ffff:169.254.169.254", false},
		{"::ffff:93.184.216.34", true},
		// NAT64 follows the embedded IPv4 address.
		{"64:ff9b::7f00:1", false},
		{"64:ff9b::a00:1", false},
		{"64:ff9b::5db8:d822", true},
		// Public.
		{"93.184.216.34", true},
		{"1.1.1.1", true},
		{"172.15.255.255", true},
		{"172.32.0.1", true},
		{"100.63.255.255", true},
		{"100.128.0.1", true},
		{"2606:4700::1111", true},
		{"2a00:1450:4001::200e", true},
	}
	for _, tt := range tests {
		if got := isPublicAddr(netip.MustParseAddr(tt.addr)); got != tt.want {
			t.Errorf("isPublicAddr(%s) = %v, want %v", tt.addr, got, tt.want)
		}
	}
	if isPublicAddr(netip.Addr{}) {
		t.Error("isPublicAddr(zero Addr) = true, want false")
	}
}

func TestPublicOnlyControl(t *testing.T) {
	control := publicOnlyControl(nil)
	for _, address := range []string{
		"127.0.0.1:80",
		"[::1]:80",
		"[::ffff:127.0.0.1]:80",
		"[fe80::1%lo]:80",
		"169.254.169.254:80",
		"10.0.0.1:443",
		"not-an-address",
		"example.com:80",
	} {
		if err := control("tcp", address, nil); !errors.Is(err, ErrDownloadRefused) {
			t.Errorf("control(%q) = %v, want ErrDownloadRefused", address, err)
		}
	}
	for _, address := range []string{"93.184.216.34:443", "[2606:4700::1111]:443"} {
		if err := control("tcp", address, nil); err != nil {
			t.Errorf("control(%q) = %v, want nil", address, err)
		}
	}

	// The exemption applies to the exact address and port only.
	exempt := netip.MustParseAddrPort("127.0.0.1:8080")
	control = publicOnlyControl(func(ap netip.AddrPort) bool { return ap == exempt })
	if err := control("tcp", "127.0.0.1:8080", nil); err != nil {
		t.Errorf("exempt address refused: %v", err)
	}
	if err := control("tcp", "127.0.0.1:8081", nil); !errors.Is(err, ErrDownloadRefused) {
		t.Errorf("non-exempt port = %v, want ErrDownloadRefused", err)
	}
}

func TestZipClient_RefusesLoopbackURL(t *testing.T) {
	srv, hits := countingServer(t, nil)

	_, err := get(t, GetZip(), srv.URL+"/plugin.zip")
	assertRefused(t, err)
	if hits.Load() != 0 {
		t.Errorf("loopback server received %d requests, want 0", hits.Load())
	}
}

func TestZipClient_RefusesIPv4MappedLoopbackURL(t *testing.T) {
	srv, hits := countingServer(t, nil)
	ap := netip.MustParseAddrPort(srv.Listener.Addr().String())
	if !ap.Addr().Is4() {
		t.Skipf("test server is not listening on IPv4: %s", ap)
	}
	mapped := fmt.Sprintf("http://[::ffff:%s]:%d/plugin.zip", ap.Addr(), ap.Port())

	_, err := get(t, GetZip(), mapped)
	assertRefused(t, err)
	if hits.Load() != 0 {
		t.Errorf("loopback server received %d requests via %s, want 0", hits.Load(), mapped)
	}
}

func TestZipClient_RefusesRedirectToLoopback(t *testing.T) {
	internal, internalHits := countingServer(t, nil)
	front, frontHits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/internal-only/status", http.StatusFound)
	})

	// Only the front server is exempt; the redirect target is not.
	c := newZipClient(allowOnly(t, front))
	_, err := get(t, c, front.URL+"/plugin.zip")
	assertRefused(t, err)
	if frontHits.Load() != 1 {
		t.Errorf("front server received %d requests, want 1", frontHits.Load())
	}
	if internalHits.Load() != 0 {
		t.Errorf("redirect target received %d requests, want 0", internalHits.Load())
	}
}

func TestZipClient_RefusesRedirectToMetadataAddress(t *testing.T) {
	front, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})

	// The dial hook refuses before any packet is sent, so nothing leaves the host.
	c := newZipClient(allowOnly(t, front))
	_, err := get(t, c, front.URL+"/plugin.zip")
	assertRefused(t, err)
	if !strings.Contains(err.Error(), "169.254.169.254") {
		t.Errorf("error should name the refused address for the operator log, got: %v", err)
	}
}

func TestZipClient_RefusesNonHTTPScheme(t *testing.T) {
	for _, url := range []string{"file:///etc/hostname", "ftp://203.0.113.1/plugin.zip", "gopher://203.0.113.1/"} {
		_, err := get(t, GetZip(), url)
		if err == nil || !errors.Is(err, ErrDownloadRefused) {
			t.Errorf("GET %s = %v, want ErrDownloadRefused", url, err)
		}
	}

	// A redirect to another scheme is refused as well.
	front, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "file:///etc/hostname")
		w.WriteHeader(http.StatusFound)
	})
	_, err := get(t, newZipClient(allowOnly(t, front)), front.URL+"/plugin.zip")
	assertRefused(t, err)
}

func TestZipClient_RedirectCap(t *testing.T) {
	// /hop/N redirects to /hop/N-1 and /hop/0 answers 200.
	front, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/hop/%d", &n); err != nil || n == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/hop/%d", n-1), http.StatusFound)
	})
	c := newZipClient(allowOnly(t, front))

	status, err := get(t, c, fmt.Sprintf("%s/hop/%d", front.URL, zipMaxRedirects))
	if err != nil || status != http.StatusOK {
		t.Fatalf("%d redirects: status=%d err=%v, want 200", zipMaxRedirects, status, err)
	}

	_, err = get(t, c, fmt.Sprintf("%s/hop/%d", front.URL, zipMaxRedirects+1))
	assertRefused(t, err)
}

func TestZipClient_FetchThroughTestSeam(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	status, err := get(t, newZipClient(allowOnly(t, srv)), srv.URL+"/plugin.zip")
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v, want 200", status, err)
	}
	if hits.Load() != 1 {
		t.Errorf("server received %d requests, want 1", hits.Load())
	}
}

func TestZipTransport_IgnoresEnvironmentProxy(t *testing.T) {
	if newZipTransport(nil).Proxy != nil {
		t.Error("ZIP transport must not use a proxy: the dial hook would check the proxy address")
	}
	if newTransport().Proxy == nil {
		t.Error("API transport should keep using the environment proxy")
	}
}

func TestGetAPI_NotRestricted(t *testing.T) {
	srv, hits := countingServer(t, nil)

	status, err := get(t, GetAPI(), srv.URL+"/info")
	if err != nil || status != http.StatusOK {
		t.Fatalf("API client: status=%d err=%v, want 200", status, err)
	}
	if hits.Load() != 1 {
		t.Errorf("server received %d requests, want 1", hits.Load())
	}
}
