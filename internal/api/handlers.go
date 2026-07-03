// Package api is the HTTP layer: it validates requests, builds commands,
// and enqueues them. It never touches hardware directly. Request/response
// shapes come from ../../openapi.yaml via generated code in ./gen.
package api

import (
	"context"
	"errors"
	"fmt"

	apigen "robottt/internal/api/gen"
	"robottt/internal/command"
)

// Handlers holds the dependencies HTTP handlers need: the command queue and
// the servo's configured angle range (used for request validation).
type Handlers struct {
	Queue         command.CommandQueue
	ServoMinAngle float64
	ServoMaxAngle float64
}

var _ apigen.StrictServerInterface = (*Handlers)(nil)

// enqueueAndRespond enqueues cmd and maps the outcome to one of the 3
// per-operation response types generated for a given endpoint. Generated
// response types are structurally identical (Status/Error string) but
// distinct per operation, so this stays generic over the response type
// rather than duplicated 3 times with only type names differing.
func enqueueAndRespond[R any](
	h *Handlers,
	cmd command.Command,
	onQueued func(status string) R,
	onQueueFull func(msg string) R,
	onError func(msg string) R,
) (R, error) {
	if err := h.Queue.Enqueue(cmd); err != nil {
		if errors.Is(err, command.ErrQueueFull) {
			return onQueueFull("queue full, retry"), nil
		}
		return onError("internal error"), nil
	}
	return onQueued("queued"), nil
}

// Shared components.responses in openapi.yaml get hoisted by oapi-codegen
// into named types (QueuedJSONResponse, BadRequestJSONResponse, etc.), and
// each operation's response type embeds one of those anonymously - so
// building e.g. PostLed202JSONResponse means constructing the embedded
// QueuedJSONResponse field, not a top-level Status field directly.

func badRequestResponse(msg string) apigen.BadRequestJSONResponse {
	return apigen.BadRequestJSONResponse{Error: msg}
}

func (h *Handlers) PostLed(_ context.Context, request apigen.PostLedRequestObject) (apigen.PostLedResponseObject, error) {
	return enqueueAndRespond[apigen.PostLedResponseObject](h, command.LEDCommand{On: request.Body.On},
		func(s string) apigen.PostLedResponseObject {
			return apigen.PostLed202JSONResponse{QueuedJSONResponse: apigen.QueuedJSONResponse{Status: s}}
		},
		func(m string) apigen.PostLedResponseObject {
			return apigen.PostLed503JSONResponse{QueueFullJSONResponse: apigen.QueueFullJSONResponse{Error: m}}
		},
		func(m string) apigen.PostLedResponseObject {
			return apigen.PostLed500JSONResponse{InternalErrorJSONResponse: apigen.InternalErrorJSONResponse{Error: m}}
		},
	)
}

func (h *Handlers) PostStepper(_ context.Context, request apigen.PostStepperRequestObject) (apigen.PostStepperResponseObject, error) {
	if request.Body.Steps <= 0 {
		return apigen.PostStepper400JSONResponse{BadRequestJSONResponse: badRequestResponse("steps must be a positive integer")}, nil
	}
	dir := command.Direction(request.Body.Dir)
	if dir != command.DirCW && dir != command.DirCCW {
		return apigen.PostStepper400JSONResponse{BadRequestJSONResponse: badRequestResponse(fmt.Sprintf("dir must be %q or %q", command.DirCW, command.DirCCW))}, nil
	}

	return enqueueAndRespond[apigen.PostStepperResponseObject](h, command.StepperCommand{Steps: request.Body.Steps, Dir: dir},
		func(s string) apigen.PostStepperResponseObject {
			return apigen.PostStepper202JSONResponse{QueuedJSONResponse: apigen.QueuedJSONResponse{Status: s}}
		},
		func(m string) apigen.PostStepperResponseObject {
			return apigen.PostStepper503JSONResponse{QueueFullJSONResponse: apigen.QueueFullJSONResponse{Error: m}}
		},
		func(m string) apigen.PostStepperResponseObject {
			return apigen.PostStepper500JSONResponse{InternalErrorJSONResponse: apigen.InternalErrorJSONResponse{Error: m}}
		},
	)
}

func (h *Handlers) PostServo(_ context.Context, request apigen.PostServoRequestObject) (apigen.PostServoResponseObject, error) {
	deg := request.Body.AngleDeg
	if deg < h.ServoMinAngle || deg > h.ServoMaxAngle {
		return apigen.PostServo400JSONResponse{BadRequestJSONResponse: badRequestResponse(fmt.Sprintf("angle_deg must be between %.2f and %.2f", h.ServoMinAngle, h.ServoMaxAngle))}, nil
	}

	return enqueueAndRespond[apigen.PostServoResponseObject](h, command.ServoCommand{AngleDeg: deg},
		func(s string) apigen.PostServoResponseObject {
			return apigen.PostServo202JSONResponse{QueuedJSONResponse: apigen.QueuedJSONResponse{Status: s}}
		},
		func(m string) apigen.PostServoResponseObject {
			return apigen.PostServo503JSONResponse{QueueFullJSONResponse: apigen.QueueFullJSONResponse{Error: m}}
		},
		func(m string) apigen.PostServoResponseObject {
			return apigen.PostServo500JSONResponse{InternalErrorJSONResponse: apigen.InternalErrorJSONResponse{Error: m}}
		},
	)
}
