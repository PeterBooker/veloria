package mcp

import (
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

// NewHTTPHandler creates an http.Handler that serves the MCP protocol
// over streamable HTTP transport. The transport is stateless: no tool,
// resource or prompt uses session state, so no session is issued or kept
// for a client that sends initialize.
func NewHTTPHandler(mcpServer *server.MCPServer) http.Handler {
	return server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true))
}
