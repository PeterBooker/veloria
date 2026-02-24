package mcp

import (
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

// NewHTTPHandler creates an http.Handler that serves the MCP protocol
// over streamable HTTP transport.
func NewHTTPHandler(mcpServer *server.MCPServer) http.Handler {
	return server.NewStreamableHTTPServer(mcpServer)
}
