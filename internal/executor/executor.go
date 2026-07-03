// Package executor drains the CommandQueue and dispatches each command to
// the matching hardware controller, one at a time. Serial dispatch is the
// hardware mutex: see docs/architecture.md decision #2 for the tradeoff.
package executor

import (
	"context"
	"log"

	"robottt/internal/command"
	"robottt/internal/hardware"
)

// Executor drives one queue against three hardware controllers.
type Executor struct {
	Queue   command.CommandQueue
	GPIO    hardware.GPIOController
	Stepper hardware.StepperController
	Servo   hardware.ServoController
}

// Run blocks, dispatching commands until ctx is cancelled, then closes all
// controllers before returning.
func (e *Executor) Run(ctx context.Context) {
	defer e.closeAll()

	for {
		cmd, err := e.Queue.Dequeue(ctx)
		if err != nil {
			return
		}
		e.dispatch(cmd)
	}
}

func (e *Executor) dispatch(cmd command.Command) {
	var err error
	switch c := cmd.(type) {
	case command.LEDCommand:
		err = e.GPIO.SetLED(c.On)
	case command.StepperCommand:
		if e.Stepper == nil {
			log.Print("executor: stepper controller not wired, dropping command")
			return
		}
		err = e.Stepper.Move(c.Steps, c.Dir)
	case command.ServoCommand:
		if e.Servo == nil {
			log.Print("executor: servo controller not wired, dropping command")
			return
		}
		err = e.Servo.SetAngle(c.AngleDeg)
	default:
		log.Printf("executor: unknown command type %T, dropping", cmd)
		return
	}
	if err != nil {
		log.Printf("executor: command %T failed: %v", cmd, err)
	}
}

func (e *Executor) closeAll() {
	if err := e.GPIO.Close(); err != nil {
		log.Printf("executor: close GPIO controller: %v", err)
	}
	if e.Stepper != nil {
		if err := e.Stepper.Close(); err != nil {
			log.Printf("executor: close stepper controller: %v", err)
		}
	}
	if e.Servo != nil {
		if err := e.Servo.Close(); err != nil {
			log.Printf("executor: close servo controller: %v", err)
		}
	}
}
