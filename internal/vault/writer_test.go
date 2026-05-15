package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists_Present(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	// Create a file with content
	relPath := filepath.Join("wiki", "test.md")
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte("# Hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !v.FileExists(relPath) {
		t.Error("expected FileExists to return true for existing file with content")
	}
}

func TestFileExists_Missing(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	if v.FileExists("wiki/nonexistent.md") {
		t.Error("expected FileExists to return false for missing file")
	}
}

func TestFileExists_Empty(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	relPath := filepath.Join("wiki", "empty.md")
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	if v.FileExists(relPath) {
		t.Error("expected FileExists to return false for empty file")
	}
}

func TestFileExists_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	relPath := filepath.Join("wiki", "blank.md")
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte("  \n\t\n  "), 0o644); err != nil {
		t.Fatal(err)
	}

	if v.FileExists(relPath) {
		t.Error("expected FileExists to return false for whitespace-only file")
	}
}
