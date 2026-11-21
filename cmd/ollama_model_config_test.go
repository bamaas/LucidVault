package main

import (
	"strings"
	"testing"
)

// TestLoadConfig_OllamaModelRequired verifies ADR-030 /
// docs/plans/plan-require-ollama-model.md: LucidVault ships no default
// OLLAMA_MODEL. loadConfig must error when it is unset or whitespace-only,
// and must trim surrounding whitespace from a value it accepts.
func TestLoadConfig_OllamaModelRequired(t *testing.T) {
	tests := []struct {
		name          string
		env           string
		set           bool
		wantErr       bool
		wantErrSubstr string
		wantModel     string
	}{
		{
			name:          "unset is an error",
			set:           false,
			wantErr:       true,
			wantErrSubstr: "OLLAMA_MODEL",
		},
		{
			name:          "whitespace-only is an error",
			env:           "   ",
			set:           true,
			wantErr:       true,
			wantErrSubstr: "OLLAMA_MODEL",
		},
		{
			name:      "set value is trimmed and used",
			env:       " gpt-oss:120b ",
			set:       true,
			wantErr:   false,
			wantModel: "gpt-oss:120b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OLLAMA_API_KEY", "test")
			t.Setenv("VAULT_PATH", "/tmp/test")

			if tt.set {
				t.Setenv("OLLAMA_MODEL", tt.env)
			} else {
				// t.Setenv always sets a value, so to exercise the "variable not
				// present at all" path we set then unset it (see unsetenv in
				// mcp_config_test.go).
				t.Setenv("OLLAMA_MODEL", "")
				if err := unsetenv(t, "OLLAMA_MODEL"); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}

			cfg, err := loadConfig(false, false)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("OLLAMA_MODEL=%q (set=%v): expected error, got nil", tt.env, tt.set)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("OLLAMA_MODEL=%q (set=%v): error %q does not mention %q",
						tt.env, tt.set, err.Error(), tt.wantErrSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("OLLAMA_MODEL=%q (set=%v): unexpected error: %v", tt.env, tt.set, err)
			}
			if cfg.ollamaModel != tt.wantModel {
				t.Errorf("OLLAMA_MODEL=%q (set=%v): ollamaModel=%q, want %q",
					tt.env, tt.set, cfg.ollamaModel, tt.wantModel)
			}
		})
	}
}
