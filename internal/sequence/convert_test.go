package sequence

import (
	"testing"
	"time"

	"robottt/internal/command"
)

func TestToCommand(t *testing.T) {
	tests := []struct {
		name string
		op   HardwareCommand
		want command.Command
	}{
		{
			name: "led",
			op:   NewLedCommand(true, 100),
			want: command.LEDCommand{On: true},
		},
		{
			name: "servo",
			op:   NewServoCommand(90, 200),
			want: command.ServoCommand{AngleDeg: 90},
		},
		{
			name: "stepper cw",
			op:   NewStepperCommand(200, "cw", 0),
			want: command.StepperCommand{Steps: 200, Dir: command.DirCW},
		},
		{
			name: "stepper ccw",
			op:   NewStepperCommand(50, "ccw", 0),
			want: command.StepperCommand{Steps: 50, Dir: command.DirCCW},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toCommand(tt.op)
			if got != tt.want {
				t.Fatalf("toCommand(%#v) = %#v, want %#v", tt.op, got, tt.want)
			}
		})
	}
}

func TestToCommand_PanicsOnUnrecognizedType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("toCommand() did not panic on an unrecognized HardwareCommand type")
		}
	}()

	toCommand(fakeHardwareCommand{})
}

// fakeHardwareCommand exists only to prove toCommand panics on a type it
// doesn't recognize (a construction bug elsewhere, never reachable via
// validated input).
type fakeHardwareCommand struct{}

func (fakeHardwareCommand) Delay() time.Duration { return 0 }
