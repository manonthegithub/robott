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

---

## 8. OpenAPI spec + codegen HTTP layer

Motivation: the robot will be driven by an LLM (via MCP later), not just curl. A
formal OpenAPI contract is what an MCP wrapper (or any other client tooling)
consumes to know the API's shape — hand-maintained docs would drift. This
component replaces the internals of `internal/api` with generated request/
response types + routing, keeping the same external contract (endpoints,
status codes, JSON shapes) so `internal/executor`, `internal/hardware`,
`internal/command`, `internal/config` are untouched.

**Steps**
1. Write `openapi.yaml` (repo root, OpenAPI 3.0.3): paths `/led`, `/stepper`,
   `/servo`, each `POST`, request/response schemas matching the existing hand
   -written DTOs (`LedRequest{on}`, `StepperRequest{steps,dir enum[cw,ccw]}`,
   `ServoRequest{angle_deg}`, `QueuedResponse{status}`, `ErrorResponse{error}`),
   responses `202`/`400`/`503`/`500`.
2. Add `oapi-codegen` (v2, `strict-server` + `std-http-server` generators —
   the latter targets Go 1.22 `http.ServeMux`, matching our existing stack).
   Config in `internal/api/gen/oapi-codegen.yaml`, generated output
   `internal/api/gen/server.gen.go` (package `apigen`, not hand-edited),
   driven by a `//go:generate` directive.
3. Rewrite `internal/api/handlers.go` to implement `apigen.StrictServerInterface`
   (one method per operation, typed request/response objects). Reuse a single
   generic helper (`enqueueAndRespond[R any]`) for the shared
   enqueue-then-map-to-response-type logic across all 3 handlers, since the
   generated response types are structurally identical but distinct per
   operation. Business-rule validation not expressible in JSON Schema (steps
   > 0, servo angle range from config) stays hand-written in each handler,
   returning the generated 400 response type directly.
4. Rewrite `internal/api/router.go`: build the generated strict handler with
   custom `RequestErrorHandlerFunc`/`ResponseErrorHandlerFunc` (so malformed
   JSON still yields our `{"error": "..."}` shape, not the generator's
   default plain text), wrap in a body-size-limit `Middleware` (replaces the
   old `http.MaxBytesReader` call site), keep the existing `Middleware` chain
   for the future auth slot.
5. Delete `internal/api/dto.go` (types now generated).
6. Rewrite `internal/api/handlers_test.go` — stays black-box httptest against
   `NewRouter`, same request/response JSON asserted, so most cases carry over
   with minimal changes.

**Inputs/outputs/side effects**
- No behavior change visible to HTTP clients — same endpoints/status codes/
  JSON shapes as before.
- Generated file is a build artifact in the sense that it's produced by
  `go generate`, but is committed (Go convention: commit generated code so
  `go build` doesn't require `oapi-codegen` installed).

**Error cases**
- Same as component 5 (malformed JSON → 400, validation failure → 400, queue
  full → 503) plus explicit 500 mapping for any non-`ErrQueueFull` enqueue
  error (previously an implicit/unreachable branch — now a real generated
  response type, closes the gap in the error-handling checklist).

**Definition of done**
- `go generate ./...` produces `server.gen.go` without errors.
- `go build ./... && go test ./...` green, `internal/api` tests pass against
  the new generated-type-based handlers with no change in asserted HTTP
  behavior.
- `openapi.yaml` validates as well-formed OpenAPI 3.0.3 (checked implicitly
  by `oapi-codegen` accepting it).

**Decision flagged for scrutiny**
- JSON Schema constraints like `dir: enum[cw,ccw]` and `steps: minimum: 1` are
  documentation-only unless paired with a request-validation middleware
  (e.g. `kin-openapi` validator) — not added here (would need the spec kept
  in lock-step with validation rules twice). Enum/range validation stays
  hand-written in Go, same as before codegen. Revisit if the schema grows
  complex enough that doc/code drift becomes a real risk.

---

## 9. Serve OpenAPI spec over HTTP

Moved `openapi.yaml` into `internal/api/` (go:embed can't reach outside its
package directory) and embed it directly into the binary, served at
`GET /openapi.yaml` on the same router. Lets an MCP wrapper (or anything
else) fetch the live contract at runtime instead of needing repo access.
Updated the `//go:generate` directive path in `internal/api/gen/generate.go`
to match the new location.

**Definition of done**: `curl localhost:8080/openapi.yaml` returns the spec.

---

## 10. MCP wrapper (`cmd/robottt-mcp`)

A separate binary/process, not part of the HTTP server: translates MCP tool
calls into HTTP requests against this API, so an LLM (Claude Desktop/Code
etc.) can drive the robot. Kept as its own binary rather than a mode flag on
`cmd/robottt`, since it's a distinct concern that could run on a different
host later.

**Steps**
1. Enable the oapi-codegen `client` generator (`internal/api/gen/oapi-codegen.yaml`)
   so `apigen.ClientWithResponses` — a typed HTTP client generated from the
   same spec — exists to call the robot API, instead of hand-writing HTTP
   calls a second time.
2. `internal/mcpserver/server.go`: wraps `apigen.ClientWithResponses`, exposes
   3 MCP tools (`set_led`, `move_stepper`, `set_servo`) via the official
   `github.com/modelcontextprotocol/go-sdk`. Each tool handler calls the
   corresponding generated client method and maps the HTTP response into an
   MCP tool result (error flag set on 4xx/5xx).
3. `cmd/robottt-mcp/main.go`: reads `ROBOT_API_URL` (default
   `http://localhost:8080`) and `MCP_LISTEN_ADDR` (default `:8081`), builds
   the server, serves over HTTP (`mcp.NewStreamableHTTPHandler`) rather than
   stdio — the Pi is already reachable on the network at a fixed address, so
   an MCP client (e.g. Claude Code on another machine) can connect directly
   via `claude mcp add robottt --transport http http://<pi>:8081` instead of
   needing to SSH in to spawn a stdio subprocess.

**Error cases**
- Robot API unreachable / non-2xx → tool result marked as error, HTTP
  status + body surfaced as the result text (so the LLM sees why a command
  failed, e.g. queue full → 503).

**Definition of done**
- `go build ./cmd/robottt-mcp` succeeds.
- Manual check: point an MCP-capable client (or `mcp` CLI inspector) at the
  binary, call `set_led`, confirm it reaches the running `robottt` HTTP
  server and toggles the LED.

**Decision flagged for scrutiny**
- Written without a local Go toolchain to confirm `github.com/modelcontextprotocol/go-sdk`'s
  exact API surface (same situation as oapi-codegen in component 8) — expect
  a fix-up round once built on the Pi.
