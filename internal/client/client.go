package client

import (
	"net"
	"net/http"
	"runtime"
	"time"
)

const UserAgent = "Veloria/1.0"

// httpClients holds HTTP clients for API and ZIP operations.
type httpClients struct {
	api *http.Client
	zip *http.Client
}

func newHTTPClients() *httpClients {
	transport := newTransport()
	return &httpClients{
		api: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		zip: &http.Client{
			Timeout:   180 * time.Second,
			Transport: transport,
		},
	}
}

var defaultClients = newHTTPClients()

// GetAPI returns the default API HTTP client.
func GetAPI() *http.Client {
	return defaultClients.api
}

// GetZip returns the default ZIP download HTTP client.
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
