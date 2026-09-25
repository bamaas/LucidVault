package main

import (
	"bytes"
	"encoding/json"
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

// parseLogLines decodes each line of buf (one JSON object per slog record,
// per slog.NewJSONHandler) into a field map, keyed by the handler's own
// attribute names. Used to assert on specific level/msg/attribute fields
// (MINOR-6, test round 2) rather than only on substrings of the raw buffer,
// so a renamed attribute key is caught instead of silently passing because
// its value happens to still appear somewhere in the line.
func parseLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("parsing log line as JSON: %v\nline: %s", err, line)
		}
		records = append(records, rec)
	}
	return records
}

// findLogRecord returns the first record whose "msg" field contains msgSubstr,
// failing the test if none is found.
func findLogRecord(t *testing.T, records []map[string]any, msgSubstr string) map[string]any {
	t.Helper()
	for _, rec := range records {
		if msg, ok := rec["msg"].(string); ok && strings.Contains(msg, msgSubstr) {
			return rec
		}
	}
	t.Fatalf("no log record found with msg containing %q; records: %v", msgSubstr, records)
	return nil
}

// TestUpsertClaudeMD_PlainWrite_NoFallbackWarn verifies MAJOR-1(a): a normal
// write, with usingVaultPathFallback false, must not mention
// CLAUDE_MD_VAULT_PATH at all. It also asserts the expected info-level
// "wrote" log actually fired (MINOR-5, test round 2) -- without this, a
// deletion of the info log entirely would leave the "no fallback warning"
// assertion green for the wrong reason (nothing logged at all, rather than
// the right thing logged).
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
	if !strings.Contains(logged, `"level":"INFO"`) {
		t.Errorf("plain write must be logged at info level; log output: %q", logged)
	}
	if !strings.Contains(logged, path) {
		t.Errorf("write log must name the target path %q; log output: %q", path, logged)
	}
}

// TestUpsertClaudeMD_FallbackWrite_WarnsWithPathAndEnvVar verifies
// MAJOR-1(b) and the ADR-028 requirement that the fallback warning name
// both the emitted path and the CLAUDE_MD_VAULT_PATH env var, at warn level.
// Beyond the substring checks, it decodes the specific JSON record and
// asserts on its structured level/emitted_path/env fields (MINOR-6, test
// round 2), so renaming one of those attribute keys would be caught even
// though the raw substring checks alone would not notice.
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

	records := parseLogLines(t, &buf)
	rec := findLogRecord(t, records, "CLAUDE_MD_VAULT_PATH is unset")
	if rec["level"] != "WARN" {
		t.Errorf("fallback record level = %v, want WARN", rec["level"])
	}
	if rec["emitted_path"] != "/container/vault" {
		t.Errorf("fallback record emitted_path = %v, want %q", rec["emitted_path"], "/container/vault")
	}
	if rec["env"] != "CLAUDE_MD_VAULT_PATH" {
		t.Errorf("fallback record env = %v, want %q", rec["env"], "CLAUDE_MD_VAULT_PATH")
	}
}

// TestUpsertClaudeMD_DivergedBlock_WarnsFileOnly_NoFallbackWarn verifies
// MAJOR-1(c): a StatusSkippedDiverged result must warn naming the file, and
// must NOT also emit the fallback-path warning -- even when
// usingVaultPathFallback is true -- since no write happened for that
// warning to be about. Beyond the substring checks, it decodes the specific
// JSON record and asserts on its structured level/path fields (MINOR-6, test
// round 2), so renaming the "path" attribute key would be caught even though
// the raw substring check alone would not notice.
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

	records := parseLogLines(t, &buf)
	rec := findLogRecord(t, records, "diverged from generated content")
	if rec["level"] != "WARN" {
		t.Errorf("diverged-skip record level = %v, want WARN", rec["level"])
	}
	if rec["path"] != path {
		t.Errorf("diverged-skip record path = %v, want %q", rec["path"], path)
	}
	if _, hasEmittedPath := rec["emitted_path"]; hasEmittedPath {
		t.Errorf("diverged-skip record must not carry emitted_path (that belongs to the fallback warning); record: %v", rec)
	}
	if rec["hint"] != "delete the lucidvault marker block in CLAUDE.md to let LucidVault regenerate it" {
		t.Errorf("diverged-skip record hint = %v, want a remediation hint naming the marker block", rec["hint"])
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

// TestLogClaudeMDUpsertResult_UnexpectedStatus_WarnsInsteadOfAssumingSuccess
// verifies the switch in logClaudeMDUpsertResult switches explicitly on
// status: a Status value it does not recognize must be logged as a warning
// naming the unexpected status, not fall through to the "wrote successfully"
// branch. claudemd.Upsert cannot produce such a value with err == nil today,
// so this calls the logging helper directly with a fabricated out-of-range
// Status to guard against a future claudemd.Status value reaching here
// unhandled.
func TestLogClaudeMDUpsertResult_UnexpectedStatus_WarnsInsteadOfAssumingSuccess(t *testing.T) {
	const fabricatedStatus = claudemd.Status(99)

	var buf bytes.Buffer
	logClaudeMDUpsertResult(fabricatedStatus, nil, "/some/CLAUDE.md", "/host/vault", false, newTestLogger(&buf))

	logged := buf.String()
	if strings.Contains(logged, "CLAUDE.md section upserted") {
		t.Errorf("unexpected status must not be logged as a successful write; log output: %q", logged)
	}
	if strings.Contains(logged, "CLAUDE_MD_VAULT_PATH") {
		t.Errorf("unexpected status must not trigger the fallback warning; log output: %q", logged)
	}

	records := parseLogLines(t, &buf)
	rec := findLogRecord(t, records, "unexpected CLAUDE.md upsert status")
	if rec["level"] != "WARN" {
		t.Errorf("unexpected-status record level = %v, want WARN", rec["level"])
	}
	if rec["path"] != "/some/CLAUDE.md" {
		t.Errorf("unexpected-status record path = %v, want %q", rec["path"], "/some/CLAUDE.md")
	}
	if gotStatus, ok := rec["status"].(float64); !ok || int(gotStatus) != int(fabricatedStatus) {
		t.Errorf("unexpected-status record status = %v, want %d", rec["status"], int(fabricatedStatus))
	}
}
