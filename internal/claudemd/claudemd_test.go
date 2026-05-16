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
