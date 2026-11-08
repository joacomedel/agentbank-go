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

	"github.com/joacomedel/agentbank-go/internal/agentruntime"
	"github.com/joacomedel/agentbank-go/internal/buildinfo"
	"github.com/joacomedel/agentbank-go/internal/cli"
	"github.com/joacomedel/agentbank-go/internal/client"
	"github.com/joacomedel/agentbank-go/internal/config"
	"github.com/joacomedel/agentbank-go/internal/db"
	"github.com/joacomedel/agentbank-go/internal/httpapi"
	"github.com/joacomedel/agentbank-go/internal/mcp"
	"github.com/joacomedel/agentbank-go/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			err = runServer(ctx)
		case "mcp":
			err = runMCP(ctx)
		default:
			err = cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
		}
	} else {
		err = runServer(ctx)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentbank:", err)
		os.Exit(1)
	}
}

func runMCP(ctx context.Context) error {
	cfg, err := cli.LoadConfig()
	if err != nil {
		return err
	}
	token := cfg.Token
	if cfg.Agent != "" {
		result, err := agentruntime.Ensure(ctx, agentruntime.Options{
			BaseURL: cfg.URL, HumanToken: cfg.HumanToken, Project: cfg.Project,
			Agent: cfg.Agent, Role: cfg.AgentRole,
		})
		if err != nil {
			return err
		}
		token = result.Token
	}
	if token == "" {
		return errors.New("MCP: falta token; corré agentbank login o definí AGENTBANK_TOKEN")
	}
	api := client.New(cfg.URL, token, cfg.Project)
	return mcp.Run(ctx, api, os.Stdin, os.Stdout, os.Stderr, buildinfo.Version)
}

func runServer(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	database, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		return err
	}

	bootstrap, err := service.New(database, nil).Bootstrap(ctx, cfg.BootstrapToken)
	if err != nil {
		return err
	}
	if bootstrap.Created {
		logger.Info("bootstrap: participante humano creado", "participant_id", bootstrap.ParticipantID)
	}

	go service.New(database, nil).RunOutboxLoop(ctx, time.Minute)

	handler := httpapi.New(database, logger, httpapi.Options{
		MaxBodyBytes: cfg.MaxBodyBytes,
		Version:      buildinfo.Version,
		CORSOrigins:  cfg.CORSOrigins,
	})
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("agentbank listening",
			"addr", cfg.Addr,
			"db", cfg.DBPath,
			"version", buildinfo.Version,
		)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("agentbank shutting down")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
