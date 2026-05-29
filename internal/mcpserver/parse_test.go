package mcpserver

import (
	"testing"
)

func TestParseIndexEntry(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		want     *IndexEntry
		wantNil  bool
	}{
		{
			name: "wiki entry with tags",
			line: "- [[kubernetes-networking]] — Kubernetes Networking Deep Dive [kubernetes, networking, cni]",
			want: &IndexEntry{
				Slug:  "kubernetes-networking",
				Title: "Kubernetes Networking Deep Dive",
				Tags:  []string{"kubernetes", "networking", "cni"},
				Type:  "wiki",
			},
		},
		{
			name: "note entry with notes/ prefix",
			line: "- [[notes/aks-thoughts]] — AKS Thoughts [azure, kubernetes]",
			want: &IndexEntry{
				Slug:  "notes/aks-thoughts",
				Title: "AKS Thoughts",
				Tags:  []string{"azure", "kubernetes"},
				Type:  "note",
			},
		},
		{
			name: "single tag",
			line: "- [[gitops]] — GitOps with ArgoCD [gitops]",
			want: &IndexEntry{
				Slug:  "gitops",
				Title: "GitOps with ArgoCD",
				Tags:  []string{"gitops"},
				Type:  "wiki",
			},
		},
		{
			name:    "header line returns nil",
			line:    "# Wiki Index",
			wantNil: true,
		},
		{
			name:    "empty line returns nil",
			line:    "",
			wantNil: true,
		},
		{
			name:    "non-entry line returns nil",
			line:    "Last updated: 2024-01-15",
			wantNil: true,
		},
		{
			name:    "section header returns nil",
			line:    "## Pages",
			wantNil: true,
		},
		{
			name: "slug with multiple hyphens",
			line: "- [[my-long-hyphenated-slug]] — Some Title [tag1, tag2]",
			want: &IndexEntry{
				Slug:  "my-long-hyphenated-slug",
				Title: "Some Title",
				Tags:  []string{"tag1", "tag2"},
				Type:  "wiki",
			},
		},
		{
			name: "entry with no tags",
			line: "- [[simple]] — Simple Page []",
			want: &IndexEntry{
				Slug:  "simple",
				Title: "Simple Page",
				Tags:  []string{},
				Type:  "wiki",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseIndexEntry(tt.line)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil result, got nil")
			}

			if got.Slug != tt.want.Slug {
				t.Errorf("Slug = %q, want %q", got.Slug, tt.want.Slug)
			}
			if got.Title != tt.want.Title {
				t.Errorf("Title = %q, want %q", got.Title, tt.want.Title)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if len(got.Tags) != len(tt.want.Tags) {
				t.Fatalf("Tags length = %d, want %d (got %v)", len(got.Tags), len(tt.want.Tags), got.Tags)
			}
			for i, tag := range got.Tags {
				if tag != tt.want.Tags[i] {
					t.Errorf("Tags[%d] = %q, want %q", i, tag, tt.want.Tags[i])
				}
			}
		})
	}
}

func TestParseWikiLinks(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "multiple links",
			content: "See [[gitops]] for deployment and [[kubernetes-networking]] for networking.",
			want:    []string{"gitops", "kubernetes-networking"},
		},
		{
			name:    "links in different sections",
			content: "# Header\n\n[[link-one]]\n\n## Another\n\n[[link-two]]",
			want:    []string{"link-one", "link-two"},
		},
		{
			name:    "note link with prefix",
			content: "Related: [[notes/aks-thoughts]]",
			want:    []string{"notes/aks-thoughts"},
		},
		{
			name:    "no links",
			content: "This is plain text with no wiki links.",
			want:    []string{},
		},
		{
			name:    "empty content",
			content: "",
			want:    []string{},
		},
		{
			name:    "link at start and end of line",
			content: "[[start-link]] some text [[end-link]]",
			want:    []string{"start-link", "end-link"},
		},
		{
			name:    "link with raw filename pattern filtered out",
			content: "*Source: [[2024-01-15-kubernetes-networking.md]]*",
			want:    []string{},
		},
		// --- Hardening: fenced code blocks ---
		{
			name:    "skip links inside backtick fenced code block",
			content: "Before [[real-link]]\n```\n[[code-link]]\n```\nAfter [[another-real]]",
			want:    []string{"real-link", "another-real"},
		},
		{
			name:    "skip links inside tilde fenced code block",
			content: "Before [[real-link]]\n~~~\n[[code-link]]\n~~~\nAfter [[another-real]]",
			want:    []string{"real-link", "another-real"},
		},
		{
			name:    "skip links inside fenced code block with language",
			content: "```markdown\n[[code-link]]\n```\n[[real-link]]",
			want:    []string{"real-link"},
		},
		{
			name:    "multiple code blocks",
			content: "[[a]]\n```\n[[b]]\n```\n[[c]]\n~~~\n[[d]]\n~~~\n[[e]]",
			want:    []string{"a", "c", "e"},
		},
		// --- Hardening: inline code ---
		{
			name:    "skip links inside inline code",
			content: "See `[[not-a-link]]` but [[real-link]] is valid",
			want:    []string{"real-link"},
		},
		{
			name:    "skip links inside double backtick inline code",
			content: "See ``[[not-a-link]]`` but [[real-link]] is valid",
			want:    []string{"real-link"},
		},
		// --- Hardening: frontmatter ---
		{
			name:    "skip links in YAML frontmatter",
			content: "---\nrelated: \"[[frontmatter-link]]\"\ntags: [test]\n---\n\n[[body-link]]",
			want:    []string{"body-link"},
		},
		{
			name:    "no frontmatter still works",
			content: "[[normal-link]] in content without frontmatter",
			want:    []string{"normal-link"},
		},
		// --- Hardening: pipe syntax ---
		{
			name:    "pipe syntax takes slug before pipe",
			content: "See [[kubernetes-networking|K8s Networking]] for details",
			want:    []string{"kubernetes-networking"},
		},
		{
			name:    "pipe syntax mixed with regular links",
			content: "[[regular-link]] and [[piped|Display Name]]",
			want:    []string{"regular-link", "piped"},
		},
		// --- Hardening: .md filtering ---
		{
			name:    "mixed md and non-md links",
			content: "[[valid-slug]] and [[raw-file.md]] and [[another-valid]]",
			want:    []string{"valid-slug", "another-valid"},
		},
		// --- Hardening: deduplication ---
		{
			name:    "duplicate links deduplicated",
			content: "[[same-link]] appears twice: [[same-link]]",
			want:    []string{"same-link"},
		},
		{
			name:    "duplicate after pipe resolution",
			content: "[[slug]] and [[slug|Different Display]]",
			want:    []string{"slug"},
		},
		// --- Combined scenarios ---
		{
			name:    "all hardening combined",
			content: "---\nrelated: \"[[fm-link]]\"\n---\n\n[[real]] and [[piped|Name]]\n```\n[[in-code]]\n```\nSee `[[inline]]` and [[real]] and [[raw.md]]",
			want:    []string{"real", "piped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWikiLinks(tt.content)

			if got == nil {
				got = []string{}
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d links %v, want %d links %v", len(got), got, len(tt.want), tt.want)
			}
			for i, link := range got {
				if link != tt.want[i] {
					t.Errorf("link[%d] = %q, want %q", i, link, tt.want[i])
				}
			}
		})
	}
}

func TestParseFrontmatterTitle(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "quoted title",
			content: `---
title: "Kubernetes Networking Deep Dive"
source: "https://example.com"
---

# Content`,
			want: "Kubernetes Networking Deep Dive",
		},
		{
			name: "unquoted title",
			content: `---
title: Kubernetes Networking
date_saved: 2024-01-15
---

# Content`,
			want: "Kubernetes Networking",
		},
		{
			name: "single quoted title",
			content: `---
title: 'Some Title'
---

# Content`,
			want: "Some Title",
		},
		{
			name:    "no frontmatter",
			content: "# Just a heading\n\nSome content.",
			want:    "",
		},
		{
			name: "frontmatter without title",
			content: `---
source: "https://example.com"
date_saved: 2024-01-15
---

# Content`,
			want: "",
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name: "title with colons",
			content: `---
title: "Key: Value Pairs in YAML"
---

# Content`,
			want: "Key: Value Pairs in YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFrontmatterTitle(tt.content)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
