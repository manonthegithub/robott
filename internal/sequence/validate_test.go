package sequence

import (
	"errors"
	"testing"
)

func TestValidate_HappyPath(t *testing.T) {
	// Mirrors the architecture doc's example tree.
	seq := []Operation{
		Loop{Times: 10, Body: []Operation{
			NewLedCommand(true, 100),
			NewLedCommand(true, 200),
		}},
		NewServoCommand(90, 200),
		Loop{Times: 10, Body: []Operation{
			NewServoCommand(45, 0),
			NewServoCommand(0, 0),
		}},
	}
	if err := validate(seq, 0); err != nil {
		t.Fatalf("validate() error = %v, want nil", err)
	}
}

func TestValidate_EmptyTopLevelSeqIsValid(t *testing.T) {
	if err := validate(nil, 0); err != nil {
		t.Fatalf("validate(nil) error = %v, want nil (a no-op sequence is valid)", err)
	}
	if err := validate([]Operation{}, 0); err != nil {
		t.Fatalf("validate([]Operation{}) error = %v, want nil", err)
	}
}

func TestValidate_RejectionCases(t *testing.T) {
	tests := []struct {
		name    string
		ops     []Operation
		wantErr error
	}{
		{
			name:    "unknown operation type",
			ops:     []Operation{"not an operation"},
			wantErr: ErrUnknownOperation,
		},
		{
			name:    "negative loop times",
			ops:     []Operation{Loop{Times: -1, Body: []Operation{NewLedCommand(true, 0)}}},
			wantErr: ErrNegativeTimes,
		},
		{
			name:    "empty loop body, finite",
			ops:     []Operation{Loop{Times: 3, Body: nil}},
			wantErr: ErrEmptyLoopBody,
		},
		{
			name:    "empty loop body, infinite",
			ops:     []Operation{Loop{Times: 0, Body: []Operation{}}},
			wantErr: ErrEmptyLoopBody,
		},
		{
			name:    "negative delay on led",
			ops:     []Operation{NewLedCommand(true, -1)},
			wantErr: ErrNegativeDelay,
		},
		{
			name:    "negative delay on servo",
			ops:     []Operation{NewServoCommand(90, -1)},
			wantErr: ErrNegativeDelay,
		},
		{
			name:    "negative delay on stepper",
			ops:     []Operation{NewStepperCommand(10, "cw", -1)},
			wantErr: ErrNegativeDelay,
		},
		{
			name:    "invalid stepper dir",
			ops:     []Operation{NewStepperCommand(10, "sideways", 0)},
			wantErr: ErrInvalidDir,
		},
		{
			name: "rejection inside nested loop propagates",
			ops: []Operation{
				Loop{Times: 1, Body: []Operation{
					Loop{Times: 1, Body: []Operation{
						NewStepperCommand(10, "bogus", 0),
					}},
				}},
			},
			wantErr: ErrInvalidDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.ops, 0)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_LoopTimesOneBehavesLikeNoLoop(t *testing.T) {
	// Times==1 must validate identically to the body running unwrapped —
	// exact boundary between "loop" and "list" semantics.
	body := []Operation{NewLedCommand(true, 0), NewServoCommand(90, 0)}
	if err := validate([]Operation{Loop{Times: 1, Body: body}}, 0); err != nil {
		t.Fatalf("validate() with Times=1 error = %v, want nil", err)
	}
	if err := validate(body, 0); err != nil {
		t.Fatalf("validate() of the unwrapped body error = %v, want nil", err)
	}
}

func TestValidate_MaxDepthBoundary(t *testing.T) {
	// Build a chain of nested loops exactly MaxDepth deep, then one deeper.
	var build func(depth int) []Operation
	build = func(depth int) []Operation {
		leaf := []Operation{NewLedCommand(true, 0)}
		if depth == 0 {
			return leaf
		}
		return []Operation{Loop{Times: 1, Body: build(depth - 1)}}
	}

	atMax := build(MaxDepth)
	if err := validate(atMax, 0); err != nil {
		t.Fatalf("validate() at exactly MaxDepth (%d) error = %v, want nil", MaxDepth, err)
	}

	overMax := build(MaxDepth + 1)
	if err := validate(overMax, 0); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("validate() at MaxDepth+1 error = %v, want ErrTooDeep", err)
	}
}

func TestValidate_TripleNestedFiniteLoopsAreValid(t *testing.T) {
	seq := []Operation{
		Loop{Times: 2, Body: []Operation{
			Loop{Times: 2, Body: []Operation{
				Loop{Times: 2, Body: []Operation{
					NewLedCommand(true, 0),
				}},
			}},
		}},
	}
	if err := validate(seq, 0); err != nil {
		t.Fatalf("validate() on triple-nested loops error = %v, want nil", err)
	}
}
