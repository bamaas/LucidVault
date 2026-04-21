package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertClaudeMD_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := upsertClaudeMD(path, "/data/vault"); err != nil {
		t.Fatalf("upsertClaudeMD: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, claudeMDStartMarker)
	assertContains(t, content, claudeMDEndMarker)
	assertContains(t, content, "/data/vault")
	assertContains(t, content, "## LucidVault Knowledge Base")
}

func TestUpsertClaudeMD_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	os.WriteFile(path, []byte("# My Config\n\nSome existing content.\n"), 0644)

	if err := upsertClaudeMD(path, "/vault"); err != nil {
		t.Fatalf("upsertClaudeMD: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, "# My Config")
	assertContains(t, content, "Some existing content.")
	assertContains(t, content, claudeMDStartMarker)
	assertContains(t, content, claudeMDEndMarker)
}

func TestUpsertClaudeMD_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	old := "# Config\n\n" + claudeMDStartMarker + "\nold content\n" + claudeMDEndMarker + "\n\n# Footer\n"
	os.WriteFile(path, []byte(old), 0644)

	if err := upsertClaudeMD(path, "/new/vault"); err != nil {
		t.Fatalf("upsertClaudeMD: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, "# Config")
	assertContains(t, content, "# Footer")
	assertContains(t, content, "/new/vault")
	assertNotContains(t, content, "old content")
}

func TestUpsertClaudeMD_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	for range 3 {
		if err := upsertClaudeMD(path, "/vault"); err != nil {
			t.Fatalf("upsertClaudeMD: %v", err)
		}
	}

	content := readFile(t, path)
	count := strings.Count(content, claudeMDStartMarker)
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
