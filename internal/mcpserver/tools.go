package mcpserver

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// safeReadFile validates that a relative path stays within the vault before reading.
func safeReadFile(v *vault.Vault, relPath string) (string, error) {
	absPath := filepath.Join(v.BasePath, relPath)
	cleanAbs := filepath.Clean(absPath)
	cleanBase := filepath.Clean(v.BasePath)
	if !strings.HasPrefix(cleanAbs, cleanBase+string(os.PathSeparator)) && cleanAbs != cleanBase {
		return "", fmt.Errorf("path escapes vault: %q", relPath)
	}
	return v.ReadFile(relPath)
}

// GrepResult represents a single match from grep_vault.
type GrepResult struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// RelatedEntry represents a linked page from related_notes.
type RelatedEntry struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Exists    bool   `json:"exists"`
	Direction string `json:"direction"` // "outbound", "inbound", or "both"
}

// VaultOverview contains high-level vault statistics for agent orientation.
type VaultOverview struct {
	WikiCount   int      `json:"wiki_count"`
	RawCount    int      `json:"raw_count"`
	NoteCount   int      `json:"note_count"`
	EdgeCount   int      `json:"edge_count"`
	TopTags     []string `json:"top_tags"`
	HasSoul     bool     `json:"has_soul"`
	LastUpdated string   `json:"last_updated"`
}

// HandleGetSoul returns the content of soul.md.
func HandleGetSoul(v *vault.Vault) (string, error) {
	content, err := v.ReadSoul()
	if err != nil {
		return "", fmt.Errorf("reading soul.md: %w", err)
	}
	if content == "" {
		return "No soul.md found. Create one at the vault root to personalize your experience.", nil
	}
	return content, nil
}

// maxSearchResults caps the number of entries returned by HandleSearchIndex.
const maxSearchResults = 50

// HandleSearchIndex searches index.md for entries matching the query.
// The query is split on whitespace; every term must match (AND semantics) against
// slug, title, or any tag (case-insensitive substring). Results are capped at
// maxSearchResults to keep responses agent-friendly.
func HandleSearchIndex(v *vault.Vault, query string) ([]IndexEntry, error) {
	indexContent, err := v.ReadIndex()
	if err != nil {
		return nil, fmt.Errorf("reading index: %w", err)
	}

	query = strings.TrimSpace(query)
	terms := strings.Fields(strings.ToLower(query))

	var results []IndexEntry

	for line := range strings.SplitSeq(indexContent, "\n") {
		if len(results) >= maxSearchResults {
			break
		}
		entry := ParseIndexEntry(line)
		if entry == nil {
			continue
		}

		// Match against slug, title, and tags (case insensitive, AND across terms).
		if matchesQuery(entry, terms) {
			results = append(results, *entry)
		}
	}

	if results == nil {
		results = []IndexEntry{}
	}
	return results, nil
}

// termMatchesEntry reports whether a single term matches the entry's slug,
// title, or any tag (case-insensitive substring). It is the shared primitive
// used by both matchesQuery (AND semantics) and suggestSlugs (OR semantics).
// The term is lowercased internally so callers need not pre-lowercase it.
func termMatchesEntry(entry *IndexEntry, term string) bool {
	term = strings.ToLower(term)
	if strings.Contains(strings.ToLower(entry.Slug), term) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Title), term) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), term) {
			return true
		}
	}
	return false
}

// matchesQuery reports whether entry matches all query terms (AND semantics).
// Each term is matched as a case-insensitive substring against the slug, title,
// and tags. Zero terms (empty query after trim) always returns false.
func matchesQuery(entry *IndexEntry, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	for _, term := range terms {
		if !termMatchesEntry(entry, term) {
			return false
		}
	}
	return true
}

// maxSlugSuggestions caps the number of similar-slug suggestions included in
// not-found error messages.
const maxSlugSuggestions = 5

// suggestSlugs returns up to maxSlugSuggestions slugs from the index that are
// similar to the failed input, using OR-scored per-term substring matching.
// The input is normalized (lowercase, separators replaced by spaces, split on
// whitespace) before scoring. Entries are ranked by score descending, with ties
// broken by index file order (deterministic). Returns nil when no entry scores
// ≥ 1, or when the index cannot be read (best-effort — never masks the original
// not-found error with an index error).
func suggestSlugs(v *vault.Vault, input string) []string {
	indexContent, err := v.ReadIndex()
	if err != nil || indexContent == "" {
		return nil
	}

	// Normalize: lowercase, replace separators with spaces, split on whitespace.
	normalized := strings.NewReplacer("-", " ", "_", " ", "/", " ").Replace(strings.ToLower(input))
	terms := strings.Fields(normalized)
	if len(terms) == 0 {
		return nil
	}

	type scoredEntry struct {
		slug  string
		score int
	}

	var scored []scoredEntry
	for line := range strings.SplitSeq(indexContent, "\n") {
		entry := ParseIndexEntry(line)
		if entry == nil {
			continue
		}
		score := 0
		for _, term := range terms {
			if termMatchesEntry(entry, term) {
				score++
			}
		}
		if score > 0 {
			scored = append(scored, scoredEntry{slug: entry.Slug, score: score})
		}
	}

	if len(scored) == 0 {
		return nil
	}

	// Sort by score descending; index order is the tiebreaker (stable sort).
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	n := min(len(scored), maxSlugSuggestions)
	slugs := make([]string, n)
	for i := range n {
		slugs[i] = scored[i].slug
	}
	return slugs
}

// HandleReadWiki reads a wiki page by slug. When the page does not exist, the
// error message is enriched with up to maxSlugSuggestions similar slugs from
// the index (OR-scored per-term substring matching) to help agents self-heal
// without an extra search_wiki round-trip.
func HandleReadWiki(v *vault.Vault, slug string) (string, error) {
	content, err := safeReadFile(v, "wiki/"+slug+".md")
	if err != nil {
		base := fmt.Errorf("wiki page %q not found: %w", slug, err)
		if suggestions := suggestSlugs(v, slug); len(suggestions) > 0 {
			return "", fmt.Errorf("%w; similar pages: %s (use search_wiki for broader discovery)",
				base, strings.Join(suggestions, ", "))
		}
		return "", base
	}
	return content, nil
}

// HandleGrepVault searches for a query string across vault files in the given scope.
func HandleGrepVault(v *vault.Vault, query, scope string) ([]GrepResult, error) {
	if scope == "" {
		scope = "wiki"
	}

	var dirs []string
	switch scope {
	case "wiki":
		dirs = []string{"wiki"}
	case "notes":
		dirs = []string{"notes"}
	case "raw":
		dirs = []string{"raw"}
	case "all":
		dirs = []string{"wiki", "notes", "raw"}
	default:
		dirs = []string{"wiki"}
	}

	queryLower := strings.ToLower(query)
	var results []GrepResult
	const maxResults = 20

	for _, dir := range dirs {
		dirPath := filepath.Join(v.BasePath, dir)
		err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			if len(results) >= maxResults {
				return filepath.SkipAll
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil // skip unreadable files
			}

			relPath, _ := filepath.Rel(v.BasePath, path)
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					results = append(results, GrepResult{
						File:    relPath,
						Line:    i + 1,
						Content: line,
					})
					if len(results) >= maxResults {
						return filepath.SkipAll
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("searching %s: %w", dir, err)
		}
	}

	if results == nil {
		results = []GrepResult{}
	}
	return results, nil
}

// HandleReadNote reads a personal note by path. Path must start with "notes/".
func HandleReadNote(v *vault.Vault, path string) (string, error) {
	if !strings.HasPrefix(path, "notes/") {
		return "", fmt.Errorf("path must start with notes/, got %q", path)
	}
	content, err := safeReadFile(v, path)
	if err != nil {
		return "", fmt.Errorf("note %q not found: %w", path, err)
	}
	return content, nil
}

// HandleReadRaw reads a raw source file by filename.
func HandleReadRaw(v *vault.Vault, filename string) (string, error) {
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		return "", fmt.Errorf("filename must not contain path separators: %q", filename)
	}
	content, err := safeReadFile(v, "raw/"+filename)
	if err != nil {
		return "", fmt.Errorf("raw file %q not found: %w", filename, err)
	}
	return content, nil
}

// HandleRelatedNotes returns pages linked from and to the given wiki page using
// bidirectional edge lookups from the store. Falls back to wikilink parsing when
// no store is available.
func HandleRelatedNotes(v *vault.Vault, slug string, db ...*store.Store) ([]RelatedEntry, error) {
	// Read the wiki page upfront: verifies existence (not-found → enriched error)
	// and provides content for the store-less fallback path without a second read.
	wikiContent, err := safeReadFile(v, "wiki/"+slug+".md")
	if err != nil {
		base := fmt.Errorf("wiki page %q not found: %w", slug, err)
		if suggestions := suggestSlugs(v, slug); len(suggestions) > 0 {
			return nil, fmt.Errorf("%w; similar pages: %s (use search_wiki for broader discovery)",
				base, strings.Join(suggestions, ", "))
		}
		return nil, base
	}

	// Track direction per related slug: outbound, inbound, or both.
	type dirInfo struct {
		outbound bool
		inbound  bool
	}
	related := make(map[string]*dirInfo)

	// Use store for bidirectional lookups if available.
	var useStore *store.Store
	if len(db) > 0 && db[0] != nil {
		useStore = db[0]
	}

	if useStore != nil {
		outEdges, err := useStore.GetOutboundEdges(slug)
		if err != nil {
			return nil, fmt.Errorf("getting outbound edges: %w", err)
		}
		for _, e := range outEdges {
			if _, ok := related[e.ToSlug]; !ok {
				related[e.ToSlug] = &dirInfo{}
			}
			related[e.ToSlug].outbound = true
		}

		inEdges, err := useStore.GetInboundEdges(slug)
		if err != nil {
			return nil, fmt.Errorf("getting inbound edges: %w", err)
		}
		for _, e := range inEdges {
			if _, ok := related[e.FromSlug]; !ok {
				related[e.FromSlug] = &dirInfo{}
			}
			related[e.FromSlug].inbound = true
		}
	} else {
		// Fallback: parse wikilinks from the already-read wiki file (forward only).
		links := ParseWikiLinks(wikiContent)
		for _, link := range links {
			related[link] = &dirInfo{outbound: true}
		}
	}

	var results []RelatedEntry
	for relSlug, dir := range related {
		direction := "outbound"
		if dir.outbound && dir.inbound {
			direction = "both"
		} else if dir.inbound {
			direction = "inbound"
		}

		entry := RelatedEntry{
			Slug:      relSlug,
			Direction: direction,
		}

		// Determine file path based on link type.
		var relPath string
		if strings.HasPrefix(relSlug, "notes/") {
			relPath = relSlug + ".md"
		} else {
			relPath = "wiki/" + relSlug + ".md"
		}

		entry.Exists = v.FileHasContent(relPath)
		if entry.Exists {
			fileContent, err := v.ReadFile(relPath)
			if err == nil {
				entry.Title = ParseFrontmatterTitle(fileContent)
			}
		}
		if entry.Title == "" {
			parts := strings.Split(relSlug, "/")
			entry.Title = parts[len(parts)-1]
		}

		results = append(results, entry)
	}

	// Sort by slug for deterministic output.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Slug < results[j].Slug
	})

	if results == nil {
		results = []RelatedEntry{}
	}
	return results, nil
}

// HandleVaultOverview returns high-level vault statistics for agent orientation.
func HandleVaultOverview(v *vault.Vault, db *store.Store) (*VaultOverview, error) {
	overview := &VaultOverview{}

	// Count wiki files.
	wikiFiles, err := v.ScanWikiDir()
	if err != nil {
		return nil, fmt.Errorf("scanning wiki dir: %w", err)
	}
	overview.WikiCount = len(wikiFiles)

	// Count raw files.
	rawFiles, err := v.ScanRawDir()
	if err != nil {
		return nil, fmt.Errorf("scanning raw dir: %w", err)
	}
	overview.RawCount = len(rawFiles)

	// Count notes.
	noteFiles, err := v.ScanNotesDir()
	if err != nil {
		return nil, fmt.Errorf("scanning notes dir: %w", err)
	}
	overview.NoteCount = len(noteFiles)

	// Edge count from store.
	if db != nil {
		count, err := db.EdgeCount()
		if err != nil {
			return nil, fmt.Errorf("counting edges: %w", err)
		}
		overview.EdgeCount = count
	}

	// Parse index.md for tags and last updated.
	indexContent, err := v.ReadIndex()
	if err != nil {
		return nil, fmt.Errorf("reading index: %w", err)
	}

	tagFreq := make(map[string]int)
	for line := range strings.SplitSeq(indexContent, "\n") {
		if strings.HasPrefix(line, "Last updated:") {
			overview.LastUpdated = strings.TrimSpace(strings.TrimPrefix(line, "Last updated:"))
		}
		entry := ParseIndexEntry(line)
		if entry == nil {
			continue
		}
		for _, tag := range entry.Tags {
			tagFreq[tag]++
		}
	}

	// Sort tags by frequency (desc), then alphabetically for ties.
	type tagCount struct {
		tag   string
		count int
	}
	var tagCounts []tagCount
	for tag, count := range tagFreq {
		tagCounts = append(tagCounts, tagCount{tag, count})
	}
	sort.Slice(tagCounts, func(i, j int) bool {
		if tagCounts[i].count != tagCounts[j].count {
			return tagCounts[i].count > tagCounts[j].count
		}
		return tagCounts[i].tag < tagCounts[j].tag
	})

	topN := 10
	if len(tagCounts) < topN {
		topN = len(tagCounts)
	}
	overview.TopTags = make([]string, topN)
	for i := 0; i < topN; i++ {
		overview.TopTags[i] = tagCounts[i].tag
	}

	// Check soul.md existence.
	overview.HasSoul = v.FileHasContent("soul.md")

	return overview, nil
}

// HandleAddBookmark creates an inbox file for a URL to be processed by the pipeline.
// Note: files with the same slug are silently overwritten (intentional — dedup by slug).
func HandleAddBookmark(v *vault.Vault, rawURL, title string, tags []string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid URL: must start with http:// or https://")
	}

	slug := ""
	if title != "" {
		slug = vault.GenerateSlug(title)
	} else {
		slug = slugFromURL(rawURL)
	}

	filename := slug + ".md"

	if tags == nil {
		tags = []string{}
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", vault.QuoteYAMLValue(title))
	b.WriteString("tags: [")
	b.WriteString(strings.Join(tags, ", "))
	b.WriteString("]\n")
	b.WriteString("---\n\n")
	b.WriteString(rawURL)
	b.WriteString("\n")

	inboxDir := filepath.Join(v.BasePath, "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return "", fmt.Errorf("creating inbox directory: %w", err)
	}

	absPath := filepath.Join(inboxDir, filename)
	if err := os.WriteFile(absPath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing inbox file: %w", err)
	}

	return filename, nil
}

// slugFromURL derives a slug from a URL's host and path segments.
func slugFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "untitled"
	}

	parts := []string{u.Hostname()}
	for seg := range strings.SplitSeq(strings.Trim(u.Path, "/"), "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}

	return vault.GenerateSlug(strings.Join(parts, " "))
}

// HandleAddNote creates a note file in the notes directory.
func HandleAddNote(v *vault.Vault, title, content string, tags []string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	slug := vault.GenerateSlug(title)
	filename := slug + ".md"

	if tags == nil {
		tags = []string{}
	}

	date := time.Now().Format("2006-01-02")

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "date: %q\n", date)
	b.WriteString("tags: [")
	b.WriteString(strings.Join(tags, ", "))
	b.WriteString("]\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", title)
	b.WriteString(content)
	b.WriteString("\n")

	notesDir := filepath.Join(v.BasePath, "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return "", fmt.Errorf("creating notes directory: %w", err)
	}

	absPath := filepath.Join(notesDir, filename)
	if err := os.WriteFile(absPath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing note file: %w", err)
	}

	return filename, nil
}

// HandleExpandGraph expands seed slugs by traversing wiki-link edges up to N hops.
// Returns deduplicated slugs reachable from seeds, excluding the seeds themselves.
func HandleExpandGraph(db *store.Store, seeds []string, hops int) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("expanding graph: store is not available")
	}
	if hops < 1 {
		hops = 2
	}
	if hops > 5 {
		hops = 5
	}
	return db.ExpandGraph(seeds, hops)
}
