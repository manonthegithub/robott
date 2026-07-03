package config

import (
	"testing"
	"time"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GPIO_CHIP", "gpiochip4")
	t.Setenv("LED_OFFSET", "17")
	t.Setenv("STEPPER_STEP_OFFSET", "27")
	t.Setenv("STEPPER_DIR_OFFSET", "22")
	t.Setenv("SERVO_CHIP_PATH", "/sys/class/pwm/pwmchip0")
	t.Setenv("SERVO_CHANNEL", "0")
}

func TestLoad_ValidEnvProducesExpectedConfig(t *testing.T) {
	setRequiredEnv(t)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if c.GPIOChip != "gpiochip4" {
		t.Errorf("GPIOChip = %q, want gpiochip4", c.GPIOChip)
	}
	if c.LEDOffset != 17 {
		t.Errorf("LEDOffset = %d, want 17", c.LEDOffset)
	}
	if c.StepperStepOffset != 27 || c.StepperDirOffset != 22 {
		t.Errorf("StepperStepOffset/DirOffset = %d/%d, want 27/22", c.StepperStepOffset, c.StepperDirOffset)
	}
	if c.ServoChipPath != "/sys/class/pwm/pwmchip0" || c.ServoChannel != 0 {
		t.Errorf("ServoChipPath/Channel = %s/%d, want /sys/class/pwm/pwmchip0/0", c.ServoChipPath, c.ServoChannel)
	}
	// Defaults.
	if c.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want default %q", c.ListenAddr, defaultListenAddr)
	}
	if c.QueueCapacity != defaultQueueCapacity {
		t.Errorf("QueueCapacity = %d, want default %d", c.QueueCapacity, defaultQueueCapacity)
	}
	if c.StepperPulseDelay != defaultStepperPulseDelay {
		t.Errorf("StepperPulseDelay = %v, want default %v", c.StepperPulseDelay, defaultStepperPulseDelay)
	}
	if c.ServoMinAngle != defaultServoMinAngle || c.ServoMaxAngle != defaultServoMaxAngle {
		t.Errorf("Servo angle range = [%v,%v], want defaults [%v,%v]", c.ServoMinAngle, c.ServoMaxAngle, defaultServoMinAngle, defaultServoMaxAngle)
	}
}

func TestLoad_OptionalOverridesApplied(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("QUEUE_CAPACITY", "64")
	t.Setenv("STEPPER_PULSE_DELAY_US", "500")
	t.Setenv("SERVO_MIN_ANGLE", "10")
	t.Setenv("SERVO_MAX_ANGLE", "170")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if c.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want :9090", c.ListenAddr)
	}
	if c.QueueCapacity != 64 {
		t.Errorf("QueueCapacity = %d, want 64", c.QueueCapacity)
	}
	if c.StepperPulseDelay != 500*time.Microsecond {
		t.Errorf("StepperPulseDelay = %v, want 500us", c.StepperPulseDelay)
	}
	if c.ServoMinAngle != 10 || c.ServoMaxAngle != 170 {
		t.Errorf("Servo angle range = [%v,%v], want [10,170]", c.ServoMinAngle, c.ServoMaxAngle)
	}
}

func TestLoad_MissingRequiredVarFailsFastNamingVar(t *testing.T) {
	tests := []string{
		"GPIO_CHIP",
		"LED_OFFSET",
		"STEPPER_STEP_OFFSET",
		"STEPPER_DIR_OFFSET",
		"SERVO_CHIP_PATH",
		"SERVO_CHANNEL",
	}

	for _, missing := range tests {
		t.Run(missing, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(missing, "")

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want error naming %s", missing)
			}
		})
	}
}

func TestLoad_InvalidIntegerFailsFast(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LED_OFFSET", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid LED_OFFSET")
	}
}
