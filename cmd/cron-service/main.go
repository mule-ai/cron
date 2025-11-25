package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"cron-microservice/internal/config"
	"cron-microservice/internal/scheduler"
	"cron-microservice/internal/server"
)

func main() {
	var (
		configFile = flag.String("config", "config.yaml", "Path to configuration file")
		addr       = flag.String("addr", ":8080", "HTTP server address")
	)
	flag.Parse()

	// Load configuration
	cfg := config.New(*configFile)
	if err := cfg.Load(); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create and start scheduler
	sched := scheduler.New(cfg)
	sched.Start()
	defer sched.Stop()

	// Load existing jobs
	if err := sched.LoadJobs(); err != nil {
		log.Printf("Warning: Failed to load some jobs: %v", err)
	}

	// Create and start HTTP server
	srv := server.New(cfg, sched)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Starting cron microservice on %s", *addr)
		if err := srv.Start(*addr); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-sigChan
	fmt.Println("\nShutting down gracefully...")
}