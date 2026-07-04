# Architecture — Robot HTTP Control API

## Component breakdown + data flow

```
HTTP client
    │  POST /led {on, delay_ms?}
    │  POST /stepper {steps, dir, delay_ms?}
    │  POST /servo {angle_deg, delay_ms?}
    │  POST /sequence {seq: [...]}
    │  POST /sequence/stop
    ▼
[api] handlers validate JSON → build Command / Operation tree
    │
    ├─ delay_ms == 0 (or n/a) ────────────────────────────┐
    │                                                      │
    ├─ delay_ms > 0 ──► goroutine ──► [command] DelayedEnqueue(delay, cmd)
    │                                     (sleeps delay, then EnqueueBlocking)
    │                                                      │
    └─ POST /sequence ──► [sequence] Sequencer.Start(seq)  │
                              409 if one already running   │
                              (single slot, not a registry)│
                              spawns goroutine: exec()      │
                              walks the Operation tree,     │
                              each HardwareCommand leaf ──► toCommand() ──► DelayedEnqueue
                                                                            │
       POST /sequence/stop ──► Sequencer.Stop() (cancels ctx; 404 if none)│
                                                                            ▼
                                          [command] CommandQueue.Enqueue(cmd)
                                          ── interface, v1 impl = buffered Go channel;
                                             Enqueue non-blocking (unchanged), plus
                                             EnqueueBlocking (blocks on full channel —
                                             Go's native backpressure — ctx only lets
                                             a caller interrupt the wait)
    │
    ▼
[executor] single goroutine, blocking Dequeue loop, serial dispatch, always
           dispatches immediately on Dequeue — the executor never sleeps;
           DelayedEnqueue above is the only sleep site in the whole system
    │  type switch on Command
    ▼
[hardware] interfaces: GPIOController / StepperController / ServoController
    │
    ├── v1 impl: gpiodirect (go-gpiocdev digital lines + sysfs HW PWM), runs on Pi5 itself
    └── future impl: mcu (serializes cmd, sends over UART/SPI to RP2040 co-processor)
```

Executor selects concrete hardware impl at startup (dependency injection in `main.go`) — swapping direct-Go ↔ MCU-offload means writing a new package satisfying the same 3 interfaces and changing one wiring line, per spec §8.

`internal/sequence` is deliberately independent of `internal/command` — its `Operation`/`Loop`/`OperationSequence` types are pure data with no import of `command`. The one file that bridges the two, `internal/sequence/convert.go`'s `toCommand(HardwareCommand) command.Command`, is the only place that knows both vocabularies, so the operation tree stays reusable/testable without pulling in hardware-dispatch types.

Package layout:
```
cmd/robottt/main.go          — wiring: config → queue → hardware impl → executor → sequencer → http server
internal/api/                — HTTP handlers, router, request/response DTOs
internal/command/            — Command types, CommandQueue interface + channel impl, DelayedEnqueue helper
internal/sequence/           — Operation/Loop/OperationSequence types, converter, Sequencer
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

**Command types** (internal, `internal/command`) — the executor-facing hardware vocabulary, unchanged by the delay/sequencing work:
```go
type Direction string
const ( DirCW Direction = "cw"; DirCCW Direction = "ccw" )

type Command interface{ isCommand() }

type LEDCommand struct{ On bool }
type StepperCommand struct{ Steps int; Dir Direction }
type ServoCommand struct{ AngleDeg float64 }
```

**Operation tree** (internal, `internal/sequence`) — the higher-level "intent" vocabulary a sequence request is built from; independent of `command`, converted to it only at dispatch time:
```go
type Operation any

type HardwareCommand interface{ Delay() time.Duration }

type baseHardwareCommand struct{ DelayMs int }
func (b baseHardwareCommand) Delay() time.Duration { return time.Duration(b.DelayMs) * time.Millisecond }

type LedCommand struct { baseHardwareCommand; On bool }
type ServoCommand struct { baseHardwareCommand; AngleDeg float64 }
type StepperCommand struct { baseHardwareCommand; Steps int; Dir string }

type Loop struct { Body []Operation; Times int }  // Times == 0 means infinite
type OperationSequence struct { Seq []Operation }
```

**HTTP request/response DTOs:**

| Endpoint | Request JSON | Success response | Error response |
|---|---|---|---|
| `POST /led` | `{"on": true, "delay_ms": 0}` | `202 {"status":"queued"}` | `400 {"error": "..."}` |
| `POST /stepper` | `{"steps": 200, "dir": "cw", "delay_ms": 0}` | `202 {"status":"queued"}` | `400`/`503 {"error": "..."}` |
| `POST /servo` | `{"angle_deg": 90.0, "delay_ms": 0}` | `202 {"status":"queued"}` | `400`/`503 {"error": "..."}` |
| `POST /sequence` | `{"seq": [Operation, ...]}` | `202 {"status":"queued"}` | `400`/`409 {"error": "..."}` |
| `POST /sequence/stop` | *(no body)* | `202 {"status":"stopped"}` | `404 {"error": "..."}` |

`delay_ms` is optional on the 3 existing endpoints, default `0`, meaning "unchanged behavior" — see below.

`Operation` (used both as `/sequence`'s array elements and nested inside `LoopOperation.body`) is a discriminated `oneOf` on `type`:
```yaml
Operation:
  oneOf: [LedOperation, ServoOperation, StepperOperation, LoopOperation]
  discriminator: {propertyName: type}
```
`LedOperation`/`ServoOperation`/`StepperOperation` *are* the request schemas for `POST /led`/`/servo`/`/stepper` — there's no separate `LedRequest`/`ServoRequest`/`StepperRequest` alongside them; the standalone endpoints and the sequence leaves share the exact same schema (plus `type` and `delay_ms` on each), single source of truth for each command's fields. `LoopOperation` has `type: "loop"`, `times` (`0` = infinite), and `body: Operation[]`.

`202 Accepted` on `/led`/`/stepper`/`/servo` with `delay_ms==0` reflects reality exactly as before: queued, not executed. With `delay_ms>0`, `202` now means "will be queued after the delay" — the command isn't on the queue yet when the response is sent (see flagged decision #5). `202` on `/sequence` means the sequence goroutine has been started, not that any step has executed yet.

`503` on the 3 existing endpoints is unchanged: returned when queue is full via the original non-blocking `Enqueue`/`select`/`default`, protecting against unbounded memory growth if the executor stalls. `/sequence` never returns `503` — it uses `EnqueueBlocking` internally instead (see below), so a full queue backpressures the sequence rather than rejecting the request; `/sequence` returns `409` instead, when a sequence is already running.

## Interfaces / contracts

```go
// internal/command
type CommandQueue interface {
    Enqueue(cmd Command) error                             // non-blocking; ErrQueueFull if full
    EnqueueBlocking(ctx context.Context, cmd Command) error // blocks until space or ctx done
    Dequeue(ctx context.Context) (Command, error)           // blocks until cmd or ctx done
}

// DelayedEnqueue is the single shared helper that ever sleeps before enqueuing —
// used by the delay_ms>0 single-shot path and by the Sequencer, never by the executor.
func DelayedEnqueue(ctx context.Context, q CommandQueue, delay time.Duration, cmd Command) error

// internal/sequence
type Sequencer struct {
    Queue command.CommandQueue
    Ctx   context.Context // server shutdown context; a running sequence's own
                           // cancellable context derives from this, so shutdown
                           // cancels it the same way it cancels everything else
                           // (falls back to context.Background() if nil)
    // mu, running, cancel — single-slot: only one sequence at a time
}
func (s *Sequencer) Start(seq OperationSequence) error // ErrAlreadyRunning if one is running
func (s *Sequencer) Stop() error                       // ErrNotRunning if none running

// the recursive walker, one goroutine per Start(), no separate "list" node type
// needed since Loop.Body / OperationSequence.Seq are already []Operation
func (s *Sequencer) exec(ctx context.Context, ops []Operation) error {
    for _, op := range ops {
        if ctx.Err() != nil {
            return ctx.Err()
        }
        switch o := op.(type) {
        case Loop:
            for i := 0; o.Times == 0 || i < o.Times; i++ {
                if err := s.exec(ctx, o.Body); err != nil {
                    return err
                }
            }
        case HardwareCommand:
            if err := command.DelayedEnqueue(ctx, s.Queue, o.Delay(), toCommand(o)); err != nil {
                return err
            }
        }
    }
    return nil
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

`main.go` wires one concrete impl of each hardware interface into the `Executor`, plus one `Sequencer` sharing the same `CommandQueue`. `gpiodirect` package provides the v1 impl of all three hardware controllers; a future `mcu` package would provide the same three interfaces, wired in with no changes to `api`, `command`, `sequence`, or `executor` packages.

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

4. **`EnqueueBlocking` backpressure means a stalled sequence can block indefinitely** if the queue never drains (e.g. executor stuck on a long stepper move). Acceptable since it's bounded by `ctx` — `Stop()` always works — and it's the same "serial executor is the hardware mutex" tradeoff already accepted in decision #2 above.

5. **Single-shot `delay_ms>0` responds `202` before the command is actually enqueued.** Previously `202` meant "on the queue now"; a delayed single command shifts that to "will be queued after the delay." Worth a doc/spec note so callers don't assume immediate queueing.

6. **Only one sequence can run at a time — a second `POST /sequence` gets `409`, not queued to run after the first finishes.** Simplest option and matches the spec as given; if "queue the next sequence" is wanted later that's a bigger change (a wait-queue, not just a single slot) and should be scoped separately.
