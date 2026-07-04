package api

import (
	"fmt"

	apigen "robottt/internal/api/gen"
	"robottt/internal/command"
	"robottt/internal/sequence"
)

// toOperation recursively maps a generated Operation DTO (a oneOf/
// discriminator union over LedOperation/ServoOperation/StepperOperation/
// LoopOperation) into the sequence.Operation/Loop hierarchy. It applies the
// same per-field validation the standalone /led, /stepper, /servo handlers
// apply inline, so a command nested inside a sequence is checked exactly as
// strictly as a standalone request against the same endpoint. Structural
// checks that don't need config (negative delay/times, bad nesting depth,
// empty loop body) are left to sequence.validate, called by Sequencer.Start.
func (h *Handlers) toOperation(op apigen.Operation) (sequence.Operation, error) {
	disc, err := op.Discriminator()
	if err != nil {
		return nil, fmt.Errorf("sequence: %w", err)
	}

	switch disc {
	case "led":
		led, err := op.AsLedOperation()
		if err != nil {
			return nil, fmt.Errorf("sequence: %w", err)
		}
		return sequence.NewLedCommand(led.On, intOrZero(led.DelayMs)), nil

	case "servo":
		servo, err := op.AsServoOperation()
		if err != nil {
			return nil, fmt.Errorf("sequence: %w", err)
		}
		if servo.AngleDeg < h.ServoMinAngle || servo.AngleDeg > h.ServoMaxAngle {
			return nil, fmt.Errorf("sequence: angle_deg must be between %.2f and %.2f", h.ServoMinAngle, h.ServoMaxAngle)
		}
		return sequence.NewServoCommand(servo.AngleDeg, intOrZero(servo.DelayMs)), nil

	case "stepper":
		st, err := op.AsStepperOperation()
		if err != nil {
			return nil, fmt.Errorf("sequence: %w", err)
		}
		if st.Steps <= 0 {
			return nil, fmt.Errorf("sequence: steps must be a positive integer")
		}
		dir := command.Direction(st.Dir)
		if dir != command.DirCW && dir != command.DirCCW {
			return nil, fmt.Errorf("sequence: dir must be %q or %q", command.DirCW, command.DirCCW)
		}
		return sequence.NewStepperCommand(st.Steps, string(dir), intOrZero(st.DelayMs)), nil

	case "loop":
		loop, err := op.AsLoopOperation()
		if err != nil {
			return nil, fmt.Errorf("sequence: %w", err)
		}
		body := make([]sequence.Operation, 0, len(loop.Body))
		for _, nested := range loop.Body {
			nestedOp, err := h.toOperation(nested)
			if err != nil {
				return nil, err
			}
			body = append(body, nestedOp)
		}
		return sequence.Loop{Times: intOrZero(loop.Times), Body: body}, nil

	default:
		return nil, fmt.Errorf("sequence: unknown type %q", disc)
	}
}
