package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"lnb_tk/internal/config"
	"lnb_tk/internal/env"
	"lnb_tk/internal/logger"
	"lnb_tk/internal/parser"
	"lnb_tk/internal/processor"
	"lnb_tk/internal/watcher"
)

func main() {
	log := logger.Default()

	envPath := flag.String("env", ".env", "path to environment file")
	flag.Parse()

	if err := env.LoadFile(*envPath); err != nil {
		log.Errorf("environment failed: %v", err)
		os.Exit(1)
	}

	settings, err := env.LoadSettings()
	if err != nil {
		log.Errorf("settings failed: %v", err)
		os.Exit(1)
	}

	watcherConfigs, err := config.Load(settings.ConfigPath)
	if err != nil {
		log.Errorf("configuration failed: %v", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fileProcessor := processor.New(log, parser.NewDispatcher(), processor.Options{
		Workers:         settings.Workers,
		QueueSize:       settings.QueueSize,
		StableDuration:  settings.StableFor,
		RetryDelay:      settings.RetryDelay,
		MaxRetries:      settings.MaxRetries,
		MetricsInterval: settings.MetricsInterval,
	})
	fileProcessor.Start(ctx)
	defer fileProcessor.Stop()

	manager := watcher.NewManager(log, watcherConfigs, fileProcessor, settings.ScanInterval)
	defer manager.Close()

	if err := manager.Start(ctx); err != nil {
		log.Errorf("watcher startup failed: %v", err)
		os.Exit(1)
	}

	log.Infof("log watcher running; press Ctrl+C to stop")
	<-ctx.Done()
	log.Infof("shutdown requested")
}
