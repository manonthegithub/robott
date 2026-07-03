# Spec — Robot HTTP Control API

## Goal
HTTP API on Pi 5, Go, controls robot LED, stepper motor, servo motor.

## Users
Dev/hobbyist on local network, sends HTTP req to move/set hardware. No end-user UI.

## Core features
1. HTTP API: endpoints to control LED (on/off), stepper motor (move N steps, dir), servo (set angle).
2. Command flow split: HTTP handlers validate + push command onto in-process Go channel (queue). Separate executor goroutine drains channel serially, drives hardware. Serial drain = natural mutex, no two cmds hit same pin same time.
3. Queue abstraction: channel wrapped behind interface (`CommandQueue` or similar), so swap to persistent broker (NATS/Redis) later needs no caller changes.
4. GPIO abstraction: wrap `go-gpiocdev` behind own interface (`GPIOController` or similar). No lib types leak outside wrapper. Swappable later (e.g. periph.io) without touching business logic.
5. Stepper motor: STEP/DIR driver chip (A4988/DRV8825/TMC2209 class), 2 GPIO pins (step pulse + dir).
6. Servo motor: Hardware PWM pin, angle set via PWM duty cycle. Wrapped behind interface (`ServoController`) — Software PWM impl may be added later, swappable without API change.
7. Auth: none in v1, but design (esp. HTTP layer) must allow adding auth middleware (e.g. API key header) later without rework.
8. Hardware control backend swap: executor-side hardware interfaces (`GPIOController`, `ServoController`, stepper control) must be swappable between "direct Go on Pi5" impl (gpiocdev/HW PWM, current v1) and "MCU co-processor offload" impl (e.g. RP2040 over UART/SPI, sends high-level cmds, MCU handles pulse timing) — without changing HTTP layer, queue, or command types. Only the executor-side impl behind the interface changes.

## Non-goals
- No auth/authz implementation in v1 (interface allows it later).
- No real message broker (NATS/Redis) implementation in v1 (interface allows it later).
- No multi-robot / multi-Pi orchestration.
- No web UI / frontend.
- No cloud connectivity / remote access beyond LAN.
- No MCU co-processor implementation in v1 (interface allows it later).

## Success criteria
- `curl` to LED endpoint toggles physical LED on/off.
- `curl` to stepper endpoint moves motor N steps in given direction.
- `curl` to servo endpoint sets servo to given angle (0-180 or configured range).
- All 3 hardware command types flow through single in-process channel queue, processed serially by one executor.
- Hardware interfaces (`GPIOController`, `ServoController`, `CommandQueue`) are mockable — unit tests run and pass without real Pi/hardware attached.
- Swapping direct-Go hardware impl for MCU-offload impl requires touching only executor-side wiring, no change to HTTP handlers, queue, or command types.
- Runs as single Go binary on Pi 5 (ARM64, Raspberry Pi OS).

## Constraints
- Language: Go.
- Target runtime: Raspberry Pi 5, ARM64, Linux (RP1 GPIO chip — requires chardev-based GPIO lib, not old mmap-style libs).
- GPIO lib: `warthog618/go-gpiocdev`, wrapped, not exposed.
- No paid API/cloud spend.
- No deadline given.
- Local network only, no auth v1 — architecture must keep auth easy to bolt on.
