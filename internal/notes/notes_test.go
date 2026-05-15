package notes

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// helper: write a file, creating parent directories as needed.
func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestScan_FindsMarkdownFiles verifies that .md files inside notes/ are returned.
func TestScan_FindsMarkdownFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "notes", "alpha.md"), "# Alpha")
	writeFile(t, filepath.Join(dir, "notes", "beta.md"), "# Beta")

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

// TestScan_Recursive verifies nested directories are walked and relative paths are correct.
func TestScan_Recursive(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "notes", "top.md"), "top")
	writeFile(t, filepath.Join(dir, "notes", "sub", "deep.md"), "deep")

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
	}
	want := []string{
		filepath.Join("notes", "top.md"),
		filepath.Join("notes", "sub", "deep.md"),
	}
	for _, w := range want {
		if !paths[w] {
			t.Errorf("expected path %q in results, got %v", w, paths)
		}
	}
}

// TestScan_SkipsNonMarkdown verifies .txt and other non-.md files are ignored.
func TestScan_SkipsNonMarkdown(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "notes", "note.md"), "# Note")
	writeFile(t, filepath.Join(dir, "notes", "readme.txt"), "plain text")

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if files[0].Path != filepath.Join("notes", "note.md") {
		t.Errorf("unexpected path: %q", files[0].Path)
	}
}

// TestScan_SkipsEmpty verifies that empty (and whitespace-only) .md files are skipped.
func TestScan_SkipsEmpty(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "notes", "empty.md"), "")
	writeFile(t, filepath.Join(dir, "notes", "blank.md"), "  \n\t\n  ")
	writeFile(t, filepath.Join(dir, "notes", "real.md"), "# Content")

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if files[0].Title != "real" {
		t.Errorf("expected title 'real', got %q", files[0].Title)
	}
}

// TestScan_ContentHash verifies that the hash is a deterministic hex-encoded SHA-256.
func TestScan_ContentHash(t *testing.T) {
	dir := t.TempDir()
	content := "# Deterministic"
	writeFile(t, filepath.Join(dir, "notes", "hash-me.md"), content)

	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])
	if files[0].ContentHash != want {
		t.Errorf("hash mismatch: got %q, want %q", files[0].ContentHash, want)
	}
}

// TestParseFrontmatter_ListStyle verifies extraction of YAML list-style tags.
func TestParseFrontmatter_ListStyle(t *testing.T) {
	content := "---\ntitle: Test\ntags:\n  - foo\n  - bar\n---\n# Body"
	tags := ParseFrontmatter(content)
	if len(tags) != 2 || tags[0] != "foo" || tags[1] != "bar" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

// TestParseFrontmatter_InlineArray verifies extraction of inline array tags.
func TestParseFrontmatter_InlineArray(t *testing.T) {
	content := "---\ntags: [alpha, beta, gamma]\n---\n# Body"
	tags := ParseFrontmatter(content)
	if len(tags) != 3 || tags[0] != "alpha" || tags[1] != "beta" || tags[2] != "gamma" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

// TestParseFrontmatter_NoFrontmatter verifies nil is returned when there is no frontmatter.
func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	content := "# Just a plain markdown file\nNo frontmatter here."
	tags := ParseFrontmatter(content)
	if tags != nil {
		t.Errorf("expected nil, got %v", tags)
	}
}

// TestParseFrontmatter_NoTags verifies nil is returned when frontmatter exists but has no tags field.
func TestParseFrontmatter_NoTags(t *testing.T) {
	content := "---\ntitle: Something\nauthor: Bas\n---\n# Body"
	tags := ParseFrontmatter(content)
	if tags != nil {
		t.Errorf("expected nil, got %v", tags)
	}
}

// TestParseFrontmatter_EmptyTagsValue verifies nil when tags: key has no value and no list items.
func TestParseFrontmatter_EmptyTagsValue(t *testing.T) {
	content := "---\ntags:\ntitle: Something\n---\n# Body"
	tags := ParseFrontmatter(content)
	if tags != nil {
		t.Errorf("expected nil, got %v", tags)
	}
}

// TestParseFrontmatter_QuotedValues verifies that surrounding quotes are stripped from tag values.
func TestParseFrontmatter_QuotedValues(t *testing.T) {
	content := `---
tags: ["machine learning", 'ai']
---
# Body`
	tags := ParseFrontmatter(content)
	if len(tags) != 2 || tags[0] != "machine learning" || tags[1] != "ai" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

// TestParseFrontmatter_CRLF verifies that Windows line endings don't break parsing.
func TestParseFrontmatter_CRLF(t *testing.T) {
	content := "---\r\ntags:\r\n  - foo\r\n  - bar\r\n---\r\n# Body"
	tags := ParseFrontmatter(content)
	if len(tags) != 2 || tags[0] != "foo" || tags[1] != "bar" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

// TestParseFrontmatter_SimilarKeyName verifies that keys like "tags_v2:" are not matched.
func TestParseFrontmatter_SimilarKeyName(t *testing.T) {
	content := "---\ntags_v2: [wrong]\ntags:\n  - correct\n---\n# Body"
	tags := ParseFrontmatter(content)
	if len(tags) != 1 || tags[0] != "correct" {
		t.Errorf("expected [correct], got %v", tags)
	}
}

// TestParseFrontmatter_IndentedTags verifies that nested (indented) tags: keys are skipped.
func TestParseFrontmatter_IndentedTags(t *testing.T) {
	content := "---\nmetadata:\n  tags: [wrong]\ntags:\n  - correct\n---\n# Body"
	tags := ParseFrontmatter(content)
	if len(tags) != 1 || tags[0] != "correct" {
		t.Errorf("expected [correct], got %v", tags)
	}
}

// TestParseTitle verifies title extraction from frontmatter.
func TestParseTitle(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"basic", "---\ntitle: My Title\n---\n# Body", "My Title"},
		{"quoted", "---\ntitle: \"Quoted Title\"\n---\n# Body", "Quoted Title"},
		{"single_quoted", "---\ntitle: 'Single'\n---\n# Body", "Single"},
		{"no_frontmatter", "# Just content", ""},
		{"no_title", "---\ntags:\n  - foo\n---\n# Body", ""},
		{"crlf", "---\r\ntitle: CRLF Title\r\n---\r\n# Body", "CRLF Title"},
		{"empty_value", "---\ntitle:\n---\n# Body", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTitle(tc.content)
			if got != tc.want {
				t.Errorf("ParseTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTitleFromFilename verifies various path inputs produce the correct base name.
func TestTitleFromFilename(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"notes/my-note.md", "my-note"},
		{"notes/sub/deep-note.md", "deep-note"},
		{"notes/simple.md", "simple"},
		{"my-note.md", "my-note"},
	}
	for _, tc := range cases {
		got := TitleFromFilename(tc.input)
		if got != tc.want {
			t.Errorf("TitleFromFilename(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
