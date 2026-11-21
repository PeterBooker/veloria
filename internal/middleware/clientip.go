package middleware

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns middleware that sets r.RemoteAddr to the client address
// reported in X-Forwarded-For, but only when the TCP peer is inside one of the
// trusted proxy prefixes. Requests from any other peer keep the TCP peer
// address, so a client cannot choose the address used for rate limiting and
// logging. True-Client-IP and X-Real-IP are never read.
func ClientIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if addr, ok := forwardedClient(r, trusted); ok {
				r.RemoteAddr = addr.String()
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forwardedClient walks X-Forwarded-For from the right, skipping entries
// appended by trusted proxies. The first entry that is not a trusted proxy is
// the client; anything further left was supplied by that client and is never
// used. It reports false when the peer is not trusted, when every entry is a
// trusted proxy, or when a malformed entry is reached first.
func forwardedClient(r *http.Request, trusted []netip.Prefix) (netip.Addr, bool) {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || !isTrusted(trusted, peer.Addr().Unmap()) {
		return netip.Addr{}, false
	}

	values := r.Header.Values("X-Forwarded-For")
	for i := len(values) - 1; i >= 0; i-- {
		rest := values[i]
		for rest != "" {
			entry := rest
			if j := strings.LastIndexByte(rest, ','); j >= 0 {
				entry, rest = rest[j+1:], rest[:j]
			} else {
				rest = ""
			}

			addr, err := netip.ParseAddr(strings.TrimSpace(entry))
			if err != nil {
				return netip.Addr{}, false
			}
			addr = addr.Unmap().WithZone("")
			if !isTrusted(trusted, addr) {
				return addr, true
			}
		}
	}
	return netip.Addr{}, false
}

func isTrusted(trusted []netip.Prefix, addr netip.Addr) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
