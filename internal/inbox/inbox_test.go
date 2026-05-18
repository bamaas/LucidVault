package inbox

import (
	"os"
	"path/filepath"
	"testing"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestScan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "inbox"))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestScan_NoInboxDir(t *testing.T) {
	dir := t.TempDir()

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestScan_URLOnly(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)
	mustWriteFile(t, filepath.Join(inboxDir, "article.md"), []byte("https://example.com/article\n"))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].URL != "https://example.com/article" {
		t.Errorf("expected URL https://example.com/article, got %q", items[0].URL)
	}
	if items[0].Title != "article" {
		t.Errorf("expected title 'article', got %q", items[0].Title)
	}
	if len(items[0].Tags) != 0 {
		t.Errorf("expected no tags, got %v", items[0].Tags)
	}
}

func TestScan_WithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)

	content := `---
title: "My Article"
tags:
  - golang
  - testing
---
https://example.com/article
`
	mustWriteFile(t, filepath.Join(inboxDir, "my-article.md"), []byte(content))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].URL != "https://example.com/article" {
		t.Errorf("expected URL https://example.com/article, got %q", items[0].URL)
	}
	if items[0].Title != "My Article" {
		t.Errorf("expected title 'My Article', got %q", items[0].Title)
	}
	if len(items[0].Tags) != 2 || items[0].Tags[0] != "golang" || items[0].Tags[1] != "testing" {
		t.Errorf("expected tags [golang, testing], got %v", items[0].Tags)
	}
}

func TestScan_InlineTagsArray(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)

	content := `---
title: "Test"
tags: [go, web]
---
https://example.com/test
`
	mustWriteFile(t, filepath.Join(inboxDir, "test.md"), []byte(content))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if len(items[0].Tags) != 2 || items[0].Tags[0] != "go" || items[0].Tags[1] != "web" {
		t.Errorf("expected tags [go, web], got %v", items[0].Tags)
	}
}

func TestScan_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)
	mustWriteFile(t, filepath.Join(inboxDir, "a.md"), []byte("https://example.com/a\n"))
	mustWriteFile(t, filepath.Join(inboxDir, "b.md"), []byte("https://example.com/b\n"))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestScan_IgnoresNonMdFiles(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)
	mustWriteFile(t, filepath.Join(inboxDir, "note.txt"), []byte("https://example.com/a\n"))
	mustWriteFile(t, filepath.Join(inboxDir, "data.json"), []byte(`{"url":"x"}`))
	mustWriteFile(t, filepath.Join(inboxDir, "valid.md"), []byte("https://example.com/b\n"))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestScan_IgnoresSubdirectories(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	subDir := filepath.Join(inboxDir, "subdir")
	mustMkdir(t, subDir)
	mustWriteFile(t, filepath.Join(subDir, "nested.md"), []byte("https://example.com/nested\n"))
	mustWriteFile(t, filepath.Join(inboxDir, "top.md"), []byte("https://example.com/top\n"))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (top-level only), got %d", len(items))
	}
	if items[0].URL != "https://example.com/top" {
		t.Errorf("expected top-level URL, got %q", items[0].URL)
	}
}

func TestScan_SkipsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)
	mustWriteFile(t, filepath.Join(inboxDir, "empty.md"), []byte(""))
	mustWriteFile(t, filepath.Join(inboxDir, "whitespace.md"), []byte("   \n\n  "))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestScan_SkipsNoURL(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)
	mustWriteFile(t, filepath.Join(inboxDir, "nourl.md"), []byte("just some text\nno url here\n"))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestScan_TitleFallbackToFilename(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox")
	mustMkdir(t, inboxDir)
	mustWriteFile(t, filepath.Join(inboxDir, "my-great-article.md"), []byte("https://example.com/article\n"))

	items, err := Scan(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Title != "my-great-article" {
		t.Errorf("expected title 'my-great-article', got %q", items[0].Title)
	}
}

func TestDelete_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.md")
	mustWriteFile(t, filePath, []byte("content"))

	err := Delete(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestDelete_NonexistentFile(t *testing.T) {
	err := Delete("/nonexistent/path/file.md")
	if err != nil {
		t.Fatalf("expected no error for nonexistent file, got: %v", err)
	}
}
