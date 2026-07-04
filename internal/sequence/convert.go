package sequence

import (
	"fmt"

	"robottt/internal/command"
)

// toCommand converts a HardwareCommand operation into the executor-facing
// command.Command. This is the only file in the package that imports
// internal/command, keeping the operation tree itself independent of it.
//
// op is assumed to have already passed validate — an unrecognized concrete
// type here means a construction bug elsewhere in this codebase (e.g. a new
// HardwareCommand implementation added without updating this switch), not
// something a caller's input can trigger, hence the panic rather than an
// error return.
func toCommand(op HardwareCommand) command.Command {
	switch c := op.(type) {
	case LedCommand:
		return command.LEDCommand{On: c.On}
	case ServoCommand:
		return command.ServoCommand{AngleDeg: c.AngleDeg}
	case StepperCommand:
		return command.StepperCommand{Steps: c.Steps, Dir: command.Direction(c.Dir)}
	default:
		panic(fmt.Sprintf("sequence: unconvertible HardwareCommand %T", op))
	}
}
