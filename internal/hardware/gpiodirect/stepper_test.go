package gpiodirect

import (
	"errors"
	"testing"
	"time"

	"robottt/internal/command"
)

func TestStepper_Move(t *testing.T) {
	tests := []struct {
		name      string
		steps     int
		dir       command.Direction
		wantDir   int
		wantSteps int
	}{
		{name: "cw moves and sets dir high", steps: 3, dir: command.DirCW, wantDir: 1, wantSteps: 3},
		{name: "ccw moves and sets dir low", steps: 2, dir: command.DirCCW, wantDir: 0, wantSteps: 2},
		{name: "zero steps still sets dir, no pulses", steps: 0, dir: command.DirCW, wantDir: 1, wantSteps: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &fakeLine{}
			dirLine := &fakeLine{}
			s := &Stepper{stepLine: step, dirLine: dirLine, pulseDelay: time.Microsecond}

			if err := s.Move(tt.steps, tt.dir); err != nil {
				t.Fatalf("Move() error = %v, want nil", err)
			}

			if len(dirLine.values) != 1 || dirLine.values[0] != tt.wantDir {
				t.Fatalf("dirLine.values = %v, want [%d]", dirLine.values, tt.wantDir)
			}

			// Each step toggles STEP high then low.
			wantPulses := tt.wantSteps * 2
			if len(step.values) != wantPulses {
				t.Fatalf("stepLine.values len = %d, want %d (values=%v)", len(step.values), wantPulses, step.values)
			}
		})
	}
}

func TestStepper_Move_NegativeStepsRejected(t *testing.T) {
	s := &Stepper{stepLine: &fakeLine{}, dirLine: &fakeLine{}, pulseDelay: time.Microsecond}

	if err := s.Move(-1, command.DirCW); err == nil {
		t.Fatal("Move(-1, ...) error = nil, want error")
	}
}

func TestStepper_Move_UnknownDirectionRejected(t *testing.T) {
	s := &Stepper{stepLine: &fakeLine{}, dirLine: &fakeLine{}, pulseDelay: time.Microsecond}

	if err := s.Move(1, command.Direction("sideways")); err == nil {
		t.Fatal("Move with unknown direction error = nil, want error")
	}
}

func TestStepper_Move_DirLineErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	dirLine := &fakeLine{err: wantErr}
	s := &Stepper{stepLine: &fakeLine{}, dirLine: dirLine, pulseDelay: time.Microsecond}

	err := s.Move(1, command.DirCW)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Move() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestStepper_Close(t *testing.T) {
	step := &fakeLine{}
	dirLine := &fakeLine{}
	s := &Stepper{stepLine: step, dirLine: dirLine, pulseDelay: time.Microsecond}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if !step.closed || !dirLine.closed {
		t.Fatal("Close() did not close both lines")
	}
}
