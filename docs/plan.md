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

## 10. MCP wrapper (`internal/mcpserver`, mounted in `cmd/robottt`)

Exposes the robot as MCP tools, so an LLM (Claude Desktop/Code etc.) can
drive it. Originally built as a separate binary/process (`cmd/robottt-mcp`)
translating MCP calls into HTTP requests against the REST API, on its own
port — reconsidered once it was clear that "could run on a different host
later" wasn't a benefit actually being used for one robot on one Pi, and was
just costing two processes/ports to keep in sync (`ROBOT_API_URL`,
`MCP_LISTEN_ADDR`). Folded into the same process as the REST API instead:
same port, mounted at `/mcp` on the same `http.ServeMux`, MCP tool handlers
call `api.Handlers` methods directly (no HTTP round-trip to itself). The
oapi-codegen `client` generator (added, then no longer needed) was removed
again.

**Steps**
1. `internal/mcpserver/server.go`: `Server` wraps `*api.Handlers` directly.
   Each MCP tool handler builds the matching generated request object
   (e.g. `apigen.PostLedRequestObject{Body: &apigen.LedRequest{...}}`) and
   calls the handler method in-process. The generated response object's
   `Visit*Response(w http.ResponseWriter) error` method is invoked against
   an `httptest.ResponseRecorder` to get an HTTP status/body pair without an
   actual network call, reusing the same response-encoding logic the REST
   API uses — then mapped into an MCP tool result (error flag on 4xx/5xx).
2. `Server.HTTPHandler()` builds the `*mcp.Server`, registers the 3 tools
   (`set_led`, `move_stepper`, `set_servo`), and returns
   `mcp.NewStreamableHTTPHandler(...)` as a plain `http.Handler`.
3. `cmd/robottt/main.go`: builds one `*api.Handlers`, shares it between
   `api.NewRouter` (mounted at `/`) and `mcpserver.New(handlers).HTTPHandler()`
   (mounted at `/mcp`) on one `http.ServeMux`, one `http.Server`, one port
   (`cfg.ListenAddr`). An MCP client anywhere on the network connects via
   `claude mcp add robottt --transport http http://<pi>:8080/mcp`.

**Error cases**
- Validation/queue-full/internal-error cases are identical to the REST API
  (component 5/8) since the same `Handlers` methods run either way — surfaced
  as the MCP tool result's status+body text, error flag set on 4xx/5xx.

**Definition of done**
- `go build ./... && go test ./...` green.
- Manual check: `claude mcp add` the running server, call `set_led` through
  an MCP client, confirm it reaches the same process and toggles the LED.

**Decision flagged for scrutiny**
- Written without a local Go toolchain to confirm `github.com/modelcontextprotocol/go-sdk`'s
  exact API surface for `mcp.NewStreamableHTTPHandler` (previously verified
  for `mcp.NewServer`/`mcp.AddTool`/stdio transport in the earlier round) —
  expect a possible fix-up round once built on the Pi.

---

## Constraint for components 11-15: no local Go toolchain

Same situation as every prior component (see `docs/progress.md`): written and
reviewed here, built/tested/fixed on the Pi. One extra wrinkle this round —
`internal/api/gen/server.gen.go` isn't even present in this checkout (it's a
`go:generate` output, not committed in the current tree), so there is nothing
to inspect locally to confirm oapi-codegen's exact output shape for anything
new added to `openapi.yaml`.

That's a low-risk bet for the `delay_ms` additions (plain optional int field,
identical pattern to `on`/`steps`/`angle_deg` already in the spec). It's a
real risk for the discriminated recursive `oneOf` (the `Operation` shape in
`docs/architecture.md`) — oapi-codegen's `oneOf`+`discriminator` support is a
known rough edge, and it can't be confirmed against real output here. An
earlier draft of this doc hedged that away with a flat tagged struct instead
of a real union — reverted per explicit direction to keep the `oneOf` design
discussed from the start. oapi-codegen's documented pattern for a `oneOf`
with a `discriminator` generates a union type with a `Discriminator() (string,
error)` method plus `As<Variant>()`/`From<Variant>()`/`Merge<Variant>()`
accessors backed by `json.RawMessage`, rather than plain struct fields —
component 14's mapper is written against that pattern. If oapi-codegen v2.4.1
actually generates something different, that mapper is the one place to fix,
not the domain hierarchy in `internal/sequence` (still untouched either way).

---

## 11. `internal/command` — `EnqueueBlocking` + `DelayedEnqueue` (modifies component 1)

Motivation: `docs/architecture.md` "Delayed and sequenced commands" — both the
`delay_ms` on single-shot endpoints and the sequencer need "sleep, then get
onto the queue, but let a caller cancel while waiting." One shared helper,
one place that ever sleeps before enqueuing (never the executor).

**Steps**
1. Add `EnqueueBlocking(ctx context.Context, cmd Command) error` to the
   `CommandQueue` interface.
2. Implement on `ChannelQueue`: `select { case ch <- cmd: return nil; case
   <-ctx.Done(): return ctx.Err() }` — no `default`, so it blocks on a full
   channel (Go's native backpressure, no extra logic needed for that part);
   `ctx` exists only so a caller can interrupt the wait.
3. Add `DelayedEnqueue(ctx context.Context, q CommandQueue, delay
   time.Duration, cmd Command) error`: if `delay > 0`, `select` on
   `time.After(delay)` / `ctx.Done()` first, then call `EnqueueBlocking`.

**Inputs/outputs/side effects**
- No new side effects beyond existing in-memory channel state.

**Error cases**
- `ctx` cancelled while blocked on send → returns `ctx.Err()`, caller (delayed
  single-shot goroutine or sequencer) treats as abort, not a crash.
- `ctx` cancelled during the delay sleep → same, returns before ever touching
  the queue.

**Definition of done**
- Unit tests: `EnqueueBlocking` on a full channel blocks until either space
  frees up (unblocks, no error) or `ctx` is cancelled (returns `ctx.Err()`,
  doesn't leak a goroutine). `DelayedEnqueue` with `delay=0` enqueues
  immediately (no measurable sleep); with `delay>0` enqueues only after
  approximately that duration; cancelling `ctx` mid-delay returns early
  without enqueuing.
- **Tricky edge cases:** `EnqueueBlocking` on a queue with free capacity
  returns immediately (doesn't accidentally wait). `ctx` already cancelled
  *before* the call (not just cancelled mid-wait) — returns `ctx.Err()`
  immediately, never attempts the send. Race between space freeing up and
  `ctx` being cancelled at (approximately) the same instant — either outcome
  (sent, or `ctx.Err()`) is acceptable, test asserts it's one or the other,
  never a panic/hang/double-send. `DelayedEnqueue` with a negative `delay`
  (shouldn't happen if callers validate first, but the helper itself must not
  hang — treat as `delay<=0`, no sleep).

---

## 12. `internal/sequence` — Operation tree + converter

**Steps**
1. `operation.go`: `Operation any`; `HardwareCommand` interface
   (`Delay() time.Duration`); `baseHardwareCommand{DelayMs int}`;
   `LedCommand`/`ServoCommand`/`StepperCommand` (embed
   `baseHardwareCommand` + their own fields); `Loop{Body []Operation, Times
   int}` (`Times == 0` = infinite); `OperationSequence{Seq []Operation}`.
   No import of `internal/command` in this file.
2. `convert.go`: `toCommand(HardwareCommand) command.Command`, type-switches
   over the 3 concrete types, `panic`s on an unrecognized type (defensive —
   should be unreachable if construction is only ever done from the
   validated wire format, per component 14).
3. `validate.go`: `validate(seq []Operation, depth int) error` — rejects
   unknown/zero-value operations, `Loop.Times < 0`, `Loop` with an empty
   `Body` (a `Times==0`/infinite loop with nothing in it is a silent
   busy-spin, not a no-op — reject it rather than let it burn CPU forever),
   `HardwareCommand.Delay() < 0`, `StepperCommand.Dir` not `"cw"`/`"ccw"`,
   and nesting deeper than a named constant `MaxDepth` (e.g. 5). Reused by
   `toCommand`'s assumption of well-formed input.

**Inputs/outputs/side effects**
- Pure data + pure functions, no I/O.

**Error cases**
- Covered by `validate` — see steps above; `toCommand` panics only on a
  construction bug elsewhere in the codebase, not on user input (input is
  validated before any `Operation` tree is built).

**Definition of done**
- Table-driven unit tests for `validate`: every rejection case above, plus
  the happy path (a tree shaped like the architecture doc's example)
  passing. Unit tests for `toCommand`: each concrete type maps to the
  expected `command.Command` value.
- **Tricky edge cases:** empty top-level `Seq` (`[]Operation{}`) is valid (a
  sequence that does nothing, completes immediately — different from an
  empty `Loop.Body`, which is rejected because it would spin). `Loop.Times ==
  1` behaves identically to no loop at all (`Body` runs exactly once) — exact
  boundary between "loop" and "list" semantics. Nesting exactly at
  `MaxDepth` passes, `MaxDepth+1` fails (off-by-one on the boundary, not
  "roughly right"). A `Loop` nested inside a `Loop` nested inside a `Loop`
  (finite, small counts) validates and executes correctly, not just one level
  of nesting.

---

## 13. `internal/sequence` — `Sequencer` engine

**Steps**
1. `Sequencer` struct: `Queue command.CommandQueue`, `mu sync.Mutex`,
   `running bool`, `cancel context.CancelFunc`.
2. `Start(seq OperationSequence) error`: under `mu`, if `running` return a
   sentinel `ErrAlreadyRunning`; else validate the tree (component 12),
   create a cancellable `context.Context`, set `running = true`, spawn
   `go s.run(ctx, seq.Seq)`, return `nil` immediately (caller doesn't wait
   for the sequence to finish).
3. `run(ctx, ops)`: calls `exec(ctx, ops)`, then under `mu` sets `running =
   false` regardless of outcome, logs the outcome (completed / cancelled /
   aborted-on-queue-error).
4. `exec(ctx, ops []Operation) error`: the recursive walker from the
   architecture doc — `Loop` repeats `exec` on `Body` `Times` times (or
   forever), `HardwareCommand` converts + calls `command.DelayedEnqueue`;
   checks `ctx.Err()` at the top of every loop iteration so an infinite loop
   actually stops within one iteration of being cancelled, not just between
   top-level steps.
5. `Stop() error`: under `mu`, if not `running` return `ErrNotRunning`; else
   call `cancel()`, return `nil` (doesn't block waiting for the goroutine to
   actually finish unwinding — `running` flips to `false` asynchronously in
   `run`).

**Inputs/outputs/side effects**
- Side effect: commands land on the shared `CommandQueue`, same as any other
  producer.

**Error cases**
- `Start` while already running → `ErrAlreadyRunning` (HTTP layer maps to
  `409`).
- `Stop` while nothing running → `ErrNotRunning` (HTTP layer maps to `404`).
- Queue error (`ctx` cancelled mid-`DelayedEnqueue`) → `exec` returns early,
  sequence ends, logged — not surfaced to any caller since `Start` already
  returned.

**Definition of done**
- Unit tests with a fake `CommandQueue` and an injectable clock/sleep seam:
  ordering of enqueued commands matches a nested example tree; finite loop
  enqueues exactly `Times × len(Body)` commands; infinite loop stopped via
  `Stop()` enqueues no more commands within one tick after cancellation;
  `Start` called twice returns `ErrAlreadyRunning` on the second call and
  does not disturb the first sequence; `Stop` with nothing running returns
  `ErrNotRunning`; a `CommandQueue` that blocks forever on `EnqueueBlocking`
  is unblocked and the sequence ends when `Stop()` is called mid-wait.
- **Tricky edge cases:** `Stop()` called twice in a row — second call returns
  `ErrNotRunning`, doesn't panic on a nil/already-cancelled `cancel` func.
  After a sequence finishes on its own (not stopped), `running` correctly
  flips back to `false` so a *new* `Start()` immediately after succeeds
  (rather than staying wrongly locked in "running"). `Start()` right after
  the *previous* sequence's goroutine has been cancelled but hasn't yet
  finished unwinding its current `DelayedEnqueue` call — must not allow two
  goroutines writing to the queue concurrently under the single-slot
  guarantee (the mutex around `running`/`cancel` needs to cover this, not
  just the initial check). An infinite loop with `delay_ms: 0` on every leaf
  hammers `EnqueueBlocking` as fast as the queue drains — confirm this is
  bounded by the queue's own backpressure (CPU-bound but not runaway memory),
  not a new failure mode to guard against.

---

## 14. `internal/api` — `delay_ms` on existing endpoints + `/sequence`, `/sequence/stop` (modifies components 5/8)

**Steps**
1. `openapi.yaml`: rename `LedRequest`/`StepperRequest`/`ServoRequest` to
   `LedOperation`/`StepperOperation`/`ServoOperation` outright and add `type`
   (enum of one literal each) plus `delay_ms` (integer, minimum 0, default 0)
   directly onto each — no `allOf` wrapper, no separate `*Request` schema
   alongside it. The standalone `/led`/`/stepper`/`/servo` endpoints and the
   sequence leaves now point at literally the same schema, so a command's
   fields are defined exactly once. Add `LoopOperation` (`type: loop`,
   `times` [0 = infinite], `body: Operation[]`, `minItems: 1`). Add
   `Operation` as the `oneOf` over all four with a `discriminator` on `type`.
   Add `SequenceRequest{seq: Operation[]}`. Add paths `POST /sequence`
   (request `SequenceRequest`, responses `202 {"status":"queued"}` / `400` /
   `409` / `500`) and `POST /sequence/stop` (no body, responses `202
   {"status":"stopped"}` / `404` / `500`). Note: `type` is schema-`required`
   on `LedOperation`/`ServoOperation`/`StepperOperation` now even for
   standalone use (proper discriminator practice) — this is documentation
   only, same as every other "required" JSON Schema constraint in this spec
   (see component 8's decision note); nothing enforces it at runtime, so
   existing standalone `/led`/`/stepper`/`/servo` callers that never send
   `type` are unaffected.
2. Regenerate via existing `//go:generate` (`oapi-codegen`) — same
   `strict-server`/`std-http-server` generators as component 8, no config
   change. Expect the discriminated `oneOf` to generate a union type
   (`Operation.Discriminator()`, `AsLedOperation()`/`AsServoOperation()`/
   `AsStepperOperation()`/`AsLoopOperation()` backed by `json.RawMessage`)
   rather than plain struct fields — see the codegen-risk note above.
3. `handlers.go`: `PostLed`/`PostStepper`/`PostServo` — if `delay_ms == 0`,
   unchanged synchronous `Enqueue` path; if `> 0`, respond `202` immediately
   and spawn a goroutine calling `command.DelayedEnqueue` with a
   server-lifetime `context.Context` (so it's cancelled on shutdown, not
   per-request). Add `PostSequence`/`PostSequenceStop` calling into a
   `Sequencer` field added to `Handlers`, mapping `ErrAlreadyRunning` → `409`,
   `ErrNotRunning` → `404`. New file `sequence_convert.go`: `toOperation(op
   apigen.Operation) (sequence.Operation, error)` calls `op.Discriminator()`
   then the matching `op.As<Variant>Operation()` accessor, recursively
   mapping into the `sequence.Operation`/`Loop` hierarchy, doing the same
   per-field validation the other 3 handlers already do inline (servo range
   via `h.ServoMinAngle`/`MaxAngle`, stepper `steps>0`/`dir` enum, via the
   same `command.Direction(...)` cast rather than referencing generated enum
   consts) so a servo step *inside* a sequence is checked exactly as strictly
   as a standalone `POST /servo` — this is the one place the generated union
   type and domain-hierarchy construction meet, kept small and isolated on
   purpose.
4. `router.go`: no structural change — new operations flow through the same
   generated strict-server registration.

**Inputs/outputs/side effects**
- Delayed single-shot commands and running sequences outlive the HTTP
  request that started them (spawned goroutines / the `Sequencer`'s own
  goroutine) — the request/response cycle no longer brackets the full
  lifetime of the side effect for these cases, unlike the `delay_ms==0` /
  existing-4-endpoint behavior.

**Error cases**
- Same `400`/`503`/`500` cases as components 5/8 for the 3 existing
  endpoints, now also covering `delay_ms < 0` (schema `minimum: 0` plus a
  code-level check, same "enforced in code, not just schema" pattern already
  used for `steps`/`dir`).
- `/sequence`: `400` on a tree that fails `sequence.validate` (unknown type,
  bad nesting, negative delay/times), `409` on `ErrAlreadyRunning`.
- `/sequence/stop`: `404` on `ErrNotRunning`.

**Definition of done**
- `go generate ./...` produces updated `server.gen.go`.
- httptest unit tests: `delay_ms==0` on all 3 existing endpoints — unchanged
  behavior (regression coverage). `delay_ms>0` — `202` returned before the
  command reaches a test double's queue, command appears on the queue only
  after the delay elapses (using a short test delay + fake clock/queue, not
  a real multi-second sleep). `/sequence` happy path — nested tree example
  from the architecture doc round-trips to the expected sequence of enqueued
  commands. `/sequence` while already running → `409`. `/sequence/stop` with
  none running → `404`; with one running → `202` and no further commands
  enqueued afterward.
- **Tricky edge cases:** a `{"type":"servo","angle_deg":999}` nested inside a
  `loop` is rejected with `400` exactly like a standalone out-of-range
  `POST /servo` would be — proves the mapper isn't a looser validation path
  than the direct endpoints. An unrecognized `type` value (one not in the
  discriminator mapping) → `400` via the mapper's own error, not a panic.
  `delay_ms` omitted entirely (not just `0`) on a single-shot request —
  JSON-omitted optional field behaves identically to explicit `0` (unchanged
  sync path), since a caller (especially an LLM generating JSON) may omit
  rather than send `0`; the same applies to `on` omitted inside a `led`
  operation — with fields merged via `allOf` rather than nested under a
  sub-object, an omitted `on` decodes as `false` (a valid, meaningful
  command), not a mismatched-payload error the way it would if the fields
  were nested under a separate object. Malformed/oversized `/sequence` body
  (e.g. deeply nested JSON crafted to exceed `MaxDepth` before validation
  even walks it) still rejected cleanly — covered by the existing
  body-size-limit middleware from component 8 plus `sequence.validate`'s
  depth check.

---

## 15. `cmd/robottt/main.go` — wire `Sequencer` (modifies component 7)

**Steps**
1. Construct one `*sequence.Sequencer{Queue: queue}` alongside the existing
   `ChannelQueue`/`Executor` construction, pass it into `api.Handlers`.
2. No new config/env vars needed — `Sequencer` only needs the already-
   constructed `CommandQueue`.

**Definition of done**
- `go build ./... && go test ./...` green.
- Manual smoke test on Pi5: `POST /sequence` with a small finite loop moves
  LED/servo in the expected order; `POST /sequence` with `times: 0`
  (infinite) followed by `POST /sequence/stop` actually stops it; a second
  concurrent `POST /sequence` while one is running gets `409`.

---

## 16. `internal/mcpserver` — MCP parity for delay_ms + /sequence, /sequence/stop (modifies component 10)

Originally deferred as out of scope for this round; built once a
post-implementation review flagged that `internal/mcpserver` hadn't been
touched at all, so an LLM driving the robot via MCP — the actual point of
this project's pivot — couldn't reach any of components 11-15's new
capability.

**Steps**
1. `LedInput`/`StepperInput`/`ServoInput` gain a `delay_ms` field, passed
   straight through to the corresponding `apigen.*Operation.DelayMs`.
2. New `internal/mcpserver/sequence_convert.go`: `SequenceStepInput`, a flat
   recursive struct (MCP's schema reflection can't describe a discriminated
   `oneOf` the way `openapi.yaml` can), and `RunSequenceInput{Seq
   []SequenceStepInput}`/`StopSequenceInput{}`. `toGenOperation` converts a
   `SequenceStepInput` tree into the generated `apigen.Operation` union via
   its `From<Variant>Operation()` setters — the mirror image of
   `internal/api/sequence_convert.go`'s `As<Variant>Operation()` usage, same
   codegen-risk profile.
3. Two new tools, `run_sequence`/`stop_sequence`, calling `h.PostSequence`/
   `h.PostSequenceStop` directly (same validation path a REST call gets,
   nothing duplicated).

**Definition of done**
- `internal/mcpserver/server_test.go` (package had zero tests before):
  happy paths for all 5 tools; `delay_ms>0` timing; a validation failure
  (bad stepper request) surfaces as an `IsError` tool result, not a Go
  error; servo-out-of-range inside a sequence rejected the same way;
  an unknown step `type` fails before `PostSequence` is even called (a Go
  error from the converter, not an `IsError` result); running →
  `stop_sequence` → no further enqueues.
- `go build ./... && go test ./...` green on the Pi (same unverified-codegen
  caveat as components 14-15).

---

## Sequencer shutdown-awareness fix (found in post-implementation review, modifies component 13)

`Sequencer.Start()` derived its cancellable context from `context.Background()`
unconditionally — never anything tied to server shutdown, unlike the
`delay_ms>0` single-shot path which correctly threads `Handlers.Ctx` through.
A sequence running during shutdown would be left permanently parked on
`EnqueueBlocking` once the executor stopped draining the queue.

**Fix:** `Sequencer` gained a `Ctx context.Context` field; `Start()` derives
its per-run context from `s.Ctx`, falling back to `context.Background()` if
nil (so existing callers/tests that don't set it are unaffected).
`cmd/robottt/main.go` now passes its shutdown `ctx` into the `Sequencer` it
constructs. New tests in `internal/sequence/sequencer_test.go`: a running
sequence ends on its own when `Ctx` is cancelled without anyone calling
`Stop()`, and the nil-`Ctx` fallback still behaves exactly as before.

---

## 17. `Par` (fork/join) — modifies components 12-16

Motivation: a heartbeat blink and a servo sweep in the same sequence ran
strictly one after the other (`Loop`/leaf steps are inherently sequential),
which looked wrong for anything meant to happen "together." `Par` runs
several branches concurrently and joins before the sequence continues — see
`docs/architecture.md` decision #7 for exactly what "concurrently" does and
doesn't guarantee (branch *pacing* is independent; hardware *dispatch* still
serializes through the one shared queue/executor).

**Steps**
1. `internal/sequence/operation.go`: `Par{Branches [][]Operation}`.
2. `internal/sequence/validate.go`: `ErrEmptyPar` (zero branches) /
   `ErrEmptyParBranch` (any branch empty); each branch validated at
   `depth+1`, same as a `Loop` body — so `Par` nesting counts toward
   `MaxDepth` too.
3. `internal/sequence/sequencer.go`: `exec`'s switch gains a `Par` case
   calling `execPar`, which spawns one goroutine per branch (each calling
   `exec` against the same shared `Queue`), `sync.WaitGroup`s for all of
   them, and returns the first non-nil branch error (which branch that is
   isn't deterministic — they're concurrent — but a `Stop()` will surface
   as `context.Canceled` from whichever branch is reported first).
4. `openapi.yaml`: `ParOperation{type: "par", branches: Operation[][]}`
   (`minItems: 1` on both the branch list and each branch — structural
   non-emptiness documented at the schema level even though it's enforced
   in code), added to `Operation`'s `oneOf`/discriminator mapping.
5. `internal/api/sequence_convert.go`: `toOperation`'s switch gains a `"par"`
   case, recursing into each branch the same way the `"loop"` case recurses
   into `Body`.
6. `internal/mcpserver/sequence_convert.go`: `SequenceStepInput` gains
   `Branches [][]any` (not `[][]SequenceStepInput` — same schema-reflection
   cycle risk `Body []any` already dodges); `toGenOperation`'s `"par"` case
   `decodeStep`s each branch element then builds `apigen.ParOperation` via
   `FromParOperation`.

**Error cases**
- Same shape as `Loop`: `ErrEmptyPar`/`ErrEmptyParBranch` → `400` via
  `sequence.validate` (REST) or an `IsError` tool result (MCP); a bad
  operation inside any branch (e.g. out-of-range servo angle) is rejected
  exactly as strictly as it would be outside a `Par`.

**Definition of done**
- `internal/sequence`: table-driven `validate` tests (happy path, no
  branches, one empty branch, error inside a branch propagates, `Par`
  nesting counts toward `MaxDepth`). `sequencer_test.go`: two branches with
  different delays prove they raced independently (the faster branch's
  command arrives first despite being listed second, not just "listed
  first runs first"); a step after a `Par` only runs once *all* branches
  finish (waits for the slowest, not the fastest); an infinite loop inside
  each of two branches both end cleanly on `Stop()` (no hang).
- `internal/api`: `/sequence` with a `par` step happy path, empty
  `branches` rejected, out-of-range servo inside one branch rejected.
- `internal/mcpserver`: `run_sequence` with a `par` step happy path
  (`map[string]any` branch elements, the real JSON-decode shape), and
  out-of-range servo inside a branch surfaces as an `IsError` result.
- `go build ./... && go test ./...` green on the Pi (same unverified-codegen
  caveat as components 14-16 — `ParOperation`/`FromParOperation`/
  `AsParOperation` untested against real oapi-codegen output).
