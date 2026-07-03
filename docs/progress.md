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
- [ ] **Not yet run: component 8 needs `go generate ./... && go mod tidy && go build ./... && go test ./...` on the Pi.** Handler code was written against the *expected* oapi-codegen v2 strict-server generated interface (types/function names) without a local Go toolchain to actually run the generator and confirm — first Pi run may need small fixes if the pinned `@v2.4.1` generator's actual generated names differ slightly.
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
- **Run `go generate ./... && go mod tidy && go build ./... && go test ./...` on the Pi for component 8** — first real compile of the codegen'd HTTP layer, written without a local toolchain to verify oapi-codegen v2's exact generated names/signatures.
- Sysfs PWM chip/channel path (`SERVO_CHIP_PATH`, e.g. `/sys/class/pwm/pwmchip0`) depends on which PWM-capable GPIO pin is wired and whether the `dtoverlay=pwm` (or similar) is enabled in Pi5's `/boot/firmware/config.txt` — needs to be set up on the Pi and confirmed before servo will work (architecture decision #3). Stepper STEP/DIR offsets also still need to be confirmed once wired.
- No linter/formatter wired yet (workflow phase 7, not started).
- MCP wrapper itself not yet scoped/planned — next step once component 8 builds clean on the Pi.
