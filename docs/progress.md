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
- [x] **Built + tested on Pi 5** (Go 1.22.12, arm64) — `go test ./...` all green: `command`, `api`, `config`, `executor`, `gpiodirect` pass; `cmd/robottt` and `hardware` correctly have no test files (wiring / pure interfaces).
- [x] LED confirmed working end-to-end on real hardware (`GPIO_CHIP=gpiochip0`, `LED_OFFSET=17`); stepper/servo temporarily disabled in `main.go` pending wiring (see executor nil-guard).
- [x] Component 8: OpenAPI spec + codegen HTTP layer (`docs/plan.md` §8) — motivated by the robot being LLM/MCP-controlled, so a formal API contract is needed. `openapi.yaml` written at repo root; `internal/api` rewritten to implement generated `apigen.StrictServerInterface` (`internal/api/gen`, driven by `//go:generate` + `oapi-codegen.yaml`); `dto.go` deleted (types now generated); `router.go` rewired onto the generated strict handler with a custom error formatter (keeps `{"error": "..."}` shape) and a body-size-limit middleware (generator doesn't apply one). Tests rewritten (still black-box httptest, mostly unchanged since HTTP contract didn't change) plus a new 500-path test.
- [x] **Component 8 verified on Pi 5** — `go generate ./... && go mod tidy && go build ./... && go test ./...` all green. One round of fixes needed: oapi-codegen v2 hoists shared `components.responses` into named types (`QueuedJSONResponse`, `BadRequestJSONResponse`, `QueueFullJSONResponse`, `InternalErrorJSONResponse`) embedded anonymously in each operation's response type, rather than aliasing the schema type directly — response construction in `handlers.go` fixed to go through the embedded field.
- [x] Component 9: serve OpenAPI spec over HTTP (`docs/plan.md` §9) — moved `openapi.yaml` into `internal/api/` (go:embed can't reach outside its package dir), embedded via `spec.go`, served at `GET /openapi.yaml`. Updated the `//go:generate` path in `internal/api/gen/generate.go` to match (`../openapi.yaml`, was `../../../openapi.yaml`). Router now built via `apigen.HandlerFromMux` so this extra route shares the mux with the generated one.
- [x] Component 10: MCP wrapper (`docs/plan.md` §10) — `internal/mcpserver` wraps a generated `apigen.ClientWithResponses` (client generator enabled in `oapi-codegen.yaml`), exposes `set_led`/`move_stepper`/`set_servo` as MCP tools via `github.com/modelcontextprotocol/go-sdk`. `cmd/robottt-mcp` runs it over stdio transport, reads `ROBOT_API_URL` (default `http://localhost:8080`).
- [x] **Components 9+10 verified on Pi 5** — `go generate ./... && go mod tidy && go build ./... && go test ./...` all green, first try, no fixes needed. `HandlerFromMux`, the `modelcontextprotocol/go-sdk` API surface, and the `apigen.StepperRequestDir` enum type all matched what was written.
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
- Pivoted to OpenAPI-spec-first + codegen for the HTTP layer once the real goal (LLM/MCP control of the robot) came up — a formal contract is what an MCP wrapper consumes; hand-maintained docs would drift. Retrofit approach chosen over "generate stub then implement" since 3 endpoints were already hand-written and tested — codegen replaces the internals, not the whole component.
- JSON Schema constraints (`dir` enum, `steps` minimum) stay documentation-only; enum/range validation is still hand-written Go in `internal/api/handlers.go`, not enforced by a request-validation middleware (would need the spec kept in lock-step with validation logic twice — revisit only if the schema grows enough that drift becomes a real risk).

## Open questions / blockers
- Sysfs PWM chip/channel path (`SERVO_CHIP_PATH`, e.g. `/sys/class/pwm/pwmchip0`) depends on which PWM-capable GPIO pin is wired and whether the `dtoverlay=pwm` (or similar) is enabled in Pi5's `/boot/firmware/config.txt` — needs to be set up on the Pi and confirmed before servo will work (architecture decision #3). Stepper STEP/DIR offsets also still need to be confirmed once wired.
- No linter/formatter wired yet (workflow phase 7, not started).
- Run `go generate ./... && go mod tidy && go build ./... && go test ./...` on the Pi for components 9+10, then `go get github.com/modelcontextprotocol/go-sdk@latest` if `go mod tidy` doesn't resolve it on its own.
