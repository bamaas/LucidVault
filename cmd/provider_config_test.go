package main

import "testing"

// TestLoadConfig_OllamaBaseURL verifies OLLAMA_BASE_URL: empty by default (the
// enricher keeps Ollama Cloud), trimmed of whitespace and a trailing slash so
// the client can append /api/chat.
func TestLoadConfig_OllamaBaseURL(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "test")
	t.Setenv("VAULT_PATH", "/tmp/test")

	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "unset keeps default", env: "", want: ""},
		{name: "plain url", env: "http://ollama:11434", want: "http://ollama:11434"},
		{name: "trailing slash and space trimmed", env: " http://ollama:11434/ ", want: "http://ollama:11434"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OLLAMA_BASE_URL", tt.env)
			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.ollamaBaseURL != tt.want {
				t.Errorf("OLLAMA_BASE_URL=%q: ollamaBaseURL=%q, want %q", tt.env, cfg.ollamaBaseURL, tt.want)
			}
		})
	}
}

// TestLoadConfig_SupadataLang verifies SUPADATA_LANG: empty by default (no
// language preference), trimmed when set.
func TestLoadConfig_SupadataLang(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "test")
	t.Setenv("VAULT_PATH", "/tmp/test")

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
