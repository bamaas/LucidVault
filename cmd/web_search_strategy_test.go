package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"lucidvault/internal/agentsmd"
)

// TestLoadConfig_AgentWebSearchStrategy verifies AGENT_WEB_SEARCH_STRATEGY
// parsing: the four canonical values map to their typed enum, and unknown or
// empty values fall back to the default (fallback). Unset behaves like empty.
//
// It also pins acceptance criterion 1's "with a logged warning" clause: an
// unknown *non-empty* value must emit exactly one slog.Warn (carrying the
// offending value), while every recognized/empty/unset input must stay silent.
// Without this, deleting the slog.Warn in loadConfig would still pass.
func TestLoadConfig_AgentWebSearchStrategy(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		set      bool
		want     agentsmd.WebSearchStrategy
		wantWarn bool
	}{
		{name: "unset defaults to fallback", set: false, want: agentsmd.StrategyFallback, wantWarn: false},
		{name: "empty defaults to fallback", env: "", set: true, want: agentsmd.StrategyFallback, wantWarn: false},
		{name: "off", env: "off", set: true, want: agentsmd.StrategyOff, wantWarn: false},
		{name: "fallback", env: "fallback", set: true, want: agentsmd.StrategyFallback, wantWarn: false},
		{name: "time-sensitive", env: "time-sensitive", set: true, want: agentsmd.StrategyTimeSensitive, wantWarn: false},
		{name: "immediately", env: "immediately", set: true, want: agentsmd.StrategyImmediately, wantWarn: false},
		{name: "unknown falls back to fallback (warns)", env: "nonsense", set: true, want: agentsmd.StrategyFallback, wantWarn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OLLAMA_API_KEY", "test")
			t.Setenv("VAULT_PATH", "/tmp/test")

			if tt.set {
				t.Setenv("AGENT_WEB_SEARCH_STRATEGY", tt.env)
			} else {
				// t.Setenv registers cleanup; set empty then unset to exercise
				// the "not present" path.
				t.Setenv("AGENT_WEB_SEARCH_STRATEGY", "")
				if err := unsetenv(t, "AGENT_WEB_SEARCH_STRATEGY"); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}

			// Capture the global logger (loadConfig logs via slog default) for the
			// duration of this subtest so we can assert the warning fires exactly
			// when an unknown non-empty value falls back.
			var logBuf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.webSearchStrategy != tt.want {
				t.Errorf("AGENT_WEB_SEARCH_STRATEGY=%q (set=%v): webSearchStrategy=%q, want %q",
					tt.env, tt.set, cfg.webSearchStrategy, tt.want)
			}

			logged := logBuf.String()
			gotWarn := strings.Contains(logged, "unknown AGENT_WEB_SEARCH_STRATEGY")
			if gotWarn != tt.wantWarn {
				t.Errorf("AGENT_WEB_SEARCH_STRATEGY=%q (set=%v): warning logged=%v, want %v\nlog output: %q",
					tt.env, tt.set, gotWarn, tt.wantWarn, logged)
			}
			// When a warning is expected, it must carry the offending value so an
			// operator can see what was rejected.
			if tt.wantWarn && !strings.Contains(logged, tt.env) {
				t.Errorf("warning must name the rejected value %q; log output: %q", tt.env, logged)
			}
		})
	}
}
