package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lucidvault/internal/vault"
)

const testSoulContent = "# Soul\n\nI am a test user."

const testIndexContent = `# Wiki Index

Last updated: 2024-01-15

## Pages

- [[kubernetes-networking]] — Kubernetes Networking Deep Dive [kubernetes, networking, cni]
- [[gitops]] — GitOps with ArgoCD [gitops, argocd, kubernetes]
- [[notes/aks-thoughts]] — AKS Thoughts [azure, kubernetes]
`

const testWikiKubernetesNetworking = `---
title: "Kubernetes Networking Deep Dive"
source: "https://example.com/k8s-networking"
date_saved: 2024-01-15
tags:
  - kubernetes
  - networking
  - cni
type: bookmark
---

# Kubernetes Networking Deep Dive

## Summary
A deep dive into Kubernetes networking concepts.

## Key Takeaways
- CNI plugins handle pod networking
- Service mesh adds observability

## Related
- [[gitops]] — Related deployment practices
- [[notes/aks-thoughts]] — Personal AKS notes
- [[nonexistent-page]] — This page doesn't exist

---

*Source: [K8s Networking](https://example.com/k8s-networking) | Raw: [[2024-01-15-kubernetes-networking.md]]*
`

const testWikiGitops = `---
title: "GitOps with ArgoCD"
source: "https://example.com/gitops"
date_saved: 2024-01-20
tags:
  - gitops
  - argocd
  - kubernetes
type: bookmark
---

# GitOps with ArgoCD

## Summary
An overview of GitOps practices using ArgoCD.

## Key Takeaways
- Declarative deployments via Git
- Automated sync with drift detection
`

const testNoteContent = `---
title: "AKS Thoughts"
tags:
  - azure
  - kubernetes
---

# AKS Thoughts

My personal notes on Azure Kubernetes Service.
Networking in AKS is surprisingly complex.
`

const testRawContent = `---
title: "Kubernetes Networking Deep Dive"
source: "https://example.com/k8s-networking"
date_saved: 2024-01-15
tags:
  - kubernetes
  - networking
  - cni
type: bookmark
---

Full raw scraped content of the Kubernetes networking article goes here.
It is much longer than the wiki summary.
`

// setupTestVault creates a temporary vault directory with fixture files
// and returns a *vault.Vault and the temp directory path.
func setupTestVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()

	dir := t.TempDir()

	// Create directory structure.
	for _, sub := range []string{"wiki", "notes", "raw"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("creating %s dir: %v", sub, err)
		}
	}

	// Write fixture files.
	fixtures := map[string]string{
		"soul.md":  testSoulContent,
		"index.md": testIndexContent,
		"wiki/kubernetes-networking.md": testWikiKubernetesNetworking,
		"wiki/gitops.md":               testWikiGitops,
		"notes/aks-thoughts.md":        testNoteContent,
		"raw/2024-01-15-kubernetes-networking.md": testRawContent,
	}

	for rel, content := range fixtures {
		path := filepath.Join(dir, rel)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}

	v := vault.New(dir)
	return v, dir
}

// ---------------------------------------------------------------------------
// get_soul
// ---------------------------------------------------------------------------

func TestHandleGetSoul(t *testing.T) {
	t.Run("returns soul content", func(t *testing.T) {
		v, _ := setupTestVault(t)
		result, err := HandleGetSoul(v)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != testSoulContent {
			t.Errorf("got %q, want %q", result, testSoulContent)
		}
	})

	t.Run("missing soul.md returns message not error", func(t *testing.T) {
		dir := t.TempDir()
		v := vault.New(dir)
		result, err := HandleGetSoul(v)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == "" {
			t.Error("expected a non-empty message when soul.md is missing")
		}
	})
}

// ---------------------------------------------------------------------------
// search_index
// ---------------------------------------------------------------------------

func TestHandleSearchIndex(t *testing.T) {
	v, _ := setupTestVault(t)

	t.Run("match by title keyword", func(t *testing.T) {
		results, err := HandleSearchIndex(v, "networking")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one result")
		}
		found := false
		for _, r := range results {
			if r.Slug == "kubernetes-networking" {
				found = true
			}
		}
		if !found {
			t.Error("expected kubernetes-networking in results")
		}
	})

	t.Run("match by tag", func(t *testing.T) {
		results, err := HandleSearchIndex(v, "argocd")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one result")
		}
		found := false
		for _, r := range results {
			if r.Slug == "gitops" {
				found = true
			}
		}
		if !found {
			t.Error("expected gitops in results")
		}
	})

	t.Run("match by slug", func(t *testing.T) {
		results, err := HandleSearchIndex(v, "aks-thoughts")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one result")
		}
		if results[0].Type != "note" {
			t.Errorf("expected type 'note', got %q", results[0].Type)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		results, err := HandleSearchIndex(v, "KUBERNETES")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) < 2 {
			t.Fatalf("expected at least 2 results for KUBERNETES, got %d", len(results))
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		results, err := HandleSearchIndex(v, "nonexistent-query-xyz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected empty results, got %d", len(results))
		}
	})

	t.Run("results are valid JSON-serializable", func(t *testing.T) {
		results, err := HandleSearchIndex(v, "kubernetes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, err := json.Marshal(results)
		if err != nil {
			t.Fatalf("failed to marshal results: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty JSON")
		}
	})
}

// ---------------------------------------------------------------------------
// read_wiki
// ---------------------------------------------------------------------------

func TestHandleReadWiki(t *testing.T) {
	v, _ := setupTestVault(t)

	t.Run("valid slug returns content", func(t *testing.T) {
		content, err := HandleReadWiki(v, "kubernetes-networking")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if content == "" {
			t.Error("expected non-empty content")
		}
		if content != testWikiKubernetesNetworking {
			t.Error("content does not match expected wiki page")
		}
	})

	t.Run("invalid slug returns error", func(t *testing.T) {
		_, err := HandleReadWiki(v, "does-not-exist")
		if err == nil {
			t.Error("expected error for non-existent slug")
		}
	})
}

// ---------------------------------------------------------------------------
// grep_vault
// ---------------------------------------------------------------------------

func TestHandleGrepVault(t *testing.T) {
	v, _ := setupTestVault(t)

	t.Run("finds matches in wiki", func(t *testing.T) {
		results, err := HandleGrepVault(v, "CNI", "wiki")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one match for CNI in wiki")
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		results, err := HandleGrepVault(v, "cni", "wiki")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected case-insensitive match for cni")
		}
	})

	t.Run("default scope is wiki", func(t *testing.T) {
		results, err := HandleGrepVault(v, "CNI", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected matches with default (empty) scope")
		}
	})

	t.Run("scope notes finds note content", func(t *testing.T) {
		results, err := HandleGrepVault(v, "AKS", "notes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected match for AKS in notes scope")
		}
	})

	t.Run("scope raw finds raw content", func(t *testing.T) {
		results, err := HandleGrepVault(v, "scraped", "raw")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected match for scraped in raw scope")
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		results, err := HandleGrepVault(v, "zzz-no-match-zzz", "wiki")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected empty results, got %d", len(results))
		}
	})

	t.Run("results contain file and line info", func(t *testing.T) {
		results, err := HandleGrepVault(v, "CNI", "wiki")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected results")
		}
		r := results[0]
		if r.File == "" {
			t.Error("expected non-empty File field")
		}
		if r.Line == 0 {
			t.Error("expected non-zero Line field")
		}
		if r.Content == "" {
			t.Error("expected non-empty Content field")
		}
	})
}

// ---------------------------------------------------------------------------
// read_note
// ---------------------------------------------------------------------------

func TestHandleReadNote(t *testing.T) {
	v, _ := setupTestVault(t)

	t.Run("valid note path returns content", func(t *testing.T) {
		content, err := HandleReadNote(v, "notes/aks-thoughts.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if content != testNoteContent {
			t.Error("content does not match expected note")
		}
	})

	t.Run("path not starting with notes/ rejected", func(t *testing.T) {
		_, err := HandleReadNote(v, "wiki/gitops.md")
		if err == nil {
			t.Error("expected error for path not starting with notes/")
		}
	})

	t.Run("missing note returns error", func(t *testing.T) {
		_, err := HandleReadNote(v, "notes/nonexistent.md")
		if err == nil {
			t.Error("expected error for missing note")
		}
	})
}

// ---------------------------------------------------------------------------
// read_raw
// ---------------------------------------------------------------------------

func TestHandleReadRaw(t *testing.T) {
	v, _ := setupTestVault(t)

	t.Run("valid filename returns content", func(t *testing.T) {
		content, err := HandleReadRaw(v, "2024-01-15-kubernetes-networking.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if content != testRawContent {
			t.Error("content does not match expected raw file")
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := HandleReadRaw(v, "nonexistent-file.md")
		if err == nil {
			t.Error("expected error for missing raw file")
		}
	})
}

// ---------------------------------------------------------------------------
// related_notes
// ---------------------------------------------------------------------------

func TestHandleRelatedNotes(t *testing.T) {
	v, _ := setupTestVault(t)

	t.Run("extracts links and checks existence", func(t *testing.T) {
		results, err := HandleRelatedNotes(v, "kubernetes-networking")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The wiki page links to: gitops, notes/aks-thoughts, nonexistent-page,
		// and 2024-01-15-kubernetes-networking.md (in the raw source line).
		// We expect at least the three semantic links.
		if len(results) < 3 {
			t.Fatalf("expected at least 3 related entries, got %d: %+v", len(results), results)
		}

		bySlug := make(map[string]RelatedEntry)
		for _, r := range results {
			bySlug[r.Slug] = r
		}

		// gitops exists as wiki/gitops.md
		if entry, ok := bySlug["gitops"]; !ok {
			t.Error("expected gitops in related entries")
		} else {
			if !entry.Exists {
				t.Error("gitops should exist")
			}
			if entry.Title == "" {
				t.Error("expected non-empty title for gitops")
			}
		}

		// notes/aks-thoughts exists as notes/aks-thoughts.md
		if entry, ok := bySlug["notes/aks-thoughts"]; !ok {
			t.Error("expected notes/aks-thoughts in related entries")
		} else if !entry.Exists {
			t.Error("notes/aks-thoughts should exist")
		}

		// nonexistent-page does not exist
		if entry, ok := bySlug["nonexistent-page"]; !ok {
			t.Error("expected nonexistent-page in related entries")
		} else if entry.Exists {
			t.Error("nonexistent-page should not exist")
		}
	})

	t.Run("missing page returns error", func(t *testing.T) {
		_, err := HandleRelatedNotes(v, "totally-missing-slug")
		if err == nil {
			t.Error("expected error for non-existent slug")
		}
	})
}
