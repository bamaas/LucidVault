package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// Enrichment Backend configuration (ADR-031,
// docs/plans/plan-openai-compatible-enrichment.md section 3).
//
// The loaded values are expected on config.enrichBaseURL, config.enrichModel
// and config.enrichAPIKey (neutral names replacing config.ollama*).

const baseURLRequiredMsg = "ENRICH_BASE_URL is required: the base URL of an OpenAI-compatible API " +
	"(Ollama Cloud: https://ollama.com/v1, self-hosted Ollama: http://<host>:11434/v1)"

const modelRequiredMsg = "ENRICH_MODEL is required"

const baseURLInvalidMsg = "ENRICH_BASE_URL is not a valid http(s) URL"

// enrichEnvVars are the six variables this feature reads. Every test controls
// all of them so values in the developer's shell cannot leak in.
var enrichEnvVars = []string{
	"ENRICH_BASE_URL", "ENRICH_MODEL", "ENRICH_API_KEY",
	"OLLAMA_BASE_URL", "OLLAMA_MODEL", "OLLAMA_API_KEY",
}

var legacyEnrichEnvVars = []string{"OLLAMA_BASE_URL", "OLLAMA_MODEL", "OLLAMA_API_KEY"}

// setEnrichEnv sets the variables in env and removes the other enrichEnvVars
// from the environment entirely.
func setEnrichEnv(t *testing.T, env map[string]string) {
	t.Helper()
	t.Setenv("VAULT_PATH", "/tmp/test")
	for _, key := range enrichEnvVars {
		if v, ok := env[key]; ok {
			t.Setenv(key, v)
			continue
		}
		t.Setenv(key, "")
		if err := unsetenv(t, key); err != nil {
			t.Fatalf("unsetenv %s: %v", key, err)
		}
	}
}

// captureLogs routes the default slog logger to a buffer at the given level
// for the rest of the test.
func captureLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func deprecationNotice(legacyVar string) string {
	return legacyVar + " is deprecated"
}

func TestLoadConfig_EnrichRequiredErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "base URL unset",
			env:     map[string]string{"ENRICH_MODEL": "m"},
			wantErr: baseURLRequiredMsg,
		},
		{
			name:    "base URL empty",
			env:     map[string]string{"ENRICH_BASE_URL": "", "ENRICH_MODEL": "m"},
			wantErr: baseURLRequiredMsg,
		},
		{
			name:    "base URL whitespace-only",
			env:     map[string]string{"ENRICH_BASE_URL": "   ", "ENRICH_MODEL": "m"},
			wantErr: baseURLRequiredMsg,
		},
		{
			name:    "base URL and legacy base URL both whitespace-only",
			env:     map[string]string{"ENRICH_BASE_URL": " ", "OLLAMA_BASE_URL": "\t ", "ENRICH_MODEL": "m"},
			wantErr: baseURLRequiredMsg,
		},
		{
			name: "only legacy key and model set (intended break: no base URL)",
			env: map[string]string{
				"OLLAMA_API_KEY": "legacy-key",
				"OLLAMA_MODEL":   "legacy-model",
			},
			wantErr: baseURLRequiredMsg,
		},
		{
			name:    "model unset",
			env:     map[string]string{"ENRICH_BASE_URL": "http://enrich.test/v1"},
			wantErr: modelRequiredMsg,
		},
		{
			name:    "model empty",
			env:     map[string]string{"ENRICH_BASE_URL": "http://enrich.test/v1", "ENRICH_MODEL": ""},
			wantErr: modelRequiredMsg,
		},
		{
			name:    "model whitespace-only",
			env:     map[string]string{"ENRICH_BASE_URL": "http://enrich.test/v1", "ENRICH_MODEL": "   "},
			wantErr: modelRequiredMsg,
		},
		{
			name: "model and legacy model both whitespace-only",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://enrich.test/v1",
				"ENRICH_MODEL":    " ",
				"OLLAMA_MODEL":    "  ",
			},
			wantErr: modelRequiredMsg,
		},
		{
			// A value that fails to parse, or parses but is missing a
			// scheme/host, must fail fast at startup with a clear error
			// instead of making every request fail later at
			// http.NewRequestWithContext (which callAPI does not classify
			// as ErrBackendUnavailable, so it would retry and write stub
			// pages — the exact outcome ADR-031 rejects).
			name:    "base URL has no scheme",
			env:     map[string]string{"ENRICH_BASE_URL": "enrich.test/v1", "ENRICH_MODEL": "m"},
			wantErr: baseURLInvalidMsg,
		},
		{
			name:    "base URL has an invalid scheme",
			env:     map[string]string{"ENRICH_BASE_URL": "ftp://enrich.test/v1", "ENRICH_MODEL": "m"},
			wantErr: baseURLInvalidMsg,
		},
		{
			name:    "base URL fails to parse (space in host)",
			env:     map[string]string{"ENRICH_BASE_URL": "https://ho st/v1", "ENRICH_MODEL": "m"},
			wantErr: baseURLInvalidMsg,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnrichEnv(t, tt.env)

			cfg, err := loadConfig(false, false)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (cfg=%+v)", tt.wantErr, cfg)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestLoadConfig_InvalidBaseURLNeverLeaksPassword guards the "never echo
// the raw value" rule for the malformed-URL error: ENRICH_BASE_URL can carry
// a password (https://user:pass@host/v1), and a user who mistypes it must
// not get that password printed back in the startup error.
func TestLoadConfig_InvalidBaseURLNeverLeaksPassword(t *testing.T) {
	const password = "s3cret-password"
	setEnrichEnv(t, map[string]string{
		"ENRICH_BASE_URL": "https://user:" + password + "@ho st/v1",
		"ENRICH_MODEL":    "m",
	})

	_, err := loadConfig(false, false)
	if err == nil {
		t.Fatal("expected an error for a malformed base URL, got nil")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("error leaks the URL's password: %v", err)
	}
	if !strings.Contains(err.Error(), baseURLInvalidMsg) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), baseURLInvalidMsg)
	}
}

func TestLoadConfig_EnrichSettings(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantBaseURL string
		wantModel   string
		wantAPIKey  string
		// wantWarn lists legacy variables that must produce a deprecation WARN.
		wantWarn []string
		// wantSilent lists legacy variables that must NOT produce one.
		wantSilent []string
	}{
		{
			name: "new variables only, no key (keyless endpoint)",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://ollama:11434/v1",
				"ENRICH_MODEL":    "llama3.2",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "llama3.2",
			wantAPIKey:  "",
			wantSilent:  legacyEnrichEnvVars,
		},
		{
			name: "new variables with key",
			env: map[string]string{
				"ENRICH_BASE_URL": "https://ollama.com/v1",
				"ENRICH_MODEL":    "gpt-oss:120b",
				"ENRICH_API_KEY":  "sk-new",
			},
			wantBaseURL: "https://ollama.com/v1",
			wantModel:   "gpt-oss:120b",
			wantAPIKey:  "sk-new",
			wantSilent:  legacyEnrichEnvVars,
		},
		{
			name: "base URL trailing slash and whitespace trimmed, model trimmed",
			env: map[string]string{
				"ENRICH_BASE_URL": "  https://ollama.com/v1/  ",
				"ENRICH_MODEL":    " gpt-oss:120b ",
			},
			wantBaseURL: "https://ollama.com/v1",
			wantModel:   "gpt-oss:120b",
			wantSilent:  legacyEnrichEnvVars,
		},
		{
			name: "new base URL gets no /v1 appended",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://llm-gateway.local:4000",
				"ENRICH_MODEL":    "m",
			},
			wantBaseURL: "http://llm-gateway.local:4000",
			wantModel:   "m",
			wantSilent:  legacyEnrichEnvVars,
		},
		{
			name: "whitespace-only API key means no key",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://ollama:11434/v1",
				"ENRICH_MODEL":    "m",
				"ENRICH_API_KEY":  "   ",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "m",
			wantAPIKey:  "",
			wantSilent:  legacyEnrichEnvVars,
		},
		{
			name: "new variables win over different legacy values",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://new-host:11434/v1",
				"ENRICH_MODEL":    "new-model",
				"ENRICH_API_KEY":  "new-key",
				"OLLAMA_BASE_URL": "http://old-host:11434",
				"OLLAMA_MODEL":    "old-model",
				"OLLAMA_API_KEY":  "old-key",
			},
			wantBaseURL: "http://new-host:11434/v1",
			wantModel:   "new-model",
			wantAPIKey:  "new-key",
			wantSilent:  legacyEnrichEnvVars,
		},
		{
			name: "new base URL without /v1 wins over legacy base URL, no /v1 appended",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://gw:4000",
				"ENRICH_MODEL":    "m",
				"OLLAMA_BASE_URL": "http://old:11434",
			},
			wantBaseURL: "http://gw:4000",
			wantModel:   "m",
			wantSilent:  []string{"OLLAMA_BASE_URL"},
		},
		{
			name: "whitespace-only legacy variables are unset and do not warn",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://ollama:11434/v1",
				"ENRICH_MODEL":    "m",
				"OLLAMA_BASE_URL": "  ",
				"OLLAMA_MODEL":    " ",
				"OLLAMA_API_KEY":  "\t",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "m",
			wantAPIKey:  "",
			wantSilent:  legacyEnrichEnvVars,
		},
		{
			name: "legacy model fallback (trimmed)",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://ollama:11434/v1",
				"OLLAMA_MODEL":    " legacy-model ",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "legacy-model",
			wantWarn:    []string{"OLLAMA_MODEL"},
			wantSilent:  []string{"OLLAMA_BASE_URL", "OLLAMA_API_KEY"},
		},
		{
			name: "whitespace-only new model falls back to legacy model",
			env: map[string]string{
				"ENRICH_BASE_URL": "http://ollama:11434/v1",
				"ENRICH_MODEL":    "   ",
				"OLLAMA_MODEL":    "legacy-model",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "legacy-model",
			wantWarn:    []string{"OLLAMA_MODEL"},
		},
		{
			name: "legacy API key fallback",
			env: map[string]string{
				"ENRICH_BASE_URL": "https://ollama.com/v1",
				"ENRICH_MODEL":    "m",
				"OLLAMA_API_KEY":  "legacy-key",
			},
			wantBaseURL: "https://ollama.com/v1",
			wantModel:   "m",
			wantAPIKey:  "legacy-key",
			wantWarn:    []string{"OLLAMA_API_KEY"},
			wantSilent:  []string{"OLLAMA_BASE_URL", "OLLAMA_MODEL"},
		},
		{
			name: "whitespace-only new API key falls back to legacy key",
			env: map[string]string{
				"ENRICH_BASE_URL": "https://ollama.com/v1",
				"ENRICH_MODEL":    "m",
				"ENRICH_API_KEY":  "  ",
				"OLLAMA_API_KEY":  "legacy-key",
			},
			wantBaseURL: "https://ollama.com/v1",
			wantModel:   "m",
			wantAPIKey:  "legacy-key",
			wantWarn:    []string{"OLLAMA_API_KEY"},
		},
		{
			name: "legacy base URL gets /v1 appended",
			env: map[string]string{
				"OLLAMA_BASE_URL": "http://ollama:11434",
				"ENRICH_MODEL":    "m",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "m",
			wantWarn:    []string{"OLLAMA_BASE_URL"},
			wantSilent:  []string{"OLLAMA_MODEL", "OLLAMA_API_KEY"},
		},
		{
			name: "legacy base URL with trailing slash gets /v1 appended once",
			env: map[string]string{
				"OLLAMA_BASE_URL": "http://ollama:11434/",
				"ENRICH_MODEL":    "m",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "m",
			wantWarn:    []string{"OLLAMA_BASE_URL"},
		},
		{
			name: "legacy base URL with whitespace and trailing slash",
			env: map[string]string{
				"OLLAMA_BASE_URL": "  http://ollama:11434/  ",
				"ENRICH_MODEL":    "m",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "m",
			wantWarn:    []string{"OLLAMA_BASE_URL"},
		},
		{
			name: "legacy base URL already ending in /v1 is not doubled",
			env: map[string]string{
				"OLLAMA_BASE_URL": "https://ollama.com/v1",
				"ENRICH_MODEL":    "m",
			},
			wantBaseURL: "https://ollama.com/v1",
			wantModel:   "m",
			wantWarn:    []string{"OLLAMA_BASE_URL"},
		},
		{
			name: "legacy base URL ending in /v1/ is not doubled",
			env: map[string]string{
				"OLLAMA_BASE_URL": "https://ollama.com/v1/",
				"ENRICH_MODEL":    "m",
			},
			wantBaseURL: "https://ollama.com/v1",
			wantModel:   "m",
			wantWarn:    []string{"OLLAMA_BASE_URL"},
		},
		{
			name: "whitespace-only new base URL falls back to legacy base URL",
			env: map[string]string{
				"ENRICH_BASE_URL": "   ",
				"OLLAMA_BASE_URL": "http://ollama:11434",
				"ENRICH_MODEL":    "m",
			},
			wantBaseURL: "http://ollama:11434/v1",
			wantModel:   "m",
			wantWarn:    []string{"OLLAMA_BASE_URL"},
		},
		{
			name: "legacy-only install keeps starting with one warning per variable",
			env: map[string]string{
				"OLLAMA_BASE_URL": "https://ollama.com",
				"OLLAMA_MODEL":    "gpt-oss:120b",
				"OLLAMA_API_KEY":  "legacy-key",
			},
			wantBaseURL: "https://ollama.com/v1",
			wantModel:   "gpt-oss:120b",
			wantAPIKey:  "legacy-key",
			wantWarn:    legacyEnrichEnvVars,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnrichEnv(t, tt.env)
			logs := captureLogs(t, slog.LevelWarn)

			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: unexpected error: %v", err)
			}

			if cfg.enrichBaseURL != tt.wantBaseURL {
				t.Errorf("enrichBaseURL = %q, want %q", cfg.enrichBaseURL, tt.wantBaseURL)
			}
			if cfg.enrichModel != tt.wantModel {
				t.Errorf("enrichModel = %q, want %q", cfg.enrichModel, tt.wantModel)
			}
			if cfg.enrichAPIKey != tt.wantAPIKey {
				t.Errorf("enrichAPIKey = %q, want %q", cfg.enrichAPIKey, tt.wantAPIKey)
			}

			logged := logs.String()
			for _, legacy := range tt.wantWarn {
				notice := deprecationNotice(legacy)
				if !strings.Contains(logged, notice) {
					t.Errorf("expected WARN %q; log output: %q", notice, logged)
				}
				if got := strings.Count(logged, notice); got > 1 {
					t.Errorf("expected one WARN %q, got %d; log output: %q", notice, got, logged)
				}
			}
			for _, legacy := range tt.wantSilent {
				notice := deprecationNotice(legacy)
				if strings.Contains(logged, notice) {
					t.Errorf("unexpected WARN %q; log output: %q", notice, logged)
				}
			}
		})
	}
}

// TestLoadConfig_EnrichDeprecationMessageNamesReplacement pins the full
// deprecation wording from the plan for each legacy variable.
func TestLoadConfig_EnrichDeprecationMessageNamesReplacement(t *testing.T) {
	setEnrichEnv(t, map[string]string{
		"OLLAMA_BASE_URL": "http://ollama:11434",
		"OLLAMA_MODEL":    "legacy-model",
		"OLLAMA_API_KEY":  "legacy-key",
	})
	logs := captureLogs(t, slog.LevelWarn)

	if _, err := loadConfig(false, false); err != nil {
		t.Fatalf("loadConfig: unexpected error: %v", err)
	}

	logged := logs.String()
	for _, want := range []string{
		"OLLAMA_BASE_URL is deprecated, use ENRICH_BASE_URL",
		"OLLAMA_MODEL is deprecated, use ENRICH_MODEL",
		"OLLAMA_API_KEY is deprecated, use ENRICH_API_KEY",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("expected WARN %q; log output: %q", want, logged)
		}
	}
	if !strings.Contains(logged, "level=WARN") {
		t.Errorf("deprecation notices must be logged at WARN; log output: %q", logged)
	}
}

func TestLoadConfig_EnrichNeverLogsAPIKey(t *testing.T) {
	const secret = "sk-secret-never-log-7731"

	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "new key",
			env: map[string]string{
				"ENRICH_BASE_URL": "https://ollama.com/v1",
				"ENRICH_MODEL":    "m",
				"ENRICH_API_KEY":  secret,
			},
		},
		{
			name: "legacy key",
			env: map[string]string{
				"OLLAMA_BASE_URL": "https://ollama.com",
				"OLLAMA_MODEL":    "m",
				"OLLAMA_API_KEY":  secret,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnrichEnv(t, tt.env)
			logs := captureLogs(t, slog.LevelDebug)

			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: unexpected error: %v", err)
			}
			if cfg.enrichAPIKey != secret {
				t.Errorf("enrichAPIKey = %q, want the configured key", cfg.enrichAPIKey)
			}
			if strings.Contains(logs.String(), secret) {
				t.Errorf("loadConfig logged the API key; log output: %q", logs.String())
			}
		})
	}
}
