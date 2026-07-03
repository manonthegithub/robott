// Command robottt is the robot HTTP control API: it loads config, wires up
// the hardware controllers, command queue, executor, and HTTP server, then
// runs until an OS signal requests shutdown.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"robottt/internal/api"
	"robottt/internal/command"
	"robottt/internal/config"
	"robottt/internal/executor"
	"robottt/internal/hardware/gpiodirect"
)

func main() {
	// .env is optional: local/dev convenience, absent in prod (systemd unit
	// sets real env vars instead). Only report load errors for a file that
	// actually exists.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatalf(".env: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	gpio, err := gpiodirect.NewGPIO(cfg.GPIOChip, cfg.LEDOffset)
	if err != nil {
		log.Fatalf("hardware: %v", err)
	}
	stepper, err := gpiodirect.NewStepper(cfg.GPIOChip, cfg.StepperStepOffset, cfg.StepperDirOffset, cfg.StepperPulseDelay)
	if err != nil {
		log.Fatalf("hardware: %v", err)
	}
	servo, err := gpiodirect.NewServo(cfg.ServoChipPath, cfg.ServoChannel, cfg.ServoMinAngle, cfg.ServoMaxAngle)
	if err != nil {
		log.Fatalf("hardware: %v", err)
	}

	queue := command.NewChannelQueue(cfg.QueueCapacity)

	exec := &executor.Executor{Queue: queue, GPIO: gpio, Stepper: stepper, Servo: servo}

	router := api.NewRouter(&api.Handlers{
		Queue:         queue,
		ServoMinAngle: cfg.ServoMinAngle,
		ServoMaxAngle: cfg.ServoMaxAngle,
	})
	server := &http.Server{Addr: cfg.ListenAddr, Handler: router}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go exec.Run(ctx)

	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown: %v", err)
	}
}
