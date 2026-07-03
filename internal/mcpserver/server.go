// Package mcpserver exposes the robot HTTP API as MCP tools, so an
// MCP-capable LLM client can drive the robot. It's a thin translation layer:
// each tool call becomes one HTTP request via the generated API client
// (robottt/internal/api/gen), no hardware or queue logic lives here.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apigen "robottt/internal/api/gen"
)

// Server wraps a robot API client and registers MCP tools against it.
type Server struct {
	client *apigen.ClientWithResponses
}

// New builds a Server calling the robot API at baseURL (e.g.
// "http://localhost:8080").
func New(baseURL string) (*Server, error) {
	client, err := apigen.NewClientWithResponses(baseURL)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: create API client for %s: %w", baseURL, err)
	}
	return &Server{client: client}, nil
}

// LedInput is the set_led tool's input schema.
type LedInput struct {
	On bool `json:"on" jsonschema:"whether to turn the LED on or off"`
}

// StepperInput is the move_stepper tool's input schema.
type StepperInput struct {
	Steps int    `json:"steps" jsonschema:"number of steps to move, must be positive"`
	Dir   string `json:"dir" jsonschema:"rotation direction, cw or ccw"`
}

// ServoInput is the set_servo tool's input schema.
type ServoInput struct {
	AngleDeg float64 `json:"angle_deg" jsonschema:"target servo angle in degrees"`
}

// RegisterTools adds all robot control tools to server.
func (s *Server) RegisterTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_led",
		Description: "Turn the robot's LED on or off",
	}, s.handleSetLED)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "move_stepper",
		Description: "Move the robot's stepper motor a number of steps in a direction",
	}, s.handleMoveStepper)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_servo",
		Description: "Set the robot's servo to an absolute angle in degrees",
	}, s.handleSetServo)
}

func (s *Server) handleSetLED(ctx context.Context, _ *mcp.CallToolRequest, in LedInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.PostLedWithResponse(ctx, apigen.PostLedJSONRequestBody{On: in.On})
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: call /led: %w", err)
	}
	return httpResult(resp.StatusCode(), resp.Body), nil, nil
}

func (s *Server) handleMoveStepper(ctx context.Context, _ *mcp.CallToolRequest, in StepperInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.PostStepperWithResponse(ctx, apigen.PostStepperJSONRequestBody{
		Steps: in.Steps,
		Dir:   apigen.StepperRequestDir(in.Dir),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: call /stepper: %w", err)
	}
	return httpResult(resp.StatusCode(), resp.Body), nil, nil
}

func (s *Server) handleSetServo(ctx context.Context, _ *mcp.CallToolRequest, in ServoInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.PostServoWithResponse(ctx, apigen.PostServoJSONRequestBody{AngleDeg: in.AngleDeg})
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: call /servo: %w", err)
	}
	return httpResult(resp.StatusCode(), resp.Body), nil, nil
}

// httpResult renders an HTTP response as an MCP tool result, flagging
// non-2xx as an error so the LLM sees why a command failed (e.g. 503 queue
// full) rather than just getting silent text back.
func httpResult(status int, body []byte) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("HTTP %d: %s", status, string(body))}},
		IsError: status >= 400,
	}
}
