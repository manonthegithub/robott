// Package config loads runtime configuration from environment variables.
// Pin/chip identifiers are required with no default — a wrong default could
// drive the wrong physical pin, so misconfiguration must fail fast at
// startup rather than silently pick a value.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultListenAddr        = ":8080"
	defaultQueueCapacity     = 32
	defaultStepperPulseDelay = time.Millisecond
	defaultServoMinAngle     = 0.0
	defaultServoMaxAngle     = 180.0
)

// Config holds all runtime configuration.
type Config struct {
	ListenAddr string

	GPIOChip          string
	LEDOffset         int
	StepperStepOffset int
	StepperDirOffset  int
	StepperPulseDelay time.Duration

	ServoChipPath string
	ServoChannel  int
	ServoMinAngle float64
	ServoMaxAngle float64

	QueueCapacity int
}

// Load reads Config from environment variables. Pin/chip identifiers are
// required; everything else has a documented default.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:        getEnvOr("LISTEN_ADDR", defaultListenAddr),
		StepperPulseDelay: defaultStepperPulseDelay,
		ServoMinAngle:     defaultServoMinAngle,
		ServoMaxAngle:     defaultServoMaxAngle,
		QueueCapacity:     defaultQueueCapacity,
	}

	var err error
	if c.GPIOChip, err = requireEnv("GPIO_CHIP"); err != nil {
		return nil, err
	}
	if c.LEDOffset, err = requireEnvInt("LED_OFFSET"); err != nil {
		return nil, err
	}
	if c.StepperStepOffset, err = requireEnvInt("STEPPER_STEP_OFFSET"); err != nil {
		return nil, err
	}
	if c.StepperDirOffset, err = requireEnvInt("STEPPER_DIR_OFFSET"); err != nil {
		return nil, err
	}
	if c.ServoChipPath, err = requireEnv("SERVO_CHIP_PATH"); err != nil {
		return nil, err
	}
	if c.ServoChannel, err = requireEnvInt("SERVO_CHANNEL"); err != nil {
		return nil, err
	}

	if v, ok := os.LookupEnv("STEPPER_PULSE_DELAY_US"); ok {
		us, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("config: STEPPER_PULSE_DELAY_US invalid: %w", err)
		}
		c.StepperPulseDelay = time.Duration(us) * time.Microsecond
	}
	if v, ok := os.LookupEnv("SERVO_MIN_ANGLE"); ok {
		if c.ServoMinAngle, err = strconv.ParseFloat(v, 64); err != nil {
			return nil, fmt.Errorf("config: SERVO_MIN_ANGLE invalid: %w", err)
		}
	}
	if v, ok := os.LookupEnv("SERVO_MAX_ANGLE"); ok {
		if c.ServoMaxAngle, err = strconv.ParseFloat(v, 64); err != nil {
			return nil, fmt.Errorf("config: SERVO_MAX_ANGLE invalid: %w", err)
		}
	}
	if v, ok := os.LookupEnv("QUEUE_CAPACITY"); ok {
		if c.QueueCapacity, err = strconv.Atoi(v); err != nil {
			return nil, fmt.Errorf("config: QUEUE_CAPACITY invalid: %w", err)
		}
	}

	return c, nil
}

func getEnvOr(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return fallback
}

func requireEnv(name string) (string, error) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", fmt.Errorf("config: required env var %s not set", name)
	}
	return v, nil
}

func requireEnvInt(name string) (int, error) {
	v, err := requireEnv(name)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s invalid integer: %w", name, err)
	}
	return n, nil
}
