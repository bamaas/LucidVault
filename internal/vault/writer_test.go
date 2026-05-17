package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initIndex(t *testing.T, dir string) {
	t.Helper()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
}

func TestRemoveFromIndex(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	initIndex(t, dir)

	if err := v.UpdateIndex("my-slug", "My Title", nil); err != nil {
		t.Fatalf("UpdateIndex: %v", err)
	}

	if err := v.RemoveFromIndex("my-slug"); err != nil {
		t.Fatalf("RemoveFromIndex: %v", err)
	}

	content, err := v.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if strings.Contains(content, "[[my-slug]]") {
		t.Error("expected [[my-slug]] to be removed from index")
	}
}

func TestRemoveFromIndex_NotFound(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	initIndex(t, dir)

	// Should return nil even when slug does not exist
	if err := v.RemoveFromIndex("nonexistent-slug"); err != nil {
		t.Fatalf("RemoveFromIndex returned unexpected error: %v", err)
	}
}

func TestRemoveFromIndex_PreservesOthers(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	initIndex(t, dir)

	if err := v.UpdateIndex("slug-one", "Title One", nil); err != nil {
		t.Fatalf("UpdateIndex slug-one: %v", err)
	}
	if err := v.UpdateIndex("slug-two", "Title Two", nil); err != nil {
		t.Fatalf("UpdateIndex slug-two: %v", err)
	}

	if err := v.RemoveFromIndex("slug-one"); err != nil {
		t.Fatalf("RemoveFromIndex: %v", err)
	}

	content, err := v.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if strings.Contains(content, "[[slug-one]]") {
		t.Error("expected [[slug-one]] to be removed from index")
	}
	if !strings.Contains(content, "[[slug-two]]") {
		t.Error("expected [[slug-two]] to remain in index")
	}
}

func TestUpdateIndex_WithNotesPrefix(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	initIndex(t, dir)

	slug := "notes/my-note"
	if err := v.UpdateIndex(slug, "My Note", []string{"personal"}); err != nil {
		t.Fatalf("UpdateIndex: %v", err)
	}

	content, err := v.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	if !strings.Contains(content, "[[notes/my-note]]") {
		t.Errorf("expected [[notes/my-note]] in index, got:\n%s", content)
	}

	// Calling again must be idempotent (no duplicate)
	if err := v.UpdateIndex(slug, "My Note", []string{"personal"}); err != nil {
		t.Fatalf("UpdateIndex (second call): %v", err)
	}
	content2, err := v.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex (second call): %v", err)
	}
	count := strings.Count(content2, "[[notes/my-note]]")
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of [[notes/my-note]], got %d", count)
	}
}

func TestDeleteFile(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	// Create a file to delete
	relPath := filepath.Join("wiki", "doomed.md")
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte("# Goodbye"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := v.DeleteFile(relPath); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, relPath)); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestDeleteFile_NonExistent(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	// Should not error for missing file
	if err := v.DeleteFile("wiki/nonexistent.md"); err != nil {
		t.Fatalf("DeleteFile on missing file: %v", err)
	}
}

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
