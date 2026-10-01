package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// --- UpdateRelatedSection tests ---

func TestUpdateRelatedSection_CreatesNewSection(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	// Write a wiki file without ## Related
	content := "---\ntitle: \"Test\"\n---\n\n# Test\n\nSome content.\n"
	if _, err := v.WriteWiki("test.md", content); err != nil {
		t.Fatal(err)
	}

	err := v.UpdateRelatedSection("wiki/test.md", []string{
		"[[cilium-ebpf]] — shared tags: kubernetes, networking",
	})
	if err != nil {
		t.Fatalf("UpdateRelatedSection: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "wiki", "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if !contains(got, "## Related") {
		t.Error("expected ## Related section to be created")
	}
	if !contains(got, "[[cilium-ebpf]] — shared tags: kubernetes, networking") {
		t.Error("expected backlink to be present")
	}
}

func TestUpdateRelatedSection_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	content := "# Test\n\n## Related\n\n- [[existing-page]] — shared tags: go\n"
	if _, err := v.WriteWiki("test.md", content); err != nil {
		t.Fatal(err)
	}

	err := v.UpdateRelatedSection("wiki/test.md", []string{
		"[[new-page]] — shared tags: rust",
	})
	if err != nil {
		t.Fatalf("UpdateRelatedSection: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "wiki", "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if !contains(got, "[[existing-page]]") {
		t.Error("expected existing link to be preserved")
	}
	if !contains(got, "[[new-page]] — shared tags: rust") {
		t.Error("expected new link to be appended")
	}
}

func TestUpdateRelatedSection_SkipsDuplicates(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	content := "# Test\n\n## Related\n\n- [[existing-page]] — shared tags: go\n"
	if _, err := v.WriteWiki("test.md", content); err != nil {
		t.Fatal(err)
	}

	err := v.UpdateRelatedSection("wiki/test.md", []string{
		"[[existing-page]] — shared tags: go",
	})
	if err != nil {
		t.Fatalf("UpdateRelatedSection: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "wiki", "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	count := countOccurrences(got, "[[existing-page]]")
	if count != 1 {
		t.Errorf("expected 1 occurrence of [[existing-page]], got %d", count)
	}
}

func TestUpdateRelatedSection_InsertsBeforeFooter(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	content := "# Test\n\nSome content.\n\n---\n*Source: https://example.com*\n"
	if _, err := v.WriteWiki("test.md", content); err != nil {
		t.Fatal(err)
	}

	err := v.UpdateRelatedSection("wiki/test.md", []string{
		"[[linked-page]] — shared tags: go",
	})
	if err != nil {
		t.Fatalf("UpdateRelatedSection: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "wiki", "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	// ## Related should appear before the footer
	relatedIdx := indexOf(got, "## Related")
	footerIdx := indexOf(got, "---\n*Source:")
	if relatedIdx == -1 {
		t.Fatal("expected ## Related section")
	}
	if footerIdx == -1 {
		t.Fatal("expected footer to be preserved")
	}
	if relatedIdx >= footerIdx {
		t.Error("expected ## Related to appear before footer")
	}
}

func TestUpdateRelatedSection_FooterDetection_OnlySourcePattern(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	// A bare --- that is NOT followed by *Source: should NOT be treated as footer
	content := "# Test\n\nSome content.\n\n---\n\nMore content.\n"
	if _, err := v.WriteWiki("test.md", content); err != nil {
		t.Fatal(err)
	}

	err := v.UpdateRelatedSection("wiki/test.md", []string{
		"[[linked-page]] — shared tags: go",
	})
	if err != nil {
		t.Fatalf("UpdateRelatedSection: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "wiki", "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	// ## Related should be appended at the end (no footer detected)
	relatedIdx := indexOf(got, "## Related")
	bareHRIdx := indexOf(got, "---\n\nMore content.")
	if relatedIdx == -1 {
		t.Fatal("expected ## Related section")
	}
	if relatedIdx < bareHRIdx {
		t.Error("bare --- should not be treated as footer; ## Related should be at the end")
	}
}

// --- FindRelatedByTags tests ---

func TestFindRelatedByTags_ExcludesSelf(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	// Create index with entries
	if err := v.UpdateIndex("my-page", "My Page", []string{"go", "networking"}); err != nil {
		t.Fatal(err)
	}
	if err := v.UpdateIndex("other-page", "Other Page", []string{"go", "networking"}); err != nil {
		t.Fatal(err)
	}

	// Create wiki files so mtime can be resolved
	for _, slug := range []string{"my-page", "other-page"} {
		if _, err := v.WriteWiki(slug+".md", "# "+slug); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := v.FindRelatedByTags("my-page", []string{"go", "networking"})
	if err != nil {
		t.Fatalf("FindRelatedByTags: %v", err)
	}

	for _, c := range candidates {
		if c.Slug == "my-page" {
			t.Error("FindRelatedByTags must exclude the new page itself")
		}
	}
}

func TestFindRelatedByTags_RequiresMinTwoSharedTags(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	// Only 1 shared tag — should not be a candidate
	if err := v.UpdateIndex("only-one-tag", "One Tag", []string{"go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteWiki("only-one-tag.md", "# one tag"); err != nil {
		t.Fatal(err)
	}

	candidates, err := v.FindRelatedByTags("new-page", []string{"go", "networking"})
	if err != nil {
		t.Fatalf("FindRelatedByTags: %v", err)
	}

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates with <2 shared tags, got %d", len(candidates))
	}
}

func TestFindRelatedByTags_SortsCorrectly(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	// Page with 3 shared tags
	if err := v.UpdateIndex("three-tags", "Three Tags", []string{"go", "networking", "kubernetes"}); err != nil {
		t.Fatal(err)
	}
	// Page with 2 shared tags
	if err := v.UpdateIndex("two-tags", "Two Tags", []string{"go", "networking"}); err != nil {
		t.Fatal(err)
	}

	for _, slug := range []string{"three-tags", "two-tags"} {
		if _, err := v.WriteWiki(slug+".md", "# "+slug); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := v.FindRelatedByTags("new-page", []string{"go", "networking", "kubernetes"})
	if err != nil {
		t.Fatalf("FindRelatedByTags: %v", err)
	}

	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}

	// three-tags (3 shared) should come before two-tags (2 shared)
	if candidates[0].Slug != "three-tags" {
		t.Errorf("expected first candidate to be three-tags, got %s", candidates[0].Slug)
	}
	if candidates[1].Slug != "two-tags" {
		t.Errorf("expected second candidate to be two-tags, got %s", candidates[1].Slug)
	}
}

func TestFindRelatedByTags_Max3(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	tags := []string{"go", "networking"}
	for _, name := range []string{"a-page", "b-page", "c-page", "d-page"} {
		if err := v.UpdateIndex(name, name, tags); err != nil {
			t.Fatal(err)
		}
		if _, err := v.WriteWiki(name+".md", "# "+name); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := v.FindRelatedByTags("new-page", tags)
	if err != nil {
		t.Fatalf("FindRelatedByTags: %v", err)
	}

	if len(candidates) > 3 {
		t.Errorf("expected max 3 candidates, got %d", len(candidates))
	}
}

func TestFindRelatedByTags_BacklinkFormat(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	if err := v.UpdateIndex("related-page", "Related Page", []string{"go", "networking", "extra"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteWiki("related-page.md", "# related"); err != nil {
		t.Fatal(err)
	}

	candidates, err := v.FindRelatedByTags("new-page", []string{"go", "networking"})
	if err != nil {
		t.Fatalf("FindRelatedByTags: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	// Verify the backlink line format
	line := candidates[0].BacklinkLine("new-page")
	if !contains(line, "[[new-page]]") {
		t.Errorf("expected backlink to contain [[new-page]], got: %s", line)
	}
	if !contains(line, "shared tags:") {
		t.Errorf("expected backlink to contain 'shared tags:', got: %s", line)
	}
}

// --- parseIndexEntry tests ---

func TestParseIndexEntry_ValidLine(t *testing.T) {
	entry := parseIndexEntry("- [[my-slug]] — My Title [go, networking]")
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Slug != "my-slug" {
		t.Errorf("expected slug 'my-slug', got %q", entry.Slug)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "go" || entry.Tags[1] != "networking" {
		t.Errorf("expected tags [go, networking], got %v", entry.Tags)
	}
}

func TestParseIndexEntry_EmptyTags(t *testing.T) {
	entry := parseIndexEntry("- [[my-slug]] — My Title []")
	if entry == nil {
		t.Fatal("expected non-nil entry for empty tag brackets")
	}
	if len(entry.Tags) != 0 {
		t.Errorf("expected 0 tags, got %v", entry.Tags)
	}
}

func TestParseIndexEntry_NoTagBrackets(t *testing.T) {
	// Lines without [tags] should not match
	entry := parseIndexEntry("- [[my-slug]] — My Title")
	if entry != nil {
		t.Error("expected nil for line without tag brackets")
	}
}

func TestParseIndexEntry_MalformedLines(t *testing.T) {
	cases := []string{
		"",
		"random text",
		"- no wiki link here [tag1]",
		"# Heading",
		"- [[slug]] - wrong dash [tag]", // regular dash, not em dash
	}
	for _, line := range cases {
		if entry := parseIndexEntry(line); entry != nil {
			t.Errorf("expected nil for line %q, got %+v", line, entry)
		}
	}
}

// --- FindRelatedByTags edge cases ---

func TestFindRelatedByTags_FewerThanTwoNewTags(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	// Add a candidate with matching tag
	if err := v.UpdateIndex("other", "Other", []string{"go"}); err != nil {
		t.Fatal(err)
	}

	// With only 1 newTag, should return nil immediately
	candidates, err := v.FindRelatedByTags("new-page", []string{"go"})
	if err != nil {
		t.Fatalf("FindRelatedByTags: %v", err)
	}
	if candidates != nil {
		t.Errorf("expected nil with <2 newTags, got %v", candidates)
	}

	// With 0 newTags
	candidates, err = v.FindRelatedByTags("new-page", nil)
	if err != nil {
		t.Fatalf("FindRelatedByTags: %v", err)
	}
	if candidates != nil {
		t.Errorf("expected nil with nil newTags, got %v", candidates)
	}
}

// --- UpdateRelatedSection edge case: append to existing Related before footer ---

func TestUpdateRelatedSection_AppendsToExistingBeforeFooter(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}

	// File with ## Related section followed by a footer
	content := "# Test\n\n## Related\n\n- [[existing]] — shared tags: go\n\n---\n*Source: https://example.com*\n"
	if _, err := v.WriteWiki("test.md", content); err != nil {
		t.Fatal(err)
	}

	err := v.UpdateRelatedSection("wiki/test.md", []string{
		"[[new-link]] — shared tags: rust",
	})
	if err != nil {
		t.Fatalf("UpdateRelatedSection: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "wiki", "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	// Both links should be present
	if !contains(got, "[[existing]]") {
		t.Error("expected existing link to be preserved")
	}
	if !contains(got, "[[new-link]]") {
		t.Error("expected new link to be added")
	}

	// New link should appear before the footer
	newLinkIdx := indexOf(got, "[[new-link]]")
	footerIdx := indexOf(got, "---\n*Source:")
	if footerIdx == -1 {
		t.Fatal("expected footer to be preserved")
	}
	if newLinkIdx >= footerIdx {
		t.Error("expected new link to appear before footer")
	}
}

// --- BacklinkLine ---

func TestBacklinkLine_Format(t *testing.T) {
	c := BacklinkCandidate{
		Slug:       "candidate-slug",
		SharedTags: []string{"go", "networking"},
	}
	line := c.BacklinkLine("new-page")
	expected := "[[new-page]] — shared tags: go, networking"
	if line != expected {
		t.Errorf("expected %q, got %q", expected, line)
	}
}

// --- AutoLinkedRelatedLines tests ---
//
// AutoLinkedRelatedLines extracts only the lines in a ## Related section that
// were written by autoLinkRelated (the format produced by
// BacklinkCandidate.BacklinkLine, identified by the "— shared tags:" marker),
// stripped of their leading "- " prefix so the result can be fed straight
// back into UpdateRelatedSection/MergeRelatedLinks. See
// docs/plans/plan-preserve-note-related-on-rebuild.md.
//
// Its section-boundary detection is a separate inline loop from
// findRelatedSectionEnd/collectExistingLinks; the cases below pin down its
// current contract precisely so the two don't silently drift apart.

func TestAutoLinkedRelatedLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name: "extracts auto-link line from Related section, stripped of leading dash",
			content: "# Test\n\n## Related\n\n" +
				"- [[auto-link]] — shared tags: go, testing\n",
			want: []string{"[[auto-link]] — shared tags: go, testing"},
		},
		{
			name: "extracts only the auto-link line, ignoring a user line without the marker",
			content: "# Test\n\n## Related\n\n" +
				"- [[manual-link]]\n" +
				"- [[auto-link]] — shared tags: go\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			name: "ignores lines in other sections",
			content: "# Test\n\n## Summary\n\n" +
				"- [[other]] — shared tags: go\n\n" +
				"## Related\n\n" +
				"- [[auto-link]] — shared tags: go\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			name: "ignores lines outside the Related section entirely",
			content: "# Test\n\n" +
				"- [[stray]] — shared tags: go\n\n" +
				"## Related\n\n" +
				"- [[auto-link]] — shared tags: go\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			name: "stops at the next heading after the Related section",
			content: "# Test\n\n## Related\n\n" +
				"- [[auto-link]] — shared tags: go\n\n" +
				"## Another Section\n\n" +
				"- [[not-related]] — shared tags: go\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			name: "returns all auto-link lines in order when there are several",
			content: "# Test\n\n## Related\n\n" +
				"- [[one]] — shared tags: go\n" +
				"- [[two]] — shared tags: rust, testing\n",
			want: []string{
				"[[one]] — shared tags: go",
				"[[two]] — shared tags: rust, testing",
			},
		},
		{
			name: "returns nil when Related section has only user-written lines",
			content: "# Test\n\n## Related\n\n" +
				"- [[manual-link]]\n" +
				"- [[another-manual]]\n",
			want: nil,
		},
		{
			name:    "returns nil when there is no Related section at all",
			content: "# Test\n\nJust a body mentioning [[some-link]].\n",
			want:    nil,
		},
		{
			name:    "returns nil for empty content",
			content: "",
			want:    nil,
		},
		{
			// Pins the current implementation's section-boundary detection,
			// which stops at a "---" line (as well as at the next heading).
			name: "stops at a --- line inside the Related section",
			content: "# Test\n\n## Related\n\n" +
				"- [[auto-link]] — shared tags: go\n\n" +
				"---\n" +
				"- [[not-related]] — shared tags: go\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			// Only a "## "/"# " heading (or "---") ends the section — a
			// sub-heading one level deeper does not, per the plan's "section
			// end via findRelatedSectionEnd" contract.
			name: "does not stop at a ### sub-heading inside the Related section",
			content: "# Test\n\n## Related\n\n" +
				"- [[auto-link]] — shared tags: go\n\n" +
				"### Sub\n\n" +
				"- [[also-related]] — shared tags: rust\n",
			want: []string{
				"[[auto-link]] — shared tags: go",
				"[[also-related]] — shared tags: rust",
			},
		},
		{
			name: "handles CRLF line endings via TrimSpace",
			content: "# Test\r\n\r\n## Related\r\n\r\n" +
				"- [[auto-link]] — shared tags: go\r\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			name: "auto-link-formatted line missing the leading dash is returned unchanged",
			content: "# Test\n\n## Related\n\n" +
				"[[auto-link]] — shared tags: go\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			name: "indented auto-link line is trimmed and still extracted",
			content: "# Test\n\n## Related\n\n" +
				"  - [[auto-link]] — shared tags: go\n",
			want: []string{"[[auto-link]] — shared tags: go"},
		},
		{
			// Only the FIRST ## Related section counts: the loop breaks at
			// the next "## " heading ("## Another"), so it never reaches the
			// second "## Related" section below it.
			name: "a second ## Related section later in the file is ignored",
			content: "# Test\n\n## Related\n\n" +
				"- [[first]] — shared tags: go\n\n" +
				"## Another\n\n" +
				"## Related\n\n" +
				"- [[second]] — shared tags: go\n",
			want: []string{"[[first]] — shared tags: go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AutoLinkedRelatedLines(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AutoLinkedRelatedLines(%q) = %#v, want %#v", tt.content, got, tt.want)
			}
		})
	}
}

// --- helpers ---

func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func countOccurrences(s, sub string) int {
	count := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}
	return count
}
