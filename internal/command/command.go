// Package command defines the hardware command types that flow from the
// HTTP layer to the executor via a CommandQueue.
package command

// Direction is the rotation direction for the stepper motor.
type Direction string

const (
	DirCW  Direction = "cw"
	DirCCW Direction = "ccw"
)

// Command is a marker interface implemented by every hardware command type.
type Command interface {
	isCommand()
}

// LEDCommand turns the LED on or off.
type LEDCommand struct {
	On bool
}

func (LEDCommand) isCommand() {}

// StepperCommand moves the stepper motor a number of steps in a direction.
type StepperCommand struct {
	Steps int
	Dir   Direction
}

func (StepperCommand) isCommand() {}

// ServoCommand sets the servo to an absolute angle in degrees.
type ServoCommand struct {
	AngleDeg float64
}

func (ServoCommand) isCommand() {}
