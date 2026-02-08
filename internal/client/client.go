package client

import (
	"net"
	"net/http"
	"runtime"
	"time"
)

const UserAgent = "Veloria/1.0"

// HTTPClients holds injectable HTTP clients for API and ZIP operations.
type HTTPClients struct {
	API *http.Client
	Zip *http.Client
}

// NewHTTPClients creates a new set of HTTP clients with default transport settings.
func NewHTTPClients() *HTTPClients {
	transport := newTransport()
	return &HTTPClients{
		API: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		Zip: &http.Client{
			Timeout:   180 * time.Second,
			Transport: transport,
		},
	}
}

// Package-level default clients for backwards compatibility.
var defaultClients = NewHTTPClients()

// GetAPI returns the default API HTTP client.
func GetAPI() *http.Client {
	return defaultClients.API
}

// GetZip returns the default ZIP download HTTP client.
func GetZip() *http.Client {
	return defaultClients.Zip
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
