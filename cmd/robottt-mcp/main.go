// Command robottt-mcp is an MCP server that translates tool calls into HTTP
// requests against the robottt HTTP API (cmd/robottt), so an MCP-capable
// LLM client can drive the robot over the network. Run alongside robottt,
// not instead of it.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"robottt/internal/mcpserver"
)

const (
	defaultRobotAPIURL = "http://localhost:8080"
	defaultListenAddr  = ":8081"
)

func main() {
	baseURL := os.Getenv("ROBOT_API_URL")
	if baseURL == "" {
		baseURL = defaultRobotAPIURL
	}
	listenAddr := os.Getenv("MCP_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	wrapper, err := mcpserver.New(baseURL)
	if err != nil {
		log.Fatalf("mcpserver: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "robottt", Version: "1.0.0"}, nil)
	wrapper.RegisterTools(server)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	log.Printf("mcp server listening on %s (robot API at %s)", listenAddr, baseURL)
	if err := http.ListenAndServe(listenAddr, handler); err != nil {
		log.Fatalf("mcp http server: %v", err)
	}
}
