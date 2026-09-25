package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lucidvault/internal/claudemd"
)

// newTestLogger returns an *slog.Logger backed by buf so a test can assert
// on exactly what upsertClaudeMD logs, without touching the package-level
// slog.Default() (which other tests in this package also mutate).
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestUpsertClaudeMD_PlainWrite_NoFallbackWarn verifies MAJOR-1(a): a normal
// write, with usingVaultPathFallback false, must not mention
// CLAUDE_MD_VAULT_PATH at all.
func TestUpsertClaudeMD_PlainWrite_NoFallbackWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	upsertClaudeMD(path, "/host/vault", false, newTestLogger(&buf))

	content := readFile(t, path)
	assertContains(t, content, "/host/vault")

	logged := buf.String()
	if strings.Contains(logged, "CLAUDE_MD_VAULT_PATH") {
		t.Errorf("plain write must not mention CLAUDE_MD_VAULT_PATH; log output: %q", logged)
	}
}

// TestUpsertClaudeMD_FallbackWrite_WarnsWithPathAndEnvVar verifies
// MAJOR-1(b) and the ADR-028 requirement that the fallback warning name
// both the emitted path and the CLAUDE_MD_VAULT_PATH env var, at warn level.
func TestUpsertClaudeMD_FallbackWrite_WarnsWithPathAndEnvVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	upsertClaudeMD(path, "/container/vault", true, newTestLogger(&buf))

	logged := buf.String()
	if !strings.Contains(logged, "CLAUDE_MD_VAULT_PATH") {
		t.Errorf("fallback write must name the CLAUDE_MD_VAULT_PATH env var; log output: %q", logged)
	}
	if !strings.Contains(logged, "/container/vault") {
		t.Errorf("fallback write must name the emitted path; log output: %q", logged)
	}
	if !strings.Contains(logged, `"level":"WARN"`) {
		t.Errorf("fallback notice must be logged at warn level; log output: %q", logged)
	}
}

// TestUpsertClaudeMD_DivergedBlock_WarnsFileOnly_NoFallbackWarn verifies
// MAJOR-1(c): a StatusSkippedDiverged result must warn naming the file, and
// must NOT also emit the fallback-path warning -- even when
// usingVaultPathFallback is true -- since no write happened for that
// warning to be about.
func TestUpsertClaudeMD_DivergedBlock_WarnsFileOnly_NoFallbackWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	diverged := "# Config\n\n" + claudemd.StartMarker + "\nuser-written content\n" + claudemd.EndMarker + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(diverged), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	upsertClaudeMD(path, "/container/vault", true, newTestLogger(&buf))

	content := readFile(t, path)
	if content != diverged {
		t.Errorf("diverged block must be left byte-identical\ngot:\n%s\nwant:\n%s", content, diverged)
	}

	logged := buf.String()
	if !strings.Contains(logged, path) {
		t.Errorf("diverged-skip warning must name the file path %q; log output: %q", path, logged)
	}
	if strings.Contains(logged, "CLAUDE_MD_VAULT_PATH") {
		t.Errorf("diverged skip must not also emit the fallback-path warning; log output: %q", logged)
	}
	if !strings.Contains(logged, `"level":"WARN"`) {
		t.Errorf("diverged skip must be logged at warn level; log output: %q", logged)
	}
}

// TestUpsertClaudeMD_ErrorResult_LogsWarningNoPanic verifies MAJOR-1(d): a
// write failure (a read-only target) must be logged as a warning naming the
// path, and must not panic or abort the caller.
func TestUpsertClaudeMD_ErrorResult_LogsWarningNoPanic(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions do not block writes")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# Config\n"), 0o444); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	var buf bytes.Buffer
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("upsertClaudeMD panicked on a write failure: %v", r)
			}
		}()
		upsertClaudeMD(path, "/host/vault", false, newTestLogger(&buf))
	}()

	logged := buf.String()
	if !strings.Contains(logged, path) {
		t.Errorf("error result must be logged naming the file path %q; log output: %q", path, logged)
	}
	if !strings.Contains(logged, `"level":"WARN"`) {
		t.Errorf("write failure must be logged at warn level; log output: %q", logged)
	}
}

// TestUpsertClaudeMD_EmitsExactClaudeMDVaultPath verifies acceptance
// criterion 9 and MAJOR-2 end-to-end: the string actually written into the
// file via the real call site is the claudeMDVaultPath passed in, not some
// other (e.g. VAULT_PATH-shaped) value.
func TestUpsertClaudeMD_EmitsExactClaudeMDVaultPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	const distinctClaudeMDVaultPath = "/Users/bas/lucid-vault-host-facing"
	const differentVaultPathStyleValue = "/vault"

	upsertClaudeMD(path, distinctClaudeMDVaultPath, false, newTestLogger(&bytes.Buffer{}))

	content := readFile(t, path)
	assertContains(t, content, distinctClaudeMDVaultPath)
	assertNotContains(t, content, differentVaultPathStyleValue)
}

// TestUpsertClaudeMD_TargetDoesNotExist_NoOp verifies the best-effort
// "target file does not exist" edge case the plan calls out: upsertClaudeMD
// must not create the file and must not log anything.
func TestUpsertClaudeMD_TargetDoesNotExist_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	var buf bytes.Buffer
	upsertClaudeMD(path, "/host/vault", false, newTestLogger(&buf))

	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected upsertClaudeMD not to create %s when it did not already exist", path)
	}
	if logged := buf.String(); logged != "" {
		t.Errorf("expected no log output when the target file does not exist; log output: %q", logged)
	}
}
