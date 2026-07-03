// Package gpiodirect is the v1 hardware.GPIOController / StepperController /
// ServoController implementation: it drives GPIO and PWM directly on the
// Pi 5 itself (RP1 chardev GPIO + sysfs PWM). A future MCU co-processor
// implementation would satisfy the same interfaces from internal/hardware
// without changing any other package.
package gpiodirect

import (
	"fmt"

	"github.com/warthog618/go-gpiocdev"

	"robottt/internal/hardware"
)

// gpioLine is the subset of *gpiocdev.Line's API this package depends on.
// Narrowing to an interface lets tests inject a fake line instead of
// requiring a real GPIO chip.
type gpioLine interface {
	SetValue(value int) error
	Close() error
}

// GPIO drives a single digital output line (the LED).
type GPIO struct {
	line gpioLine
}

// NewGPIO requests offset as an output line on chip (e.g. "gpiochip4" on
// Pi 5's RP1), initially low.
func NewGPIO(chip string, offset int) (*GPIO, error) {
	line, err := gpiocdev.RequestLine(chip, offset, gpiocdev.AsOutput(0))
	if err != nil {
		return nil, fmt.Errorf("gpiodirect: request LED line chip=%s offset=%d: %w", chip, offset, err)
	}
	return &GPIO{line: line}, nil
}

func (g *GPIO) SetLED(on bool) error {
	v := 0
	if on {
		v = 1
	}
	if err := g.line.SetValue(v); err != nil {
		return fmt.Errorf("gpiodirect: set LED value: %w", err)
	}
	return nil
}

func (g *GPIO) Close() error {
	return g.line.Close()
}

var _ hardware.GPIOController = (*GPIO)(nil)
