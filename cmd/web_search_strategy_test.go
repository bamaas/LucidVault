package main

import (
	"testing"

	"lucidvault/internal/agentsmd"
)

// TestLoadConfig_AgentWebSearchStrategy verifies AGENT_WEB_SEARCH_STRATEGY
// parsing: the four canonical values map to their typed enum, and unknown or
// empty values fall back to the default (fallback). Unset behaves like empty.
func TestLoadConfig_AgentWebSearchStrategy(t *testing.T) {
	tests := []struct {
		name string
		env  string
		set  bool
		want agentsmd.WebSearchStrategy
	}{
		{name: "unset defaults to fallback", set: false, want: agentsmd.StrategyFallback},
		{name: "empty defaults to fallback", env: "", set: true, want: agentsmd.StrategyFallback},
		{name: "off", env: "off", set: true, want: agentsmd.StrategyOff},
		{name: "fallback", env: "fallback", set: true, want: agentsmd.StrategyFallback},
		{name: "time-sensitive", env: "time-sensitive", set: true, want: agentsmd.StrategyTimeSensitive},
		{name: "immediately", env: "immediately", set: true, want: agentsmd.StrategyImmediately},
		{name: "unknown falls back to fallback", env: "nonsense", set: true, want: agentsmd.StrategyFallback},
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

			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.webSearchStrategy != tt.want {
				t.Errorf("AGENT_WEB_SEARCH_STRATEGY=%q (set=%v): webSearchStrategy=%q, want %q",
					tt.env, tt.set, cfg.webSearchStrategy, tt.want)
			}
		})
	}
}
