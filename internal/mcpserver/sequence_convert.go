package mcpserver

import (
	"encoding/json"
	"fmt"

	apigen "robottt/internal/api/gen"
)

// SequenceStepInput is one node of a run_sequence tool call: a plain,
// flat struct (MCP's schema reflection can't describe a discriminated
// union the way openapi.yaml's Operation oneOf does), later converted into
// the generated apigen.Operation union so it goes through exactly the same
// PostSequence/toOperation validation a REST /sequence call would.
//
// Body is []any, not []SequenceStepInput: the MCP SDK's schema reflector
// walks a tool input type to build its JSON schema and panics ("cycle
// detected") on a type that transitively contains itself, so the struct
// can't directly recurse. Recursion is instead handled at runtime in
// toGenOperation, which re-decodes each body element via decodeStep.
type SequenceStepInput struct {
	Type     string  `json:"type" jsonschema:"led, servo, stepper, or loop"`
	On       bool    `json:"on,omitempty" jsonschema:"led only: whether to turn the LED on or off"`
	AngleDeg float64 `json:"angle_deg,omitempty" jsonschema:"servo only: target angle in degrees"`
	Steps    int     `json:"steps,omitempty" jsonschema:"stepper only: number of steps, must be positive"`
	Dir      string  `json:"dir,omitempty" jsonschema:"stepper only: cw or ccw"`
	DelayMs  int     `json:"delay_ms,omitempty" jsonschema:"led/servo/stepper only: milliseconds to wait before this step runs"`
	Times    int     `json:"times,omitempty" jsonschema:"loop only: number of repetitions; 0 or omitted means infinite (until stop_sequence)"`
	Body     []any   `json:"body,omitempty" jsonschema:"loop only: nested steps (same shape as a top-level seq entry) to repeat"`
	Branches [][]any `json:"branches,omitempty" jsonschema:"par only: each branch is a list of steps (same shape as a top-level seq entry) run concurrently with the others"`
}

// decodeStep re-decodes one Body element (already JSON-decoded generically
// as part of the surrounding SequenceStepInput, so a map[string]any in
// practice) back into a concrete SequenceStepInput.
func decodeStep(raw any) (SequenceStepInput, error) {
	var step SequenceStepInput
	b, err := json.Marshal(raw)
	if err != nil {
		return step, fmt.Errorf("mcpserver: re-encode nested step: %w", err)
	}
	if err := json.Unmarshal(b, &step); err != nil {
		return step, fmt.Errorf("mcpserver: decode nested step: %w", err)
	}
	return step, nil
}

// RunSequenceInput is the run_sequence tool's input schema.
type RunSequenceInput struct {
	Seq []SequenceStepInput `json:"seq" jsonschema:"ordered list of steps and loops to run"`
}

// StopSequenceInput is the stop_sequence tool's input schema (no fields:
// there is at most one sequence running, nothing to identify).
type StopSequenceInput struct{}

// toGenOperation converts one SequenceStepInput (and, for a loop, its
// nested Body) into the generated apigen.Operation union via its
// From<Variant>Operation setters.
func toGenOperation(in SequenceStepInput) (apigen.Operation, error) {
	var op apigen.Operation

	switch in.Type {
	case "led":
		if err := op.FromLedOperation(apigen.LedOperation{
			Type:    "led",
			On:      in.On,
			DelayMs: &in.DelayMs,
		}); err != nil {
			return op, fmt.Errorf("mcpserver: build led operation: %w", err)
		}

	case "servo":
		if err := op.FromServoOperation(apigen.ServoOperation{
			Type:     "servo",
			AngleDeg: in.AngleDeg,
			DelayMs:  &in.DelayMs,
		}); err != nil {
			return op, fmt.Errorf("mcpserver: build servo operation: %w", err)
		}

	case "stepper":
		if err := op.FromStepperOperation(apigen.StepperOperation{
			Type:    "stepper",
			Steps:   in.Steps,
			Dir:     apigen.StepperOperationDir(in.Dir),
			DelayMs: &in.DelayMs,
		}); err != nil {
			return op, fmt.Errorf("mcpserver: build stepper operation: %w", err)
		}

	case "loop":
		body := make([]apigen.Operation, 0, len(in.Body))
		for _, raw := range in.Body {
			nested, err := decodeStep(raw)
			if err != nil {
				return op, err
			}
			nestedOp, err := toGenOperation(nested)
			if err != nil {
				return op, err
			}
			body = append(body, nestedOp)
		}
		times := in.Times
		if err := op.FromLoopOperation(apigen.LoopOperation{
			Type:  "loop",
			Times: &times,
			Body:  body,
		}); err != nil {
			return op, fmt.Errorf("mcpserver: build loop operation: %w", err)
		}

	case "par":
		branches := make([][]apigen.Operation, 0, len(in.Branches))
		for _, rawBranch := range in.Branches {
			branch := make([]apigen.Operation, 0, len(rawBranch))
			for _, raw := range rawBranch {
				nested, err := decodeStep(raw)
				if err != nil {
					return op, err
				}
				nestedOp, err := toGenOperation(nested)
				if err != nil {
					return op, err
				}
				branch = append(branch, nestedOp)
			}
			branches = append(branches, branch)
		}
		if err := op.FromParOperation(apigen.ParOperation{
			Type:     "par",
			Branches: branches,
		}); err != nil {
			return op, fmt.Errorf("mcpserver: build par operation: %w", err)
		}

	default:
		return op, fmt.Errorf("mcpserver: unknown step type %q", in.Type)
	}

	return op, nil
}
