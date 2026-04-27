package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
	"github.com/mwaddip/blockhost-monitor/internal/config"
	"github.com/mwaddip/blockhost-monitor/internal/poller"
	"github.com/mwaddip/blockhost-monitor/internal/prov"
	"github.com/mwaddip/blockhost-monitor/internal/vmdb"
)

func main() {
	configPath := flag.String("config", "/etc/blockhost/monitor.yaml", "path to monitor config")
	flag.Parse()

	cfg, err := config.LoadMonitorConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	log := setupLogger(cfg.Log)

	manifest, err := prov.LoadManifest(cfg.Paths.ProvisionerManifest)
	if err != nil {
		log.Error("provisioner manifest", "error", err)
		os.Exit(1)
	}

	metricsCmd, err := manifest.GetCommand("metrics")
	if err != nil {
		log.Error("provisioner missing metrics command", "error", err)
		os.Exit(1)
	}

	dbCfg, err := config.LoadDbConfig(cfg.Paths.DbConfig)
	if err != nil {
		log.Error("db config", "error", err)
		os.Exit(1)
	}

	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
				return nil, fmt.Errorf("%w: stderr=%s", err, bytes.TrimSpace(exitErr.Stderr))
			}
			return nil, err
		}
		return out, nil
	}

	store := poller.NewStore()
	coll := collector.New(metricsCmd, run)
	discover := func() ([]vmdb.VM, error) {
		return vmdb.LoadActiveVMs(dbCfg.DbFile)
	}

	p := poller.New(poller.Options{
		Collector:   coll,
		Store:       store,
		DiscoverVMs: discover,
		Budget:      time.Duration(cfg.Polling.BudgetMs) * time.Millisecond,
		MinInterval: time.Duration(cfg.Polling.MinIntervalMs) * time.Millisecond,
		Log:         log,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	log.Info("starting blockhost-watchdog",
		"provisioner", manifest.Name,
		"budget_ms", cfg.Polling.BudgetMs,
		"min_interval_ms", cfg.Polling.MinIntervalMs,
	)

	if err := p.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("poller exited", "error", err)
		os.Exit(1)
	}

	log.Info("blockhost-watchdog stopped")
}

func setupLogger(cfg config.LogConfig) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{}

	switch cfg.Level {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}

	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
