package gpiodirect

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"robottt/internal/hardware"
)

const (
	// servoPeriodNs is the PWM period: 20ms = 50Hz, the standard servo
	// control signal frequency.
	servoPeriodNs = 20_000_000
	// servoMinPulseUs / servoMaxPulseUs are the pulse-width bounds
	// corresponding to minAngle/maxAngle, per common hobby servo spec.
	servoMinPulseUs = 500
	servoMaxPulseUs = 2500
)

// Servo drives a hardware PWM channel via the Linux sysfs PWM interface
// (/sys/class/pwm/pwmchipN/pwmM). See docs/architecture.md decision #3:
// verify sysfs PWM availability on the target Pi5 kernel before relying on
// this in production; software PWM is the documented fallback.
type Servo struct {
	pwmPath  string
	minAngle float64
	maxAngle float64
}

// NewServo configures PWM channel on chipPath (e.g.
// "/sys/class/pwm/pwmchip0"), exporting it first if not already exported,
// for a servo operating over [minAngle, maxAngle] degrees.
func NewServo(chipPath string, channel int, minAngle, maxAngle float64) (*Servo, error) {
	if maxAngle <= minAngle {
		return nil, fmt.Errorf("gpiodirect: servo maxAngle (%.2f) must be > minAngle (%.2f)", maxAngle, minAngle)
	}

	pwmPath := filepath.Join(chipPath, fmt.Sprintf("pwm%d", channel))

	if _, err := os.Stat(pwmPath); os.IsNotExist(err) {
		if err := writeSysfs(filepath.Join(chipPath, "export"), strconv.Itoa(channel)); err != nil {
			return nil, fmt.Errorf("gpiodirect: export pwm channel %d: %w", channel, err)
		}
	}

	if err := writeSysfs(filepath.Join(pwmPath, "period"), strconv.Itoa(servoPeriodNs)); err != nil {
		return nil, fmt.Errorf("gpiodirect: set pwm period: %w", err)
	}

	s := &Servo{pwmPath: pwmPath, minAngle: minAngle, maxAngle: maxAngle}

	mid := minAngle + (maxAngle-minAngle)/2
	if err := s.SetAngle(mid); err != nil {
		return nil, fmt.Errorf("gpiodirect: set initial servo angle: %w", err)
	}

	if err := writeSysfs(filepath.Join(pwmPath, "enable"), "1"); err != nil {
		return nil, fmt.Errorf("gpiodirect: enable pwm: %w", err)
	}

	return s, nil
}

func (s *Servo) SetAngle(deg float64) error {
	if deg < s.minAngle || deg > s.maxAngle {
		return fmt.Errorf("gpiodirect: servo angle %.2f outside [%.2f,%.2f]: %w", deg, s.minAngle, s.maxAngle, hardware.ErrOutOfRange)
	}

	pulseUs := servoMinPulseUs + (deg-s.minAngle)/(s.maxAngle-s.minAngle)*(servoMaxPulseUs-servoMinPulseUs)
	dutyNs := int64(pulseUs * 1000)

	if err := writeSysfs(filepath.Join(s.pwmPath, "duty_cycle"), strconv.FormatInt(dutyNs, 10)); err != nil {
		return fmt.Errorf("gpiodirect: set servo duty cycle: %w", err)
	}
	return nil
}

func (s *Servo) Close() error {
	if err := writeSysfs(filepath.Join(s.pwmPath, "enable"), "0"); err != nil {
		return fmt.Errorf("gpiodirect: disable pwm: %w", err)
	}
	return nil
}

func writeSysfs(path, value string) error {
	return os.WriteFile(path, []byte(value), 0644)
}

var _ hardware.ServoController = (*Servo)(nil)
