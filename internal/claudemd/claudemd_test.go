package claudemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsert_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := Upsert(path, "/data/vault"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, StartMarker)
	assertContains(t, content, EndMarker)
	assertContains(t, content, "/data/vault")
	assertContains(t, content, "## LucidVault Knowledge Base")
}

func TestUpsert_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := os.WriteFile(path, []byte("# My Config\n\nSome existing content.\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := Upsert(path, "/vault"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, "# My Config")
	assertContains(t, content, "Some existing content.")
	assertContains(t, content, StartMarker)
	assertContains(t, content, EndMarker)
}

func TestUpsert_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	old := "# Config\n\n" + StartMarker + "\nold content\n" + EndMarker + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := Upsert(path, "/new/vault"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, "# Config")
	assertContains(t, content, "# Footer")
	assertContains(t, content, "/new/vault")
	assertNotContains(t, content, "old content")
}

func TestUpsert_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	for range 3 {
		if err := Upsert(path, "/vault"); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	content := readFile(t, path)
	count := strings.Count(content, StartMarker)
	if count != 1 {
		t.Errorf("expected 1 start marker, got %d", count)
	}
}

// TestUpsert_PointerForm verifies the injected section is the minimal pointer
// (ADR-025): it carries the vault's absolute path and tells the agent to read
// AGENTS.md and follow it -- and it no longer duplicates the retrieval strategy
// steps or the per-directory file legend that AGENTS.md now owns.
func TestUpsert_PointerForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := Upsert(path, "/data/vault"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	content := readFile(t, path)

	// Markers preserved.
	assertContains(t, content, StartMarker)
	assertContains(t, content, EndMarker)

	// The one thing AGENTS.md cannot self-supply: the absolute vault path.
	assertContains(t, content, "/data/vault")

	// The pointer must direct the agent to AGENTS.md and to follow it.
	assertContains(t, content, "AGENTS.md")
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "follow") {
		t.Errorf("pointer must instruct the agent to follow AGENTS.md; got:\n%s", content)
	}

	// The duplicated retrieval strategy and file legend must be gone -- they now
	// live solely in AGENTS.md (no drift).
	assertNotContains(t, content, "Retrieval Strategy")
	assertNotContains(t, content, "Grep index.md")
	assertNotContains(t, content, "Vault Structure")
	assertNotContains(t, content, "LLM-enriched summaries")
	assertNotContains(t, content, "Fetch a URL")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func assertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("expected content to contain %q", substr)
	}
}

func assertNotContains(t *testing.T, content, substr string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Errorf("expected content NOT to contain %q", substr)
	}
}
