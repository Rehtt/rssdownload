package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Rehtt/rssdownload/internal/app"
	"github.com/Rehtt/rssdownload/internal/auth"
	"github.com/Rehtt/rssdownload/internal/config"
	"github.com/Rehtt/rssdownload/internal/database"
	"github.com/Rehtt/rssdownload/internal/httpapi"
	"github.com/Rehtt/rssdownload/internal/store"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("rssdownload stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Printf("rssdownload %s (commit %s, built %s)\n", version, commit, buildTime)
		return nil
	}
	resetPassword := len(args) >= 2 && args[0] == "admin" && args[1] == "reset-password"
	if resetPassword {
		args = args[2:]
	}
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	ctx := context.Background()
	db, dbPath, err := database.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer database.Close(db)
	dataStore := store.New(db)
	authService := auth.New(dataStore)

	if resetPassword {
		if err := authService.ResetPassword(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			return fmt.Errorf("reset administrator password: %w", err)
		}
		logger.Info("administrator password reset", "username", cfg.AdminUsername, "database", dbPath)
		return nil
	}
	if err := authService.Bootstrap(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		return err
	}

	appService := app.New(dataStore, logger)
	runContext, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()
	appService.Start(runContext)

	apiServer := httpapi.New(
		dataStore,
		authService,
		appService,
		logger,
		cfg.CookieSecure,
	)
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(
			"rssdownload started",
			"listen", cfg.Listen,
			"database", dbPath,
			"version", version,
		)
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case <-runContext.Done():
		logger.Info("shutting down")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			appService.Stop()
			return fmt.Errorf("serve HTTP: %w", err)
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		logger.Warn("HTTP shutdown did not finish cleanly", "error", err)
	}
	appService.Stop()
	return nil
}
