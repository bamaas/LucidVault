package vault

import (
	"strings"
	"testing"
)

func TestQuoteYAMLValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain value", in: "hello world", want: "hello world"},
		{name: "empty string", in: "", want: `""`},
		{name: "colon in value", in: "ArgoCD: ApplicationSets", want: `"ArgoCD: ApplicationSets"`},
		{name: "hash in value", in: "C# Programming", want: `"C# Programming"`},
		{name: "ampersand in value", in: "foo & bar", want: `"foo & bar"`},
		{name: "multiple special chars", in: "ArgoCD: Apps & Sets", want: `"ArgoCD: Apps & Sets"`},
		{name: "already double-quoted", in: `"already quoted"`, want: `"already quoted"`},
		{name: "already single-quoted", in: `'already quoted'`, want: `'already quoted'`},
		{name: "contains double quote", in: `say "hello"`, want: `"say \"hello\""`},
		{name: "starts with dash", in: "- list item", want: `"- list item"`},
		{name: "starts with space", in: " leading space", want: `" leading space"`},
		{name: "square bracket", in: "[array]", want: `"[array]"`},
		{name: "curly bracket", in: "{object}", want: `"{object}"`},
		{name: "asterisk", in: "glob*", want: `"glob*"`},
		{name: "exclamation", in: "!important", want: `"!important"`},
		{name: "pipe", in: "a | b", want: `"a | b"`},
		{name: "url with special chars", in: "https://example.com/path?q=1&a=2", want: `"https://example.com/path?q=1&a=2"`},
		{name: "plain date", in: "2024-01-15", want: "2024-01-15"},
		{name: "plain word", in: "bookmark", want: "bookmark"},
		{name: "comma in value", in: "one, two", want: `"one, two"`},
		{name: "percent", in: "100% done", want: `"100% done"`},
		{name: "at sign", in: "user@host", want: `"user@host"`},
		{name: "backtick", in: "use `code`", want: "\"use `code`\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteYAMLValue(tt.in)
			if got != tt.want {
				t.Errorf("QuoteYAMLValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatRawContent_QuotesSpecialChars(t *testing.T) {
	got := FormatRawContent(
		"ArgoCD: ApplicationSets & Generators",
		"https://example.com/path?a=1&b=2",
		"2024-01-15",
		[]string{"kubernetes"},
		"body content",
	)

	if !strings.Contains(got, `title: "ArgoCD: ApplicationSets & Generators"`) {
		t.Errorf("title with special chars not quoted in FormatRawContent output:\n%s", got)
	}
	if !strings.Contains(got, `source: "https://example.com/path?a=1&b=2"`) {
		t.Errorf("source URL not quoted in FormatRawContent output:\n%s", got)
	}
}

func TestFormatRawContent_PlainTitleNotQuoted(t *testing.T) {
	got := FormatRawContent(
		"Simple Title",
		"https://example.com",
		"2024-01-15",
		nil,
		"body",
	)

	if !strings.Contains(got, "title: Simple Title\n") {
		t.Errorf("plain title should not be quoted, got:\n%s", got)
	}
	// URLs always contain ':' so they're always quoted
	if !strings.Contains(got, `source: "https://example.com"`) {
		t.Errorf("source URL should be quoted (contains colon), got:\n%s", got)
	}
}

func TestFixFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no frontmatter",
			in:   "# Hello\nWorld",
			want: "# Hello\nWorld",
		},
		{
			name: "unquoted title with colon",
			in:   "---\ntitle: ArgoCD: ApplicationSets & More\ntype: bookmark\n---\n\n# Body",
			want: "---\ntitle: \"ArgoCD: ApplicationSets & More\"\ntype: bookmark\n---\n\n# Body",
		},
		{
			name: "unquoted source URL",
			in:   "---\ntitle: Safe Title\nsource: https://example.com/path?q=1&a=2\n---\n\nBody",
			want: "---\ntitle: Safe Title\nsource: \"https://example.com/path?q=1&a=2\"\n---\n\nBody",
		},
		{
			name: "already quoted stays unchanged",
			in:   "---\ntitle: \"Already Quoted: Value\"\ntype: bookmark\n---\n\nBody",
			want: "---\ntitle: \"Already Quoted: Value\"\ntype: bookmark\n---\n\nBody",
		},
		{
			name: "single-quoted stays unchanged",
			in:   "---\ntitle: 'Single Quoted: Value'\ntype: bookmark\n---\n\nBody",
			want: "---\ntitle: 'Single Quoted: Value'\ntype: bookmark\n---\n\nBody",
		},
		{
			name: "tags list preserved",
			in:   "---\ntitle: Test\ntags:\n  - kubernetes\n  - go-lang\ntype: bookmark\n---\n\nBody",
			want: "---\ntitle: Test\ntags:\n  - kubernetes\n  - go-lang\ntype: bookmark\n---\n\nBody",
		},
		{
			name: "plain values not quoted",
			in:   "---\ntitle: Simple Title\ntype: bookmark\ndate_saved: 2024-01-15\n---\n\nBody",
			want: "---\ntitle: Simple Title\ntype: bookmark\ndate_saved: 2024-01-15\n---\n\nBody",
		},
		{
			name: "hash in title",
			in:   "---\ntitle: C# Programming Guide\ntype: bookmark\n---\n\nBody",
			want: "---\ntitle: \"C# Programming Guide\"\ntype: bookmark\n---\n\nBody",
		},
		{
			name: "incomplete frontmatter passthrough",
			in:   "---\ntitle: foo\nno closing fence",
			want: "---\ntitle: foo\nno closing fence",
		},
		{
			name: "multiple values need quoting",
			in:   "---\ntitle: ArgoCD: ApplicationSets\nsource: https://example.com?a=1&b=2\ntype: bookmark\n---\n\nBody",
			want: "---\ntitle: \"ArgoCD: ApplicationSets\"\nsource: \"https://example.com?a=1&b=2\"\ntype: bookmark\n---\n\nBody",
		},
		{
			name: "value with internal double quotes",
			in:   "---\ntitle: Using \"kubectl\" with ArgoCD: Tips\ntype: bookmark\n---\n\nBody",
			want: "---\ntitle: \"Using \\\"kubectl\\\" with ArgoCD: Tips\"\ntype: bookmark\n---\n\nBody",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FixFrontmatter(tt.in)
			if got != tt.want {
				t.Errorf("FixFrontmatter() mismatch\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}
