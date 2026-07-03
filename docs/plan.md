# Plan — Robot HTTP Control API

Components in dependency order. Each done = tests green before next starts.

---

## 1. `internal/command` — Command types + queue

**Steps**
1. Define `Direction` type + consts (`cw`/`ccw`).
2. Define `Command` marker interface + `LEDCommand`, `StepperCommand`, `ServoCommand` structs.
3. Define `CommandQueue` interface (`Enqueue`, `Dequeue`).
4. Implement `ChannelQueue` — buffered chan, configurable capacity, non-blocking `Enqueue` (select/default → `ErrQueueFull`), blocking `Dequeue` (select on chan / ctx.Done()).

**Inputs/outputs/side effects**
- In: `Command` value on `Enqueue`. Out: `Command` value + error on `Dequeue`.
- Side effect: none beyond in-memory chan state.

**Error cases**
- Queue full → `ErrQueueFull`, caller (HTTP handler) turns into `503`.
- `Dequeue` with cancelled ctx → returns ctx.Err(), executor exits cleanly on shutdown.

**Definition of done**
- Unit tests: enqueue/dequeue roundtrip, full-queue rejection, ctx-cancel unblocks Dequeue. All green.

---

## 2. `internal/hardware` — Controller interfaces

**Steps**
1. Define `GPIOController`, `StepperController`, `ServoController` interfaces (per architecture doc).
2. Define sentinel errors (e.g. `ErrOutOfRange` for bad servo angle).

**Inputs/outputs/side effects**
- Pure interface definitions, no logic — no tests needed beyond compile check.

**Definition of done**
- Package compiles, referenced by both `gpiodirect` and `executor`.

---

## 3. `internal/hardware/gpiodirect` — v1 hardware impl (Pi5 direct)

**Steps**
1. `gpio.go`: wrap `go-gpiocdev` line request for LED pin → `SetLED(on bool) error`.
2. `stepper.go`: wrap 2 GPIO lines (STEP, DIR) → `Move(steps int, dir Direction) error`, software toggle loop per architecture decision #1, configurable step-pulse delay constant.
3. `servo.go`: sysfs PWM wrapper (`/sys/class/pwm/pwmchipN/pwm0`: `export`, `period`, `duty_cycle`, `enable` files) → `SetAngle(deg float64) error`, angle→duty-cycle math as named constants (not magic numbers).
4. `Close()` on all three: release GPIO lines / unexport PWM, called on shutdown.

**Inputs/outputs/side effects**
- Side effects: real GPIO/sysfs writes. Requires running on Pi5 (or mocked chip in tests).
- Config in: pin numbers / pwmchip path, injected via constructor (from `internal/config`), not hardcoded.

**Error cases**
- GPIO line request failure (pin busy/invalid) → wrapped error with pin number context.
- Servo angle out of configured range → `ErrOutOfRange`, no partial write.
- Sysfs file open/write failure (permissions, pwmchip not present) → wrapped error, explicit message naming the sysfs path.
- Concurrent calls: not required — executor already serializes (architecture decision #2), so no internal locking needed here.

**Definition of done**
- Unit tests against `go-gpiocdev`'s mock chip (uapi simulation) for GPIO — verifies exact line get/set calls without real hardware.
- Sysfs PWM wrapper tested against a temp-dir-backed fake sysfs tree (inject base path) — verifies correct file writes for known angle inputs, no real Pi5 needed for CI.
- Manual smoke test on real Pi5 hardware (LED blinks, stepper turns N steps, servo moves) — recorded in `docs/progress.md`, not a CI-blocking step.

---

## 4. `internal/executor` — command executor loop

**Steps**
1. `Executor` struct holds `CommandQueue` + 3 controller interfaces.
2. `Run(ctx)`: loop `Dequeue`, type-switch, dispatch to matching controller method, log errors (don't crash loop on single command failure).
3. Graceful shutdown: `ctx` cancellation stops loop, calls `Close()` on all controllers.

**Inputs/outputs/side effects**
- In: commands from queue. Side effect: hardware state changes via controllers.

**Error cases**
- Controller method returns error → log with command context, continue loop (one bad command shouldn't kill executor).
- Unknown command type in switch → log + continue (defensive, shouldn't happen if `api` layer only builds known types).

**Definition of done**
- Unit tests with mock `CommandQueue` + mock controllers (table-driven: each command type dispatches to correct controller call, error from controller doesn't stop loop, ctx-cancel stops loop and calls Close on all controllers).

---

## 5. `internal/api` — HTTP layer

**Steps**
1. DTOs + JSON (de)serialization for 3 request bodies.
2. Validation: `steps` non-zero int, `dir` in `{cw,ccw}`, `angle_deg` within configured range (reuse range from config, not hardcoded).
3. Handlers build `Command`, call `CommandQueue.Enqueue`, map result → `202`/`400`/`503` with JSON body.
4. Router: Go 1.22 `http.ServeMux` with method+path patterns (`POST /led`, `POST /stepper`, `POST /servo`).
5. Middleware slot: wrap mux with a no-op passthrough middleware chain now (so auth middleware later per spec §7 is a one-line insert, not a rewrite).

**Inputs/outputs/side effects**
- In: HTTP request. Out: HTTP response + queue side effect.

**Error cases**
- Malformed JSON → `400 {"error": "invalid request body"}`.
- Out-of-range / bad enum values → `400` with specific field named in error.
- Queue full → `503 {"error": "queue full, retry"}`.
- Oversized body → reject via `http.MaxBytesReader` (defend against malformed/huge payloads).

**Definition of done**
- Unit tests (httptest): each endpoint happy path → `202` + queue received expected `Command`; each validation failure → `400`; simulated full queue → `503`.

---

## 6. `internal/config` — configuration

**Steps**
1. Struct: LED pin, stepper STEP/DIR pins, servo pwmchip path + channel, servo angle range, queue capacity, HTTP listen addr.
2. Load from env vars, sane defaults where safe (e.g. queue capacity), required vars fail fast at startup with clear error (no silent defaults for pin numbers — wrong pin could damage hardware).

**Error cases**
- Missing required env var → fail fast at startup, explicit message naming the var.
- Invalid value (non-numeric pin) → fail fast, explicit message.

**Definition of done**
- Unit tests: valid env → correct struct; missing/invalid required var → error naming the var.

---

## 7. `cmd/robottt/main.go` — wiring

**Steps**
1. Load config → construct `ChannelQueue` → construct `gpiodirect` controllers (injected pins from config) → construct `Executor` → construct `api` router (injected queue) → construct `http.Server`.
2. Run executor in goroutine, HTTP server in main goroutine.
3. OS signal handling (SIGINT/SIGTERM) → cancel shared ctx → graceful HTTP shutdown + executor drains/stops + controllers `Close()`.

**Error cases**
- Config load failure → log + exit non-zero before anything starts.
- Hardware controller construction failure (pin busy) → log + exit non-zero (fail fast, don't serve HTTP with broken hardware layer).

**Definition of done**
- `go build` produces working binary.
- Manual smoke test on Pi5: start binary, hit all 3 endpoints via curl, verify hardware response, verify Ctrl+C shuts down cleanly (no goroutine leak, GPIO lines released).
