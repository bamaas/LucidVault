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

func TestInit_CreatesTemplates(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, name := range []string{"note.md", "inbox.md"} {
		path := filepath.Join(dir, "templates", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected template %s to exist", name)
		}
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

func TestFileHasContent_Present(t *testing.T) {
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

	if !v.FileHasContent(relPath) {
		t.Error("expected FileHasContent to return true for existing file with content")
	}
}

func TestFileHasContent_Missing(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	if v.FileHasContent("wiki/nonexistent.md") {
		t.Error("expected FileHasContent to return false for missing file")
	}
}

func TestFileHasContent_Empty(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	relPath := filepath.Join("wiki", "empty.md")
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	if v.FileHasContent(relPath) {
		t.Error("expected FileHasContent to return false for empty file")
	}
}

func TestScanWikiDir_Empty(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	paths, err := v.ScanWikiDir()
	if err != nil {
		t.Fatalf("ScanWikiDir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

func TestScanWikiDir_FindsFiles(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create wiki files at different depths
	wikiDir := filepath.Join(dir, "wiki")
	notesDir := filepath.Join(dir, "wiki", "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"foo.md", "bar.md"} {
		if err := os.WriteFile(filepath.Join(wikiDir, f), []byte("# "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(notesDir, "baz.md"), []byte("# baz"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Non-md file should be ignored
	if err := os.WriteFile(filepath.Join(wikiDir, "readme.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := v.ScanWikiDir()
	if err != nil {
		t.Fatalf("ScanWikiDir: %v", err)
	}

	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}

	// Check expected paths (relative to vault base)
	expected := map[string]bool{
		"wiki/foo.md":       false,
		"wiki/bar.md":       false,
		"wiki/notes/baz.md": false,
	}
	for _, p := range paths {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected path: %s", p)
		} else {
			expected[p] = true
		}
	}
	for p, found := range expected {
		if !found {
			t.Errorf("missing expected path: %s", p)
		}
	}
}

func TestScanWikiDir_NoWikiDir(t *testing.T) {
	dir := t.TempDir()
	// Don't init -- no wiki/ directory
	v := New(dir)

	paths, err := v.ScanWikiDir()
	if err != nil {
		t.Fatalf("ScanWikiDir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for missing wiki dir, got %d", len(paths))
	}
}

func TestScanNotesDir_Empty(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatalf("creating notes dir: %v", err)
	}

	paths, err := v.ScanNotesDir()
	if err != nil {
		t.Fatalf("ScanNotesDir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

func TestScanNotesDir_FindsFiles(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	notesDir := filepath.Join(dir, "notes")
	subDir := filepath.Join(notesDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create .md files at different depths.
	for _, f := range []string{"alpha.md", "beta.md"} {
		if err := os.WriteFile(filepath.Join(notesDir, f), []byte("# "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(subDir, "gamma.md"), []byte("# gamma"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Non-md file should be ignored.
	if err := os.WriteFile(filepath.Join(notesDir, "readme.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := v.ScanNotesDir()
	if err != nil {
		t.Fatalf("ScanNotesDir: %v", err)
	}

	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}

	expected := map[string]bool{
		"notes/alpha.md":     false,
		"notes/beta.md":      false,
		"notes/sub/gamma.md": false,
	}
	for _, p := range paths {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected path: %s", p)
		} else {
			expected[p] = true
		}
	}
	for p, found := range expected {
		if !found {
			t.Errorf("missing expected path: %s", p)
		}
	}
}

func TestScanNotesDir_NoNotesDir(t *testing.T) {
	dir := t.TempDir()
	// Don't create notes/ directory.
	v := New(dir)

	paths, err := v.ScanNotesDir()
	if err != nil {
		t.Fatalf("ScanNotesDir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for missing notes dir, got %d", len(paths))
	}
}

func TestFileHasContent_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)

	relPath := filepath.Join("wiki", "blank.md")
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte("  \n\t\n  "), 0o644); err != nil {
		t.Fatal(err)
	}

	if v.FileHasContent(relPath) {
		t.Error("expected FileHasContent to return false for whitespace-only file")
	}
}
