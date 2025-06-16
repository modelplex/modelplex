// Package main provides the modelplex CLI application.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/modelplex/modelplex/internal/config"
	"github.com/modelplex/modelplex/internal/server"
)

const (
	// shutdownTimeout is the maximum time to wait for graceful shutdown
	shutdownTimeout = 5 * time.Second
)

// Options defines command line options
type Options struct {
	Config  string `short:"c" long:"config" default:"config.toml" description:"Path to configuration file"`
	Socket  string `short:"s" long:"socket" default:"./modelplex.socket" description:"Path to Unix socket"`
	Verbose bool   `short:"v" long:"verbose" description:"Enable verbose logging"`
	Version bool   `long:"version" description:"Show version information"`
}

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)
	parser.Name = "modelplex"
	parser.Usage = "[OPTIONS]"

	_, err := parser.Parse()
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if opts.Version {
		fmt.Printf("modelplex %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built: %s\n", date)
		os.Exit(0)
	}

	if opts.Verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		})))
		slog.Info("Verbose logging enabled")
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))
	}

	cfg, err := config.Load(opts.Config)
	if err != nil {
		slog.Error("Failed to load config", "file", opts.Config, "error", err)
		os.Exit(1)
	}

	slog.Info("Loaded configuration", "file", opts.Config)
	slog.Info("Starting server", "socket", opts.Socket)

	srv := server.New(cfg, opts.Socket)
	done := srv.Start()

	select {
	case err := <-done:
		if err != nil {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	default:
	}

	slog.Info("Server started successfully", "socket", opts.Socket)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	srv.Stop(ctx)
}
