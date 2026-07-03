// Package hardware defines the controller interfaces the executor drives.
// Concrete implementations (e.g. gpiodirect) live in sub-packages and are
// swapped at wiring time in main.go without touching the executor or API
// layers.
package hardware

import (
	"errors"

	"robottt/internal/command"
)

// ErrOutOfRange is returned when a command value falls outside the
// hardware's configured operating range (e.g. servo angle).
var ErrOutOfRange = errors.New("value out of range")

// GPIOController drives a simple on/off digital output (the LED).
type GPIOController interface {
	SetLED(on bool) error
	Close() error
}

// StepperController drives a STEP/DIR stepper motor driver chip.
type StepperController interface {
	// Move blocks until the requested number of steps have been issued.
	Move(steps int, dir command.Direction) error
	Close() error
}

// ServoController drives a PWM servo motor.
type ServoController interface {
	// SetAngle sets the servo to an absolute angle in degrees.
	// Returns ErrOutOfRange if deg is outside the configured range.
	SetAngle(deg float64) error
	Close() error
}
