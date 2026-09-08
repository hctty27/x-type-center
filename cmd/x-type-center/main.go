package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hctty27/x-type-center/internal/config"
	"github.com/hctty27/x-type-center/internal/httpapi"
	"github.com/hctty27/x-type-center/internal/service"
	"github.com/hctty27/x-type-center/internal/store"
	webassets "github.com/hctty27/x-type-center/web"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "server":
		err = runServer(logger, os.Args[2:])
	case "migrate":
		err = runMigrate(logger, os.Args[2:])
	case "version":
		err = runVersion(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		logger.Error("command failed", "command", os.Args[1], "error", err)
		os.Exit(1)
	}
}

func runServer(logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("server does not accept positional arguments")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DSN, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	registry := service.NewRegistry(db)
	static := http.FileServer(http.FS(webassets.Files))
	api := httpapi.New(
		registry,
		cfg.SkillPackagePath,
		cfg.PublicURL,
		cfg.TrustedProxies,
		httpapi.BuildInfo{Version: version, Commit: commit, BuildTime: buildTime},
		logger,
	)
	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      api.Routes(static),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("type registry started", "addr", cfg.Addr, "version", version, "commit", commit)
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			stop()
			return
		}
		serverErr <- nil
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	if err := <-serverErr; err != nil {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

func runMigrate(_ *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("migrate does not accept positional arguments")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DSN, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	fmt.Fprintln(os.Stdout, "migration completed")
	return nil
}

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("version does not accept positional arguments")
	}

	fmt.Fprintf(os.Stdout, "x-type-center %s\ncommit: %s\nbuilt: %s\n", version, commit, buildTime)
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `x-type-center commands:
  server
      Start HTTP API and embedded Web UI.

  migrate
      Apply embedded MySQL schema migrations.

  version
      Print build version information.

Environment:
  TYPE_REGISTRY_ADDR
  TYPE_REGISTRY_DSN
  TYPE_REGISTRY_SKILL_PACKAGE_PATH
  TYPE_REGISTRY_PUBLIC_URL
  TYPE_REGISTRY_TRUSTED_PROXIES
`)
}
