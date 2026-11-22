package main

import "testing"

// OLLAMA_BASE_URL parsing moved to enrich_config_test.go: it is now a
// deprecated fallback for ENRICH_BASE_URL (ADR-031).

// TestLoadConfig_SupadataLang verifies SUPADATA_LANG: empty by default (no
// language preference), trimmed when set.
func TestLoadConfig_SupadataLang(t *testing.T) {
	t.Setenv("ENRICH_BASE_URL", "http://enrich.test/v1")
	t.Setenv("VAULT_PATH", "/tmp/test")
	t.Setenv("ENRICH_MODEL", "test-model")

	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "unset means no preference", env: "", want: ""},
		{name: "set", env: "en", want: "en"},
		{name: "whitespace trimmed", env: " nl ", want: "nl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SUPADATA_LANG", tt.env)
			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.supadataLang != tt.want {
				t.Errorf("SUPADATA_LANG=%q: supadataLang=%q, want %q", tt.env, cfg.supadataLang, tt.want)
			}
		})
	}
}
