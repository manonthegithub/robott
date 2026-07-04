// Package api is the HTTP layer: it validates requests, builds commands,
// and enqueues them. It never touches hardware directly. Request/response
// shapes come from ../../openapi.yaml via generated code in ./gen.
package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	apigen "robottt/internal/api/gen"
	"robottt/internal/command"
	"robottt/internal/sequence"
)

// Handlers holds the dependencies HTTP handlers need: the command queue, the
// servo's configured angle range (used for request validation), the
// Sequencer for /sequence, and a server-lifetime Ctx used for goroutines
// spawned by a delay_ms>0 single-shot request — those outlive the HTTP
// request that started them, so they're scoped to server shutdown, not the
// request's own (already-finished) context.
type Handlers struct {
	Queue         command.CommandQueue
	ServoMinAngle float64
	ServoMaxAngle float64
	Sequencer     *sequence.Sequencer
	Ctx           context.Context
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

// intOrZero dereferences an optional *int field (delay_ms, times), treating
// an absent/omitted value the same as an explicit 0 — an LLM-generated
// request is as likely to omit an optional field as send it explicitly.
func intOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// delayedResponse spawns a goroutine that waits delayMs then enqueues cmd,
// scoped to h.Ctx (server lifetime, not the HTTP request's own context,
// which is already gone by the time the delay elapses). Used by the 3
// single-shot handlers when delay_ms>0: the 202 response is sent before the
// command actually reaches the queue.
func (h *Handlers) delayedResponse(cmd command.Command, delayMs int) {
	go func() {
		delay := time.Duration(delayMs) * time.Millisecond
		if err := command.DelayedEnqueue(h.Ctx, h.Queue, delay, cmd); err != nil {
			log.Printf("api: delayed enqueue of %T failed: %v", cmd, err)
		}
	}()
}

func (h *Handlers) PostLed(_ context.Context, request apigen.PostLedRequestObject) (apigen.PostLedResponseObject, error) {
	cmd := command.LEDCommand{On: request.Body.On}
	if delayMs := intOrZero(request.Body.DelayMs); delayMs > 0 {
		h.delayedResponse(cmd, delayMs)
		return apigen.PostLed202JSONResponse{QueuedJSONResponse: apigen.QueuedJSONResponse{Status: "queued"}}, nil
	}

	return enqueueAndRespond[apigen.PostLedResponseObject](h, cmd,
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

	cmd := command.StepperCommand{Steps: request.Body.Steps, Dir: dir}
	if delayMs := intOrZero(request.Body.DelayMs); delayMs > 0 {
		h.delayedResponse(cmd, delayMs)
		return apigen.PostStepper202JSONResponse{QueuedJSONResponse: apigen.QueuedJSONResponse{Status: "queued"}}, nil
	}

	return enqueueAndRespond[apigen.PostStepperResponseObject](h, cmd,
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

	cmd := command.ServoCommand{AngleDeg: deg}
	if delayMs := intOrZero(request.Body.DelayMs); delayMs > 0 {
		h.delayedResponse(cmd, delayMs)
		return apigen.PostServo202JSONResponse{QueuedJSONResponse: apigen.QueuedJSONResponse{Status: "queued"}}, nil
	}

	return enqueueAndRespond[apigen.PostServoResponseObject](h, cmd,
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

func (h *Handlers) PostSequence(_ context.Context, request apigen.PostSequenceRequestObject) (apigen.PostSequenceResponseObject, error) {
	ops := make([]sequence.Operation, 0, len(request.Body.Seq))
	for _, step := range request.Body.Seq {
		op, err := h.toOperation(step)
		if err != nil {
			return apigen.PostSequence400JSONResponse{BadRequestJSONResponse: badRequestResponse(err.Error())}, nil
		}
		ops = append(ops, op)
	}

	err := h.Sequencer.Start(sequence.OperationSequence{Seq: ops})
	switch {
	case err == nil:
		return apigen.PostSequence202JSONResponse{QueuedJSONResponse: apigen.QueuedJSONResponse{Status: "queued"}}, nil
	case errors.Is(err, sequence.ErrAlreadyRunning):
		return apigen.PostSequence409JSONResponse{ConflictJSONResponse: apigen.ConflictJSONResponse{Error: err.Error()}}, nil
	default:
		// Only validation errors reach here (ErrAlreadyRunning is the only
		// non-validation error Start returns) — a structurally-invalid tree
		// that passed toOperation's own checks but fails sequence.validate
		// (e.g. depth, negative times) is still a 400, not a 500.
		return apigen.PostSequence400JSONResponse{BadRequestJSONResponse: badRequestResponse(err.Error())}, nil
	}
}

func (h *Handlers) PostSequenceStop(_ context.Context, _ apigen.PostSequenceStopRequestObject) (apigen.PostSequenceStopResponseObject, error) {
	err := h.Sequencer.Stop()
	switch {
	case err == nil:
		return apigen.PostSequenceStop202JSONResponse{StoppedJSONResponse: apigen.StoppedJSONResponse{Status: "stopped"}}, nil
	case errors.Is(err, sequence.ErrNotRunning):
		return apigen.PostSequenceStop404JSONResponse{NotFoundJSONResponse: apigen.NotFoundJSONResponse{Error: err.Error()}}, nil
	default:
		return apigen.PostSequenceStop500JSONResponse{InternalErrorJSONResponse: apigen.InternalErrorJSONResponse{Error: err.Error()}}, nil
	}
}
