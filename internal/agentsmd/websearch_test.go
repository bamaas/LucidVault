package agentsmd

import (
	"strings"
	"testing"
)

// providerNames are search-vendor names that must never appear in any generated
// AGENTS.md, in any strategy mode. The generated prose is provider-agnostic: it
// instructs the agent to use its *own* configured web search (ADR-024).
var providerNames = []string{
	"tavily", "exa", "brave", "firecrawl", "fastcrw", "scavio", "serpapi",
	"bing", "duckduckgo", "perplexity", "kagi", "google search",
}

// assertNoProvider fails if the generated output names any search vendor.
func assertNoProvider(t *testing.T, doc string) {
	t.Helper()
	lower := strings.ToLower(doc)
	for _, name := range providerNames {
		if strings.Contains(lower, name) {
			t.Errorf("generated AGENTS.md names search provider %q; output must be provider-agnostic", name)
		}
	}
}

// TestParseWebSearchStrategy verifies the four canonical values parse to their
// typed enum, and that unknown/empty input falls back to StrategyFallback. The
// boolean return distinguishes "recognized" from "fell back" so the caller can
// warn on an unknown non-empty value.
func TestParseWebSearchStrategy(t *testing.T) {
	tests := []struct {
		in     string
		want   WebSearchStrategy
		wantOK bool
	}{
		{in: "off", want: StrategyOff, wantOK: true},
		{in: "fallback", want: StrategyFallback, wantOK: true},
		{in: "time-sensitive", want: StrategyTimeSensitive, wantOK: true},
		{in: "immediately", want: StrategyImmediately, wantOK: true},
		// Empty falls back silently (not an error worth warning about).
		{in: "", want: StrategyFallback, wantOK: false},
		// Unknown non-empty falls back, but is NOT recognized (caller warns).
		{in: "nope", want: StrategyFallback, wantOK: false},
		{in: "FALLBACK", want: StrategyFallback, wantOK: false},
		{in: "Off", want: StrategyFallback, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseWebSearchStrategy(tt.in)
			if got != tt.want {
				t.Errorf("ParseWebSearchStrategy(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if ok != tt.wantOK {
				t.Errorf("ParseWebSearchStrategy(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
		})
	}
}

// TestGenerate_StrategyOff_OmitsAllWebSearch verifies that the off mode emits an
// AGENTS.md with no `## Web Search` section AND no web-search attribution bullet:
// the phrase "web search" must be absent entirely (acceptance criterion 3).
func TestGenerate_StrategyOff_OmitsAllWebSearch(t *testing.T) {
	result := Generate(nil, VaultStats{}, StrategyOff)

	if strings.Contains(result, "## Web Search") {
		t.Error("off mode must not emit a '## Web Search' section")
	}
	// No web-search guidance anywhere -- section or attribution bullet.
	if strings.Contains(strings.ToLower(result), "web search") {
		t.Error("off mode must contain no 'web search' guidance anywhere in AGENTS.md")
	}

	// Source Attribution must still render (it covers vault + model knowledge),
	// but its web-search bullet is gone. (Match the bullet marker, not a bare
	// "web" -- legitimate prose like "website" must not trip the assertion.)
	sa := sectionBody(result, "## Source Attribution")
	if sa == "" {
		t.Fatal("off mode should still emit the '## Source Attribution' section")
	}
	if strings.Contains(sa, "**Web search**") {
		t.Error("off mode must omit the web-search bullet from Source Attribution")
	}

	assertNoProvider(t, result)
}

// TestGenerate_StrategyFallback verifies the default mode's first directive:
// reach for the agent's own web search only when the vault does not cover the
// question.
func TestGenerate_StrategyFallback(t *testing.T) {
	result := Generate(nil, VaultStats{}, StrategyFallback)

	ws := sectionBody(result, "## Web Search")
	if ws == "" {
		t.Fatal("fallback mode must emit a '## Web Search' section")
	}
	// First directive: web search only when the vault does not cover the question.
	if !strings.Contains(ws, "does not cover the question") {
		t.Error("fallback first directive must say to use web search only when the vault does not cover the question")
	}
	assertSharedWebSearchRules(t, result)
	assertNoProvider(t, result)
}

// TestGenerate_StrategyTimeSensitive verifies the time-sensitive mode's first
// directive: use web search when the question is time-sensitive or uncovered.
func TestGenerate_StrategyTimeSensitive(t *testing.T) {
	result := Generate(nil, VaultStats{}, StrategyTimeSensitive)

	ws := sectionBody(result, "## Web Search")
	if ws == "" {
		t.Fatal("time-sensitive mode must emit a '## Web Search' section")
	}
	if !strings.Contains(ws, "time-sensitive") {
		t.Error("time-sensitive first directive must mention time-sensitive questions")
	}
	// The directive enumerates time-sensitive signals.
	if !strings.Contains(ws, "latest") || !strings.Contains(ws, "prices") {
		t.Error("time-sensitive first directive should enumerate signals like latest/current/versions/news/prices/dates")
	}
	if !strings.Contains(ws, "does not cover it") {
		t.Error("time-sensitive first directive must also cover the uncovered-vault case")
	}
	assertSharedWebSearchRules(t, result)
	assertNoProvider(t, result)
}

// TestGenerate_StrategyImmediately verifies the most-aggressive mode's first
// directive: for any substantive/factual question, use web search and the vault
// in parallel.
func TestGenerate_StrategyImmediately(t *testing.T) {
	result := Generate(nil, VaultStats{}, StrategyImmediately)

	ws := sectionBody(result, "## Web Search")
	if ws == "" {
		t.Fatal("immediately mode must emit a '## Web Search' section")
	}
	if !strings.Contains(ws, "substantive or factual question") {
		t.Error("immediately first directive must mention substantive or factual questions")
	}
	if !strings.Contains(ws, "in parallel") {
		t.Error("immediately first directive must say to use web search and the vault in parallel")
	}
	assertSharedWebSearchRules(t, result)
	assertNoProvider(t, result)
}

// assertSharedWebSearchRules verifies the rules every non-off mode appends to the
// Web Search section: trust-vs-recency, provider-agnostic ("your own"), and
// citation. These are identical across fallback/time-sensitive/immediately.
func assertSharedWebSearchRules(t *testing.T, doc string) {
	t.Helper()
	ws := sectionBody(doc, "## Web Search")
	if ws == "" {
		t.Fatal("missing '## Web Search' section")
	}
	// Trust: the curated vault is weighted above web results by default.
	if !strings.Contains(ws, "above") {
		t.Error("missing trust rule: weight the curated vault above web results by default")
	}
	// Recency override: a newer web result on a time-sensitive question leads,
	// and the vault page is flagged as possibly outdated.
	if !strings.Contains(ws, "newer") {
		t.Error("missing recency-override rule (newer web result leads on time-sensitive questions)")
	}
	if !strings.Contains(ws, "outdated") {
		t.Error("missing 'flag the vault page as possibly outdated' guidance in recency override")
	}
	// Provider-agnostic: the agent uses its OWN configured web search; LucidVault
	// does not provide one.
	if !strings.Contains(ws, "your own") {
		t.Error("missing provider-agnostic guidance (use your own configured web search)")
	}
	if !strings.Contains(ws, "LucidVault does not provide") {
		t.Error("missing 'LucidVault does not provide a web search' clarification")
	}
	// Citation: every source must be cited.
	if !strings.Contains(ws, "Cite") && !strings.Contains(ws, "cite") {
		t.Error("missing citation rule in Web Search section")
	}
}

// TestGenerate_AllModes_NameNoProvider is a belt-and-suspenders sweep: across
// every strategy mode (including off), the generated AGENTS.md must name no
// search vendor (acceptance criterion 4).
func TestGenerate_AllModes_NameNoProvider(t *testing.T) {
	for _, s := range []WebSearchStrategy{StrategyOff, StrategyFallback, StrategyTimeSensitive, StrategyImmediately} {
		t.Run(string(s), func(t *testing.T) {
			assertNoProvider(t, Generate(nil, VaultStats{}, s))
		})
	}
}

// TestGenerate_NonOffModes_ShareIdenticalSharedRules verifies that only the first
// directive differs between non-off modes; the trust-vs-recency, provider-agnostic,
// and citation rules are byte-identical across fallback/time-sensitive/immediately.
func TestGenerate_NonOffModes_ShareIdenticalSharedRules(t *testing.T) {
	modes := []WebSearchStrategy{StrategyFallback, StrategyTimeSensitive, StrategyImmediately}
	// Extract the shared-rules tail (everything from the "above" trust rule
	// onward) and assert it is identical across modes.
	tail := func(s WebSearchStrategy) string {
		ws := sectionBody(Generate(nil, VaultStats{}, s), "## Web Search")
		idx := strings.Index(ws, "above")
		if idx < 0 {
			t.Fatalf("mode %q: missing shared trust rule", s)
		}
		return ws[idx:]
	}
	want := tail(StrategyFallback)
	for _, s := range modes[1:] {
		if got := tail(s); got != want {
			t.Errorf("mode %q shared rules differ from fallback; only the first directive should change\nfallback tail:\n%s\n%s tail:\n%s", s, want, s, got)
		}
	}
}
