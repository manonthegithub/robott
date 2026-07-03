package gpiodirect

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"robottt/internal/hardware"
)

// newFakeSysfsChip creates a temp dir with pwm0 already "exported" (dir +
// files present), matching sysfs state after a real export, so NewServo's
// export-skip path (os.Stat succeeds) is exercised without a real chip.
func newFakeSysfsChip(t *testing.T, channel int) string {
	t.Helper()
	chipPath := t.TempDir()
	pwmPath := filepath.Join(chipPath, "pwm0")
	if err := os.MkdirAll(pwmPath, 0755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", pwmPath, err)
	}
	for _, f := range []string{"period", "duty_cycle", "enable"} {
		if err := os.WriteFile(filepath.Join(pwmPath, f), []byte("0"), 0644); err != nil {
			t.Fatalf("seed %s error = %v", f, err)
		}
	}
	return chipPath
}

func readSysfs(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(b)
}

func TestNewServo_ConfiguresPeriodAndEnables(t *testing.T) {
	chipPath := newFakeSysfsChip(t, 0)

	_, err := NewServo(chipPath, 0, 0, 180)
	if err != nil {
		t.Fatalf("NewServo() error = %v, want nil", err)
	}

	pwmPath := filepath.Join(chipPath, "pwm0")
	if got := readSysfs(t, filepath.Join(pwmPath, "period")); got != "20000000" {
		t.Fatalf("period = %q, want %q", got, "20000000")
	}
	if got := readSysfs(t, filepath.Join(pwmPath, "enable")); got != "1" {
		t.Fatalf("enable = %q, want %q", got, "1")
	}
}

func TestServo_SetAngle_WritesExpectedDutyCycle(t *testing.T) {
	tests := []struct {
		name string
		deg  float64
		want string
	}{
		{name: "min angle -> min pulse", deg: 0, want: "500000"},
		{name: "max angle -> max pulse", deg: 180, want: "2500000"},
		{name: "mid angle -> mid pulse", deg: 90, want: "1500000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chipPath := newFakeSysfsChip(t, 0)
			s, err := NewServo(chipPath, 0, 0, 180)
			if err != nil {
				t.Fatalf("NewServo() error = %v", err)
			}

			if err := s.SetAngle(tt.deg); err != nil {
				t.Fatalf("SetAngle(%v) error = %v, want nil", tt.deg, err)
			}

			got := readSysfs(t, filepath.Join(chipPath, "pwm0", "duty_cycle"))
			if got != tt.want {
				t.Fatalf("duty_cycle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServo_SetAngle_OutOfRangeRejected(t *testing.T) {
	chipPath := newFakeSysfsChip(t, 0)
	s, err := NewServo(chipPath, 0, 0, 180)
	if err != nil {
		t.Fatalf("NewServo() error = %v", err)
	}

	err = s.SetAngle(200)
	if !errors.Is(err, hardware.ErrOutOfRange) {
		t.Fatalf("SetAngle(200) error = %v, want wrapping ErrOutOfRange", err)
	}
}

func TestServo_Close_DisablesPwm(t *testing.T) {
	chipPath := newFakeSysfsChip(t, 0)
	s, err := NewServo(chipPath, 0, 0, 180)
	if err != nil {
		t.Fatalf("NewServo() error = %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if got := readSysfs(t, filepath.Join(chipPath, "pwm0", "enable")); got != "0" {
		t.Fatalf("enable = %q, want %q", got, "0")
	}
}
