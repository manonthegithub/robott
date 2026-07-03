# Architecture — Robot HTTP Control API

## Component breakdown + data flow

```
HTTP client
    │  POST /led {on}
    │  POST /stepper {steps, dir}
    │  POST /servo {angle_deg}
    ▼
[api] handlers validate JSON → build Command struct
    │
    ▼
[command] CommandQueue.Enqueue(cmd)   ── interface, v1 impl = buffered Go channel
    │
    ▼
[executor] single goroutine, blocking Dequeue loop, serial dispatch
    │  type switch on Command
    ▼
[hardware] interfaces: GPIOController / StepperController / ServoController
    │
    ├── v1 impl: gpiodirect (go-gpiocdev digital lines + sysfs HW PWM), runs on Pi5 itself
    └── future impl: mcu (serializes cmd, sends over UART/SPI to RP2040 co-processor)
```

Executor selects concrete hardware impl at startup (dependency injection in `main.go`) — swapping direct-Go ↔ MCU-offload means writing a new package satisfying the same 3 interfaces and changing one wiring line, per spec §8.

Package layout:
```
cmd/robottt/main.go          — wiring: config → queue → hardware impl → executor → http server
internal/api/                — HTTP handlers, router, request/response DTOs
internal/command/            — Command types, CommandQueue interface + channel impl
internal/executor/           — executor loop
internal/hardware/           — GPIOController / StepperController / ServoController interfaces
internal/hardware/gpiodirect/— v1 impl: go-gpiocdev + sysfs PWM, runs directly on Pi5
internal/config/             — pin/config loading from env vars
```

## Stack choices

| Choice | Why | Alternative rejected |
|---|---|---|
| Go stdlib `net/http` + Go 1.22 method-pattern `ServeMux` | 3 endpoints, no need for router lib features (middleware chains still doable via stdlib wrapping) | chi/gin — extra dep for no real gain at this size |
| `warthog618/go-gpiocdev` for digital GPIO (LED, stepper DIR, stepper STEP) | Only maintained Go lib matching Pi5's RP1 chardev GPIO chip; old mmap libs (go-rpio) broken on Pi5 | periph.io — heavier, broader scope than needed, wrap surface bigger |
| Hand-rolled sysfs PWM wrapper (`/sys/class/pwm/pwmchipN`) for servo HW PWM | No mature small Go lib for Pi5 HW PWM; sysfs interface is ~4 files (export/period/duty_cycle/enable), trivial to wrap directly, keeps external deps minimal | periph.io PWM support — Pi5 maturity unconfirmed, extra dep for a few file writes |
| In-process buffered Go channel behind `CommandQueue` interface | Matches spec — simplest, zero extra infra, swappable later | NATS/Redis now — no persistence need yet, adds a process to keep alive on a Pi |
| Software GPIO toggle loop (goroutine) for stepper STEP pulses, not HW PWM | See risky decision #1 below | HW PWM burst for STEP — rejected, breaks exact step-count guarantee |

## Data models / schemas

**Command types** (internal, `internal/command`):
```go
type Direction string
const ( DirCW Direction = "cw"; DirCCW Direction = "ccw" )

type Command interface{ isCommand() }

type LEDCommand struct{ On bool }
type StepperCommand struct{ Steps int; Dir Direction }
type ServoCommand struct{ AngleDeg float64 }
```

**HTTP request/response DTOs:**

| Endpoint | Request JSON | Success response | Error response |
|---|---|---|---|
| `POST /led` | `{"on": true}` | `202 {"status":"queued"}` | `400 {"error": "..."}` |
| `POST /stepper` | `{"steps": 200, "dir": "cw"}` | `202 {"status":"queued"}` | `400`/`503 {"error": "..."}` |
| `POST /servo` | `{"angle_deg": 90.0}` | `202 {"status":"queued"}` | `400`/`503 {"error": "..."}` |

`202 Accepted` reflects reality: HTTP layer only confirms the command was queued, not executed (executor runs async). No command-status/result endpoint in v1 (non-goal — keep simple).

`503` returned when queue is full (bounded channel, non-blocking enqueue via `select`/`default`) — protects against unbounded memory growth if executor stalls; caller should retry.

## Interfaces / contracts

```go
// internal/command
type CommandQueue interface {
    Enqueue(cmd Command) error          // non-blocking; ErrQueueFull if full
    Dequeue(ctx context.Context) (Command, error) // blocks until cmd or ctx done
}

// internal/hardware
type GPIOController interface {
    SetLED(on bool) error
    Close() error
}
type StepperController interface {
    Move(steps int, dir Direction) error   // blocks until move complete
    Close() error
}
type ServoController interface {
    SetAngle(deg float64) error
    Close() error
}
```

`main.go` wires one concrete impl of each into the `Executor`. `gpiodirect` package provides the v1 impl of all three; a future `mcu` package would provide the same three interfaces, wired in with no changes to `api`, `command`, or `executor` packages.

## External dependencies

| Dep | Reason it earns its place |
|---|---|
| `github.com/warthog618/go-gpiocdev` | Only viable maintained Go GPIO lib for Pi5's RP1 chardev interface |
| Go stdlib only otherwise (`net/http`, `os`, `context`) | Small surface area, avoid dependency bloat for a 3-endpoint API |

## Decisions most likely to be wrong — flagged for scrutiny

1. **Stepper STEP pulses via software GPIO toggle loop, not HW PWM.**
   HW PWM is free-running by frequency/duty-cycle, not by pulse *count* — turning it on for a computed duration to approximate N pulses risks off-by-one-ish step errors (bad for position accuracy). A software loop toggling the STEP line exactly N times guarantees the step *count* is exact, at the cost of coarser timing precision (Go scheduler/GC jitter, sub-ms range) on pulse *spacing*. Judged acceptable since position accuracy usually matters more than exact speed for a hobby robot. Revisit if high-speed precise motion profiles are needed later (would push toward the MCU co-processor path already designed for in spec §8).

2. **Single serial executor goroutine for all 3 device types.**
   Simple, and free hardware-mutex safety, but means a long stepper move blocks a queued LED toggle until it finishes. Acceptable for v1 given single-robot low-concurrency use, but worth revisiting (e.g. per-device worker goroutines) if responsiveness complaints show up.

3. **Sysfs-based hand-rolled PWM wrapper for servo.**
   Sysfs PWM interface is being deprecated in newer kernels in favor of char-device based control in some contexts; Pi5's actual current kernel support should be verified on real hardware before committing. If sysfs PWM proves unavailable/unstable on target Pi5 OS image, fallback is software PWM (already anticipated as swappable impl per spec §6).
