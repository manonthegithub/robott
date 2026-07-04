// Package mcpserver exposes the robot API as MCP tools, so an MCP-capable
// LLM client can drive the robot. It runs in the same process as the REST
// API (mounted at /mcp by cmd/robottt), calling api.Handlers directly - no
// network hop to itself.
package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"robottt/internal/api"
	apigen "robottt/internal/api/gen"
)

// Server wraps the robot API's handlers and registers MCP tools against
// them.
type Server struct {
	handlers *api.Handlers
}

// New builds a Server calling straight into handlers.
func New(handlers *api.Handlers) *Server {
	return &Server{handlers: handlers}
}

// HTTPHandler builds the MCP server and returns it as an http.Handler,
// ready to be mounted on a path (e.g. "/mcp") alongside the REST routes.
func (s *Server) HTTPHandler() http.Handler {
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "robottt", Version: "1.0.0"}, nil)
	s.registerTools(mcpServer)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
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

func (s *Server) registerTools(server *mcp.Server) {
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
	resp, err := s.handlers.PostLed(ctx, apigen.PostLedRequestObject{Body: &apigen.LedOperation{On: in.On}})
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: PostLed: %w", err)
	}
	return renderResult(resp.VisitPostLedResponse)
}

func (s *Server) handleMoveStepper(ctx context.Context, _ *mcp.CallToolRequest, in StepperInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.handlers.PostStepper(ctx, apigen.PostStepperRequestObject{Body: &apigen.StepperOperation{
		Steps: in.Steps,
		Dir:   apigen.StepperOperationDir(in.Dir),
	}})
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: PostStepper: %w", err)
	}
	return renderResult(resp.VisitPostStepperResponse)
}

func (s *Server) handleSetServo(ctx context.Context, _ *mcp.CallToolRequest, in ServoInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.handlers.PostServo(ctx, apigen.PostServoRequestObject{Body: &apigen.ServoOperation{AngleDeg: in.AngleDeg}})
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: PostServo: %w", err)
	}
	return renderResult(resp.VisitPostServoResponse)
}

// renderResult runs a generated response object's Visit method against a
// recorder to get its HTTP status/body without an actual network
// round-trip, then maps that into an MCP tool result.
func renderResult(visit func(w http.ResponseWriter) error) (*mcp.CallToolResult, any, error) {
	rec := httptest.NewRecorder()
	if err := visit(rec); err != nil {
		return nil, nil, fmt.Errorf("mcpserver: render response: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("HTTP %d: %s", rec.Code, rec.Body.String())}},
		IsError: rec.Code >= 400,
	}, nil, nil
}
