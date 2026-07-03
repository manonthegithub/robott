package gpiodirect

import "testing"

type fakeLine struct {
	values []int
	closed bool
	err    error
}

func (f *fakeLine) SetValue(value int) error {
	if f.err != nil {
		return f.err
	}
	f.values = append(f.values, value)
	return nil
}

func (f *fakeLine) Close() error {
	f.closed = true
	return nil
}

func TestGPIO_SetLED(t *testing.T) {
	tests := []struct {
		name string
		on   bool
		want int
	}{
		{name: "on writes 1", on: true, want: 1},
		{name: "off writes 0", on: false, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := &fakeLine{}
			g := &GPIO{line: line}

			if err := g.SetLED(tt.on); err != nil {
				t.Fatalf("SetLED() error = %v, want nil", err)
			}
			if len(line.values) != 1 || line.values[0] != tt.want {
				t.Fatalf("line.values = %v, want [%d]", line.values, tt.want)
			}
		})
	}
}

func TestGPIO_Close(t *testing.T) {
	line := &fakeLine{}
	g := &GPIO{line: line}

	if err := g.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if !line.closed {
		t.Fatal("Close() did not close underlying line")
	}
}
