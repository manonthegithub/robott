package gpiodirect

import (
	"fmt"
	"time"

	"github.com/warthog618/go-gpiocdev"

	"robottt/internal/command"
	"robottt/internal/hardware"
)

// stepperDirValue maps a command.Direction to the DIR line's digital value.
// Wiring-dependent, but consistent within this package.
var stepperDirValue = map[command.Direction]int{
	command.DirCW:  1,
	command.DirCCW: 0,
}

// Stepper drives a STEP/DIR stepper motor driver chip (A4988/DRV8825/
// TMC2209-class) by toggling the STEP line in software. See
// docs/architecture.md decision #1 for why this is software-toggled rather
// than HW PWM: exact step count matters more than pulse timing precision.
type Stepper struct {
	stepLine   gpioLine
	dirLine    gpioLine
	pulseDelay time.Duration // time high/low per half-pulse; sets max step rate
}

// NewStepper requests stepOffset/dirOffset as output lines on chip.
// pulseDelay is the delay held after each STEP transition (high and low),
// so one step takes 2*pulseDelay.
func NewStepper(chip string, stepOffset, dirOffset int, pulseDelay time.Duration) (*Stepper, error) {
	stepLine, err := gpiocdev.RequestLine(chip, stepOffset, gpiocdev.AsOutput(0))
	if err != nil {
		return nil, fmt.Errorf("gpiodirect: request STEP line chip=%s offset=%d: %w", chip, stepOffset, err)
	}
	dirLine, err := gpiocdev.RequestLine(chip, dirOffset, gpiocdev.AsOutput(0))
	if err != nil {
		stepLine.Close()
		return nil, fmt.Errorf("gpiodirect: request DIR line chip=%s offset=%d: %w", chip, dirOffset, err)
	}
	return &Stepper{stepLine: stepLine, dirLine: dirLine, pulseDelay: pulseDelay}, nil
}

func (s *Stepper) Move(steps int, dir command.Direction) error {
	if steps < 0 {
		return fmt.Errorf("gpiodirect: steps must be non-negative, got %d", steps)
	}
	dirVal, ok := stepperDirValue[dir]
	if !ok {
		return fmt.Errorf("gpiodirect: unknown direction %q", dir)
	}
	if err := s.dirLine.SetValue(dirVal); err != nil {
		return fmt.Errorf("gpiodirect: set DIR line: %w", err)
	}

	for i := 0; i < steps; i++ {
		if err := s.stepLine.SetValue(1); err != nil {
			return fmt.Errorf("gpiodirect: set STEP line high (step %d/%d): %w", i+1, steps, err)
		}
		time.Sleep(s.pulseDelay)
		if err := s.stepLine.SetValue(0); err != nil {
			return fmt.Errorf("gpiodirect: set STEP line low (step %d/%d): %w", i+1, steps, err)
		}
		time.Sleep(s.pulseDelay)
	}
	return nil
}

func (s *Stepper) Close() error {
	stepErr := s.stepLine.Close()
	dirErr := s.dirLine.Close()
	if stepErr != nil {
		return stepErr
	}
	return dirErr
}

var _ hardware.StepperController = (*Stepper)(nil)
