package main

import (
	"testing"

	"github.com/mwaddip/blockhost-monitor/internal/config"
)

func TestSetupLogger_JSON(t *testing.T) {
	log := setupLogger(config.LogConfig{Level: "debug", Format: "json"})
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
	if !log.Enabled(nil, -4) { // slog.LevelDebug = -4
		t.Error("debug level should be enabled")
	}
}

func TestSetupLogger_Text(t *testing.T) {
	log := setupLogger(config.LogConfig{Level: "warn", Format: "text"})
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
	if log.Enabled(nil, 0) { // slog.LevelInfo = 0
		t.Error("info level should not be enabled at warn level")
	}
}
