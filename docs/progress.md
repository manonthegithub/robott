# Progress Log — Robot HTTP Control API

## Phase status
- [x] Spec — approved (`docs/spec.md`)
- [x] Architecture — approved (`docs/architecture.md`)
- [x] Plan — approved (`docs/plan.md`)
- [x] Component 1: `internal/command` — implemented + unit tests written
- [x] Component 2: `internal/hardware` — interfaces implemented (compile-only, no logic)
- [x] Component 3: `internal/hardware/gpiodirect` — implemented + unit tests written (fake line / fake sysfs dir, no real hardware needed)
- [x] Component 4: `internal/executor` — implemented + unit tests written
- [x] Component 5: `internal/api` — implemented + unit tests written (httptest)
- [x] Component 6: `internal/config` — implemented + unit tests written
- [x] Component 7: `cmd/robottt/main.go` — wiring implemented
- [ ] **Not yet run**: no Go toolchain on dev machine — code has not been compiled or `go test` run anywhere. Must build/test on Pi 5 (or install Go) before trusting it.
- [ ] Refactor + tooling — not started (linter/formatter, pre-commit hook)
- [ ] Production build — not started
- [ ] Deployment — not started (n/a unless containerized/deployed as a service)
- [ ] Security review — not started

## Decision log
- Chose in-process Go channel (not NATS/Redis) for v1 command queue — hobby scale, no persistence need yet; wrapped behind `CommandQueue` interface for later swap.
- Chose `warthog618/go-gpiocdev` for digital GPIO — only maintained Go lib matching Pi5 RP1 chardev chip.
- Chose hand-rolled sysfs PWM wrapper for servo (not a library) — Pi5 HW PWM Go lib support unconfirmed; sysfs interface is a few files, cheap to hand-roll.
- Chose software GPIO toggle loop for stepper STEP pulses, not HW PWM — HW PWM guarantees frequency/duty but not exact pulse *count*; exact step count matters more for position accuracy. Reversed an earlier verbal suggestion (HW PWM for STEP too) once this was thought through in architecture phase.
- Single serial executor goroutine for all 3 device types — simplest, free hardware mutex, accepted tradeoff that a long stepper move blocks a queued LED toggle.
- Spec requires hardware backend (direct-Go vs MCU co-processor) be swappable behind the same 3 interfaces — no MCU impl built yet (non-goal for v1), but `gpiodirect` package boundary was designed so a future `mcu` package is a drop-in.
- No auth in v1; `api.NewRouter` takes variadic `Middleware`, empty for now — auth becomes one added middleware, no handler rewrite.

## Open questions / blockers
- **Go toolchain not available on dev machine.** All code written and reasoned through carefully, but never compiled. First step on Pi 5 (or wherever Go gets installed): `go mod tidy && go build ./... && go vet ./... && go test ./...` — expect to fix minor compile issues before anything else.
- GPIO chip name (`GPIO_CHIP` env var, e.g. `gpiochip4`) and exact pin offsets for LED/STEP/DIR are Pi5/wiring-specific — must be confirmed against actual wiring before first run.
- Sysfs PWM chip/channel path (`SERVO_CHIP_PATH`, e.g. `/sys/class/pwm/pwmchip0`) depends on which PWM-capable GPIO pin is wired and whether the `dtoverlay=pwm` (or similar) is enabled in Pi5's `/boot/firmware/config.txt` — needs to be set up on the Pi and confirmed before servo will work (architecture decision #3).
- No linter/formatter wired yet (workflow phase 7, not started — code not compiled/tested yet so premature).
