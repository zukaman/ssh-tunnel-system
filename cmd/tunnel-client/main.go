package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"ssh-tunnel-system/pkg/config"
	"ssh-tunnel-system/pkg/tunnel"

	"github.com/sirupsen/logrus"
)

func main() {
	var configPath = flag.String("config", "configs/client.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadClientConfig(*configPath)
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}

	// Setup logging
	setupLogging(cfg.Logging)

	logrus.WithFields(logrus.Fields{
		"version":   "0.1.0",
		"client_id": cfg.ClientID,
		"server":    cfg.Server.Host,
	}).Info("Starting SSH Tunnel Client")

	// Create and start tunnel client
	client, err := tunnel.NewClient(cfg)
	if err != nil {
		logrus.Fatalf("Failed to create tunnel client: %v", err)
	}

	// Start client in goroutine
	if err := client.Start(); err != nil {
		logrus.Fatalf("Failed to start tunnel client: %v", err)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logrus.Info("Shutting down tunnel client...")
	client.Stop()
	logrus.Info("Tunnel client stopped")
}

func setupLogging(cfg config.LoggingConfig) {
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		logrus.Warnf("Invalid log level '%s', using 'info'", cfg.Level)
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)

	if cfg.Format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}

	if cfg.File != "" {
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			logrus.Warnf("Failed to open log file '%s': %v", cfg.File, err)
		} else {
			logrus.SetOutput(file)
		}
	}
}
