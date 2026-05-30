package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

func setupHygieneEnv(t *testing.T) (string, *store.Store, *vault.Vault) {
	t.Helper()
	tmpDir := t.TempDir()

	v := vault.New(tmpDir)
	if err := v.Init(); err != nil {
		t.Fatalf("vault init: %v", err)
	}

	dbPath := filepath.Join(tmpDir, ".lucidvault.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return tmpDir, db, v
}

// writeWikiFile creates a wiki file with the given content.
func writeWikiFile(t *testing.T, vaultPath, filename, content string) {
	t.Helper()
	absPath := filepath.Join(vaultPath, "wiki", filename)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeRawFile creates a raw file with the given content.
func writeRawFile(t *testing.T, vaultPath, filename, content string) {
	t.Helper()
	absPath := filepath.Join(vaultPath, "raw", filename)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- Broken edge cleanup ---

func TestRunHygiene_DeletesBrokenEdges(t *testing.T) {
	tmpDir, db, v := setupHygieneEnv(t)

	// Create a wiki file for page-a but NOT for page-b
	writeWikiFile(t, tmpDir, "page-a.md", "# Page A\n\nLinks to [[page-b]].")

	// Insert edges: a->b (b doesn't exist on disk)
	edges := []store.Edge{
		{FromSlug: "page-a", ToSlug: "page-b", Type: "wikilink"},
	}
	if err := db.RebuildEdges("wikilink", edges); err != nil {
		t.Fatal(err)
	}

	// Precondition: verify edge exists before hygiene
	preCount, err := db.EdgeCount()
	if err != nil {
		t.Fatal(err)
	}
	if preCount != 1 {
		t.Fatalf("precondition failed: expected 1 edge before hygiene, got %d", preCount)
	}

	runHygiene(db, v)

	// Broken edge should be deleted
	count, err := db.EdgeCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 edges after hygiene, got %d", count)
	}
}

// --- Stale index entries ---

func TestSyncIndex_RemovesStaleEntries(t *testing.T) {
	_, _, v := setupHygieneEnv(t)

	// Add an index entry for a page that doesn't exist
	if err := v.UpdateIndex("deleted-page", "Deleted Page", []string{"old"}); err != nil {
		t.Fatal(err)
	}

	// Precondition: verify entry exists before sync
	preContent, _ := v.ReadIndex()
	if !strings.Contains(preContent, "[[deleted-page]]") {
		t.Fatal("precondition failed: stale entry not found in index before sync")
	}

	syncIndex(v)

	indexContent, _ := v.ReadIndex()
	if strings.Contains(indexContent, "[[deleted-page]]") {
		t.Error("expected stale index entry to be removed")
	}
}

// --- Missing index entries ---

func TestSyncIndex_AddsMissingEntries(t *testing.T) {
	tmpDir, _, v := setupHygieneEnv(t)

	// Create a wiki file that's NOT in the index
	content := `---
title: "Unlisted Page"
tags:
  - golang
  - testing
---

# Unlisted Page
`
	writeWikiFile(t, tmpDir, "unlisted-page.md", content)

	syncIndex(v)

	indexContent, _ := v.ReadIndex()
	if !strings.Contains(indexContent, "[[unlisted-page]]") {
		t.Error("expected missing wiki file to be added to index")
	}
	if !strings.Contains(indexContent, "golang") {
		t.Error("expected tags from frontmatter in index entry")
	}
}

// --- Tag/title drift ---

func TestSyncIndex_FixesTagDrift(t *testing.T) {
	tmpDir, _, v := setupHygieneEnv(t)

	// Create wiki file with tags [golang, testing]
	content := `---
title: "Drifted Page"
tags:
  - golang
  - testing
---

# Drifted Page
`
	writeWikiFile(t, tmpDir, "drifted-page.md", content)

	// Add index entry with stale tags [old-tag]
	if err := v.UpdateIndex("drifted-page", "Old Title", []string{"old-tag"}); err != nil {
		t.Fatal(err)
	}

	syncIndex(v)

	indexContent, _ := v.ReadIndex()
	// Should have updated tags
	if !strings.Contains(indexContent, "golang") {
		t.Error("expected synced tags to include 'golang'")
	}
	if strings.Contains(indexContent, "old-tag") {
		t.Error("expected old-tag to be replaced")
	}
	// Should have updated title
	if !strings.Contains(indexContent, "Drifted Page") {
		t.Error("expected title to be synced from frontmatter")
	}
}

// --- Orphaned raw files (D14) ---

func TestCleanRawWikiOrphans_DeletesOrphanedRaw(t *testing.T) {
	tmpDir, _, v := setupHygieneEnv(t)

	// Create a raw file with NO matching wiki
	writeRawFile(t, tmpDir, "orphaned.md", "raw content")

	cleanRawWikiOrphans(v)

	rawPath := filepath.Join(tmpDir, "raw", "orphaned.md")
	if _, err := os.Stat(rawPath); !os.IsNotExist(err) {
		t.Error("expected orphaned raw file to be deleted")
	}
}

func TestCleanRawWikiOrphans_KeepsRawWithWiki(t *testing.T) {
	tmpDir, _, v := setupHygieneEnv(t)

	// Create raw AND wiki
	writeRawFile(t, tmpDir, "paired.md", "raw content")
	writeWikiFile(t, tmpDir, "paired.md", "wiki content")

	cleanRawWikiOrphans(v)

	rawPath := filepath.Join(tmpDir, "raw", "paired.md")
	if _, err := os.Stat(rawPath); os.IsNotExist(err) {
		t.Error("expected paired raw file to be kept")
	}
}

// --- Broken raw footer links (D15) ---

func TestCleanRawWikiOrphans_RewritesBrokenFooter(t *testing.T) {
	tmpDir, _, v := setupHygieneEnv(t)

	// Create wiki file with footer pointing to raw file that doesn't exist
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

	writeWikiFile(t, tmpDir, "test-article.md", content)
	// Note: NO raw file created

	cleanRawWikiOrphans(v)

	data, err := os.ReadFile(filepath.Join(tmpDir, "wiki", "test-article.md"))
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)

	if vault.HasRawFooterLink(result, "raw/test-article.md") {
		t.Error("expected broken raw footer link to be rewritten")
	}
}

// --- Orphan pages logged ---

func TestRunHygiene_LogsOrphans(t *testing.T) {
	tmpDir, db, v := setupHygieneEnv(t)

	// Create a wiki page and a bookmark record (so FindOrphans picks it up)
	writeWikiFile(t, tmpDir, "lonely-page.md", "# Lonely\n\nNo links here.")
	if err := db.UpsertBookmark(&store.BookmarkRecord{
		WikiPath:      "wiki/lonely-page.md",
		RawPath:       "raw/lonely-page.md",
		Title:         "Lonely Page",
		URL:           "https://example.com/lonely",
		URLNormalized: "https://example.com/lonely",
	}); err != nil {
		t.Fatal(err)
	}

	// Should not panic or error — orphans are just logged
	runHygiene(db, v)
}

// --- Empty vault / fresh install ---

func TestRunHygiene_EmptyVaultIsNoop(t *testing.T) {
	_, db, v := setupHygieneEnv(t)

	// Should not panic or error on empty vault
	runHygiene(db, v)
}

// --- Hygiene interval config ---

func TestLoadConfig_HygieneInterval(t *testing.T) {
	// Set required vars
	t.Setenv("OLLAMA_API_KEY", "test")
	t.Setenv("VAULT_PATH", "/tmp/test")

	// Default
	t.Setenv("HYGIENE_INTERVAL", "")
	cfg, err := loadConfig(false, false)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.hygieneInterval != 10 {
		t.Errorf("expected default hygiene interval 10, got %d", cfg.hygieneInterval)
	}

	// Custom
	t.Setenv("HYGIENE_INTERVAL", "5")
	cfg, err = loadConfig(false, false)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.hygieneInterval != 5 {
		t.Errorf("expected hygiene interval 5, got %d", cfg.hygieneInterval)
	}

	// Invalid
	t.Setenv("HYGIENE_INTERVAL", "not-a-number")
	_, err = loadConfig(false, false)
	if err == nil {
		t.Error("expected error for invalid HYGIENE_INTERVAL")
	}
}

// --- SyncIndex with no-frontmatter pages ---

func TestSyncIndex_FallsBackToSlugTitle(t *testing.T) {
	tmpDir, _, v := setupHygieneEnv(t)

	// Wiki file without frontmatter
	writeWikiFile(t, tmpDir, "no-frontmatter.md", "# Just a heading\n\nSome content.")

	syncIndex(v)

	indexContent, _ := v.ReadIndex()
	if !strings.Contains(indexContent, "[[no-frontmatter]]") {
		t.Error("expected page without frontmatter to be added to index")
	}
}

// --- Hygiene runs every Nth cycle ---

func TestRunPollCycle_RunsHygieneAtInterval(t *testing.T) {
	tmpDir, db, v := setupHygieneEnv(t)

	// Create a broken edge that hygiene should clean up
	writeWikiFile(t, tmpDir, "page-a.md", "# Page A\n\nLinks to [[missing]].")
	edges := []store.Edge{
		{FromSlug: "page-a", ToSlug: "missing", Type: "wikilink"},
	}
	if err := db.RebuildEdges("wikilink", edges); err != nil {
		t.Fatal(err)
	}

	// Simulate poll cycles — hygiene should run on Nth cycle
	cfg := &config{hygieneInterval: 2}

	// Cycle 1: no hygiene yet
	pollCycleCount = 0
	runPollCycleWithHygiene(cfg, db, v)
	count, _ := db.EdgeCount()
	if count == 0 {
		t.Error("expected broken edge to survive first cycle (hygiene not yet triggered)")
	}

	// Cycle 2: hygiene runs
	runPollCycleWithHygiene(cfg, db, v)
	count, _ = db.EdgeCount()
	if count != 0 {
		t.Errorf("expected broken edge to be cleaned on 2nd cycle, got %d edges", count)
	}
}


