package vault

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestScanRawDir_Empty(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	paths, err := v.ScanRawDir()
	if err != nil {
		t.Fatalf("ScanRawDir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected empty, got %v", paths)
	}
}

func TestScanRawDir_FindsFiles(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create raw files
	rawDir := filepath.Join(dir, "raw")
	for _, name := range []string{"page-a.md", "page-b.md"} {
		if err := os.WriteFile(filepath.Join(rawDir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Create a non-.md file that should be ignored
	if err := os.WriteFile(filepath.Join(rawDir, "readme.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := v.ScanRawDir()
	if err != nil {
		t.Fatalf("ScanRawDir: %v", err)
	}
	sort.Strings(paths)
	if len(paths) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(paths), paths)
	}
	if paths[0] != "raw/page-a.md" || paths[1] != "raw/page-b.md" {
		t.Errorf("unexpected paths: %v", paths)
	}
}

func TestScanRawDir_NoRawDir(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	// Don't call Init — no raw/ dir exists

	paths, err := v.ScanRawDir()
	if err != nil {
		t.Fatalf("ScanRawDir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected empty, got %v", paths)
	}
}

func TestParseFrontmatterURL(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "standard url field",
			content: `---
title: "Test"
url: "https://example.com/article"
tags:
  - test
---

# Content`,
			want: "https://example.com/article",
		},
		{
			name: "source field as fallback",
			content: `---
title: "Test"
source: "https://example.com/source"
tags:
  - test
---

# Content`,
			want: "https://example.com/source",
		},
		{
			name: "url takes precedence over source",
			content: `---
title: "Test"
url: "https://example.com/url"
source: "https://example.com/source"
---

# Content`,
			want: "https://example.com/url",
		},
		{
			name:    "no frontmatter",
			content: "# No frontmatter",
			want:    "",
		},
		{
			name: "no url or source",
			content: `---
title: "Test"
tags:
  - test
---

# Content`,
			want: "",
		},
		{
			name: "unquoted url",
			content: `---
title: "Test"
url: https://example.com/unquoted
---

# Content`,
			want: "https://example.com/unquoted",
		},
		{
			name: "single-quoted url",
			content: `---
title: "Test"
url: 'https://example.com/single-quoted'
---

# Content`,
			want: "https://example.com/single-quoted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFrontmatterURL(tt.content)
			if got != tt.want {
				t.Errorf("ParseFrontmatterURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasRawFooterLink(t *testing.T) {
	tests := []struct {
		name    string
		content string
		rawPath string
		want    bool
	}{
		{
			name: "has raw footer link",
			content: `# Article

## Summary
Content here.

---
*Source: [Raw](raw/my-article.md) | [Original](https://example.com)*`,
			rawPath: "raw/my-article.md",
			want:    true,
		},
		{
			name: "no footer",
			content: `# Article

## Summary
Content here.`,
			rawPath: "raw/my-article.md",
			want:    false,
		},
		{
			name: "footer without raw link",
			content: `# Article

---
*Source: [Original](https://example.com)*`,
			rawPath: "raw/my-article.md",
			want:    false,
		},
		{
			name: "different raw path",
			content: `# Article

---
*Source: [Raw](raw/other-article.md) | [Original](https://example.com)*`,
			rawPath: "raw/my-article.md",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasRawFooterLink(tt.content, tt.rawPath)
			if got != tt.want {
				t.Errorf("HasRawFooterLink() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRewriteFooterLink(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	content := `---
title: "Test Article"
url: "https://example.com/article"
tags:
  - test
---

# Test Article

## Summary
Content here.

---
*Source: [Raw](raw/test-article.md) | [Original](https://example.com/article)*`

	wikiPath := filepath.Join("wiki", "test-article.md")
	absPath := filepath.Join(dir, wikiPath)
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := v.RewriteFooterLink(wikiPath, "raw/test-article.md", "https://example.com/article")
	if err != nil {
		t.Fatalf("RewriteFooterLink: %v", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	result := string(data)

	// Should no longer contain raw link
	if HasRawFooterLink(result, "raw/test-article.md") {
		t.Error("expected raw footer link to be removed")
	}

	// Should contain URL instead
	if !strings.Contains(result, "https://example.com/article") {
		t.Error("expected URL to be in footer")
	}
}

func TestRewriteFooterLink_NoMatch(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	content := `# No footer here`
	wikiPath := filepath.Join("wiki", "test.md")
	absPath := filepath.Join(dir, wikiPath)
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should not error, just no-op
	err := v.RewriteFooterLink(wikiPath, "raw/test.md", "https://example.com")
	if err != nil {
		t.Fatalf("RewriteFooterLink: %v", err)
	}

	data, _ := os.ReadFile(absPath)
	if string(data) != content {
		t.Error("expected content to be unchanged")
	}
}


