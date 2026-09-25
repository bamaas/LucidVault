package main

import "testing"

// TestLoadConfig_ClaudeMDVaultPath verifies CLAUDE_MD_VAULT_PATH parsing per
// ADR-028 and docs/plans/plan-claudemd-portable-pointer.md: loadConfig reads
// the raw env var into cfg.claudeMDVaultPath (empty string when unset or
// whitespace-only -- the plan's edge case for "treated as unset"), and
// resolveClaudeMDVaultPath implements the precedence the Upsert call site in
// main() relies on: CLAUDE_MD_VAULT_PATH when it carries a non-whitespace
// value, VAULT_PATH (cfg.vaultPath) otherwise. resolveClaudeMDVaultPath also
// reports whether it fell back to VAULT_PATH, which main() uses verbatim as
// usingVaultPathFallback -- so this table is the only place that condition is
// exercised (see MINOR-1, test round 2).
func TestLoadConfig_ClaudeMDVaultPath(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "test")
	t.Setenv("VAULT_PATH", "/container/vault")

	tests := []struct {
		name         string
		env          string
		set          bool
		wantField    string
		wantResolved string
		wantFallback bool
	}{
		{name: "unset falls back to VAULT_PATH", set: false, wantField: "", wantResolved: "/container/vault", wantFallback: true},
		{name: "empty falls back to VAULT_PATH", env: "", set: true, wantField: "", wantResolved: "/container/vault", wantFallback: true},
		{name: "whitespace only falls back to VAULT_PATH", env: "   ", set: true, wantField: "", wantResolved: "/container/vault", wantFallback: true},
		{name: "set overrides VAULT_PATH", env: "/Users/bas/lucid-vault", set: true, wantField: "/Users/bas/lucid-vault", wantResolved: "/Users/bas/lucid-vault", wantFallback: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("CLAUDE_MD_VAULT_PATH", tt.env)
			} else {
				// t.Setenv registers cleanup; set empty then unset to exercise
				// the "not present" path (see unsetenv, defined alongside the
				// other cmd config tests in mcp_config_test.go).
				t.Setenv("CLAUDE_MD_VAULT_PATH", "")
				if err := unsetenv(t, "CLAUDE_MD_VAULT_PATH"); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}

			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.claudeMDVaultPath != tt.wantField {
				t.Errorf("CLAUDE_MD_VAULT_PATH=%q (set=%v): claudeMDVaultPath=%q, want %q",
					tt.env, tt.set, cfg.claudeMDVaultPath, tt.wantField)
			}
			gotResolved, gotFallback := resolveClaudeMDVaultPath(cfg)
			if gotResolved != tt.wantResolved {
				t.Errorf("CLAUDE_MD_VAULT_PATH=%q (set=%v): resolveClaudeMDVaultPath path=%q, want %q",
					tt.env, tt.set, gotResolved, tt.wantResolved)
			}
			if gotFallback != tt.wantFallback {
				t.Errorf("CLAUDE_MD_VAULT_PATH=%q (set=%v): resolveClaudeMDVaultPath usingFallback=%v, want %v",
					tt.env, tt.set, gotFallback, tt.wantFallback)
			}
		})
	}
}
