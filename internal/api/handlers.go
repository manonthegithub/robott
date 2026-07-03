// Package api is the HTTP layer: it validates requests, builds commands,
// and enqueues them. It never touches hardware directly.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"robottt/internal/command"
)

// maxBodyBytes bounds request body size; these are small fixed-shape JSON
// bodies, anything larger is malformed or abusive.
const maxBodyBytes = 1024

// Handlers holds the dependencies HTTP handlers need: the command queue and
// the servo's configured angle range (used for request validation).
type Handlers struct {
	Queue         command.CommandQueue
	ServoMinAngle float64
	ServoMaxAngle float64
}

func (h *Handlers) HandleLED(w http.ResponseWriter, r *http.Request) {
	var req ledRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.enqueueOrRespond(w, command.LEDCommand{On: req.On})
}

func (h *Handlers) HandleStepper(w http.ResponseWriter, r *http.Request) {
	var req stepperRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Steps <= 0 {
		writeError(w, http.StatusBadRequest, "steps must be a positive integer")
		return
	}
	dir := command.Direction(req.Dir)
	if dir != command.DirCW && dir != command.DirCCW {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("dir must be %q or %q", command.DirCW, command.DirCCW))
		return
	}
	h.enqueueOrRespond(w, command.StepperCommand{Steps: req.Steps, Dir: dir})
}

func (h *Handlers) HandleServo(w http.ResponseWriter, r *http.Request) {
	var req servoRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.AngleDeg < h.ServoMinAngle || req.AngleDeg > h.ServoMaxAngle {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("angle_deg must be between %.2f and %.2f", h.ServoMinAngle, h.ServoMaxAngle))
		return
	}
	h.enqueueOrRespond(w, command.ServoCommand{AngleDeg: req.AngleDeg})
}

func (h *Handlers) enqueueOrRespond(w http.ResponseWriter, cmd command.Command) {
	if err := h.Queue.Enqueue(cmd); err != nil {
		if errors.Is(err, command.ErrQueueFull) {
			writeError(w, http.StatusServiceUnavailable, "queue full, retry")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, queuedResponse{Status: "queued"})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
