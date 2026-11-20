package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8:aaaa::/48"),
	}

	tests := []struct {
		name    string
		trusted []netip.Prefix
		peer    string
		xff     []string
		headers map[string]string
		want    string
	}{
		{
			name: "untrusted peer keeps its address",
			peer: "203.0.113.10:40000",
			xff:  []string{"192.0.2.1"},
			want: "203.0.113.10:40000",
		},
		{
			name:    "no trusted proxies configured",
			trusted: []netip.Prefix{},
			peer:    "10.0.0.1:40000",
			xff:     []string{"192.0.2.1"},
			want:    "10.0.0.1:40000",
		},
		{
			name: "trusted peer without the header",
			peer: "10.0.0.1:40000",
			want: "10.0.0.1:40000",
		},
		{
			name: "single entry",
			peer: "10.0.0.1:40000",
			xff:  []string{"198.51.100.7"},
			want: "198.51.100.7",
		},
		{
			name: "right-most untrusted entry wins over a spoofed one",
			peer: "10.0.0.1:40000",
			xff:  []string{"192.0.2.1, 198.51.100.7"},
			want: "198.51.100.7",
		},
		{
			name: "trusted hops on the right are skipped",
			peer: "10.0.0.1:40000",
			xff:  []string{"192.0.2.1,198.51.100.7 , 10.0.0.3,10.0.0.2"},
			want: "198.51.100.7",
		},
		{
			name: "multiple header lines are read as one list",
			peer: "10.0.0.1:40000",
			xff:  []string{"192.0.2.1", "198.51.100.7", "10.0.0.2"},
			want: "198.51.100.7",
		},
		{
			name: "every entry trusted keeps the peer",
			peer: "10.0.0.1:40000",
			xff:  []string{"10.0.0.3, 10.0.0.2"},
			want: "10.0.0.1:40000",
		},
		{
			name: "malformed entry before any client keeps the peer",
			peer: "10.0.0.1:40000",
			xff:  []string{"192.0.2.1, unknown, 10.0.0.2"},
			want: "10.0.0.1:40000",
		},
		{
			name: "malformed entry left of the client is never read",
			peer: "10.0.0.1:40000",
			xff:  []string{"unknown, 198.51.100.7"},
			want: "198.51.100.7",
		},
		{
			name: "trailing comma keeps the peer",
			peer: "10.0.0.1:40000",
			xff:  []string{"198.51.100.7,"},
			want: "10.0.0.1:40000",
		},
		{
			name: "X-Real-IP and True-Client-IP are ignored",
			peer: "10.0.0.1:40000",
			headers: map[string]string{
				"X-Real-IP":      "192.0.2.1",
				"True-Client-IP": "192.0.2.2",
			},
			want: "10.0.0.1:40000",
		},
		{
			name: "IPv6 proxy and client",
			peer: "[2001:db8:aaaa::1]:40000",
			xff:  []string{"192.0.2.1, 2001:db8:bbbb::7"},
			want: "2001:db8:bbbb::7",
		},
		{
			name: "IPv4-mapped peer matches an IPv4 prefix",
			peer: "[::ffff:10.0.0.1]:40000",
			xff:  []string{"198.51.100.7"},
			want: "198.51.100.7",
		},
		{
			name: "IPv4-mapped trusted hop is skipped",
			peer: "10.0.0.1:40000",
			xff:  []string{"198.51.100.7, ::ffff:10.0.0.2"},
			want: "198.51.100.7",
		},
		{
			name: "unparseable peer keeps its address",
			peer: "@",
			xff:  []string{"198.51.100.7"},
			want: "@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prefixes := trusted
			if tt.trusted != nil {
				prefixes = tt.trusted
			}

			var got string
			h := ClientIP(prefixes)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = r.RemoteAddr
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.peer
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if got != tt.want {
				t.Errorf("RemoteAddr = %q, want %q", got, tt.want)
			}
		})
	}
}
