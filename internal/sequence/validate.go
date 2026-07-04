package sequence

import (
	"errors"
	"fmt"
)

// MaxDepth bounds how deeply Loops may nest. Needed because Times==0 (an
// infinite loop) can't be bounded by expanding it, so pathological nesting
// has to be rejected structurally instead.
const MaxDepth = 5

var (
	// ErrEmptyLoopBody is returned when a Loop's Body is empty — an
	// infinite (or even finite) loop over nothing would either spin forever
	// doing no work or be a silent no-op, neither of which is a sequence
	// worth accepting.
	ErrEmptyLoopBody = errors.New("sequence: loop body must not be empty")
	// ErrNegativeTimes is returned when a Loop's Times is negative (0 is
	// valid and means infinite; negative has no meaning).
	ErrNegativeTimes = errors.New("sequence: loop times must be >= 0")
	// ErrNegativeDelay is returned when a HardwareCommand's delay is negative.
	ErrNegativeDelay = errors.New("sequence: delay_ms must be >= 0")
	// ErrInvalidDir is returned when a StepperCommand's Dir isn't "cw"/"ccw".
	ErrInvalidDir = errors.New(`sequence: dir must be "cw" or "ccw"`)
	// ErrTooDeep is returned when a sequence nests Loops beyond MaxDepth.
	ErrTooDeep = fmt.Errorf("sequence: nesting exceeds max depth %d", MaxDepth)
	// ErrUnknownOperation is returned for any Operation value that isn't a
	// Loop, a Par, or a recognized HardwareCommand.
	ErrUnknownOperation = errors.New("sequence: unrecognized operation type")
	// ErrEmptyPar is returned when a Par has no branches — nothing to run
	// concurrently, not a meaningful sequence step.
	ErrEmptyPar = errors.New("sequence: par must have at least one branch")
	// ErrEmptyParBranch is returned when one of a Par's branches is empty.
	ErrEmptyParBranch = errors.New("sequence: par branch must not be empty")
)

// validate walks ops (and everything nested inside any Loop) checking every
// structural invariant that doesn't depend on runtime config (servo angle
// range, which needs config, is checked by the caller before an Operation is
// even constructed — see internal/api's mapper).
func validate(ops []Operation, depth int) error {
	if depth > MaxDepth {
		return ErrTooDeep
	}
	for _, op := range ops {
		switch o := op.(type) {
		case Loop:
			if o.Times < 0 {
				return ErrNegativeTimes
			}
			if len(o.Body) == 0 {
				return ErrEmptyLoopBody
			}
			if err := validate(o.Body, depth+1); err != nil {
				return err
			}
		case Par:
			if len(o.Branches) == 0 {
				return ErrEmptyPar
			}
			for _, branch := range o.Branches {
				if len(branch) == 0 {
					return ErrEmptyParBranch
				}
				if err := validate(branch, depth+1); err != nil {
					return err
				}
			}
		case LedCommand:
			if err := validateDelay(o); err != nil {
				return err
			}
		case ServoCommand:
			if err := validateDelay(o); err != nil {
				return err
			}
		case StepperCommand:
			if err := validateDelay(o); err != nil {
				return err
			}
			if o.Dir != "cw" && o.Dir != "ccw" {
				return ErrInvalidDir
			}
		default:
			return ErrUnknownOperation
		}
	}
	return nil
}

func validateDelay(c HardwareCommand) error {
	if c.Delay() < 0 {
		return ErrNegativeDelay
	}
	return nil
}
