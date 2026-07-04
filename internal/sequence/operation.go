// Package sequence describes a tree of hardware operations (a "sequence")
// that can be started/stopped as a unit. Its types are independent of
// internal/command — no import of it in this file — so the operation tree
// can be built, validated, and tested without pulling in hardware-dispatch
// types. internal/sequence/convert.go is the only file that bridges the two.
package sequence

import "time"

// Operation is any node in a sequence tree: a HardwareCommand or a Loop.
type Operation any

// HardwareCommand is an Operation that dispatches to hardware, carrying the
// delay to wait before it's enqueued.
type HardwareCommand interface {
	Delay() time.Duration
}

type baseHardwareCommand struct {
	DelayMs int
}

func (b baseHardwareCommand) Delay() time.Duration {
	return time.Duration(b.DelayMs) * time.Millisecond
}

// LedCommand turns the LED on or off.
type LedCommand struct {
	baseHardwareCommand
	On bool
}

// ServoCommand sets the servo to an absolute angle in degrees.
type ServoCommand struct {
	baseHardwareCommand
	AngleDeg float64
}

// StepperCommand moves the stepper motor a number of steps in a direction
// ("cw"/"ccw"). Dir is a plain string, not command.Direction, to keep this
// package independent of internal/command.
type StepperCommand struct {
	baseHardwareCommand
	Steps int
	Dir   string
}

// NewLedCommand, NewServoCommand, NewStepperCommand construct the
// corresponding HardwareCommand with the given delay in milliseconds.
func NewLedCommand(on bool, delayMs int) LedCommand {
	return LedCommand{baseHardwareCommand: baseHardwareCommand{DelayMs: delayMs}, On: on}
}

func NewServoCommand(angleDeg float64, delayMs int) ServoCommand {
	return ServoCommand{baseHardwareCommand: baseHardwareCommand{DelayMs: delayMs}, AngleDeg: angleDeg}
}

func NewStepperCommand(steps int, dir string, delayMs int) StepperCommand {
	return StepperCommand{baseHardwareCommand: baseHardwareCommand{DelayMs: delayMs}, Steps: steps, Dir: dir}
}

// Loop repeats Body Times times. Times == 0 means infinite (run until the
// sequence is stopped). Body must be non-empty — an infinite loop over
// nothing would spin forever doing no work, so it's rejected by validate,
// not allowed to construct silently.
type Loop struct {
	Body  []Operation
	Times int
}

// Par runs each of Branches concurrently (one goroutine per branch, each
// walking its branch the same way exec walks any []Operation), then waits
// for all of them to finish before the sequence continues past this node.
// Branches still funnel into the same shared CommandQueue/single serial
// executor, so this doesn't make hardware dispatch itself simultaneous —
// it makes each branch's own delay/pacing independent of the others, which
// is what makes e.g. a blink pattern and a servo sweep look concurrent
// instead of strictly one-after-the-other.
type Par struct {
	Branches [][]Operation
}

// OperationSequence is the top-level operation list a Sequencer runs.
type OperationSequence struct {
	Seq []Operation
}
