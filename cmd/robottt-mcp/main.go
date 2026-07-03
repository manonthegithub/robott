// Command robottt-mcp is an MCP server that translates tool calls into HTTP
// requests against the robottt HTTP API (cmd/robottt), so an MCP-capable
// LLM client can drive the robot. Run alongside robottt, not instead of it.
package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"robottt/internal/mcpserver"
)

const defaultRobotAPIURL = "http://localhost:8080"

func main() {
	baseURL := os.Getenv("ROBOT_API_URL")
	if baseURL == "" {
		baseURL = defaultRobotAPIURL
	}

	wrapper, err := mcpserver.New(baseURL)
	if err != nil {
		log.Fatalf("mcpserver: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "robottt", Version: "1.0.0"}, nil)
	wrapper.RegisterTools(server)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}
