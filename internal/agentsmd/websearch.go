package agentsmd

import "strings"

// WebSearchStrategy selects how the generated AGENTS.md instructs an agent to
// use its *own* web search relative to the curated vault. LucidVault never
// provides, proxies, or names a search provider (ADR-024); the strategy is
// advisory prose, not code-enforced.
type WebSearchStrategy string

const (
	// StrategyOff omits all web-search guidance: no `## Web Search` section and
	// no web-search bullet under Source Attribution.
	StrategyOff WebSearchStrategy = "off"
	// StrategyFallback (default) reaches for web search only when the vault does
	// not cover the question.
	StrategyFallback WebSearchStrategy = "fallback"
	// StrategyTimeSensitive reaches for web search on time-sensitive questions
	// or when the vault does not cover the question.
	StrategyTimeSensitive WebSearchStrategy = "time-sensitive"
	// StrategyImmediately uses web search and the vault in parallel for any
	// substantive or factual question.
	StrategyImmediately WebSearchStrategy = "immediately"
)

// ParseWebSearchStrategy resolves a raw env value to a WebSearchStrategy. It
// returns the parsed strategy and whether the input was a recognized canonical
// value. Unknown or empty input resolves to StrategyFallback with ok=false so
// the caller can warn on an unknown (non-empty) value.
func ParseWebSearchStrategy(s string) (WebSearchStrategy, bool) {
	switch WebSearchStrategy(s) {
	case StrategyOff, StrategyFallback, StrategyTimeSensitive, StrategyImmediately:
		return WebSearchStrategy(s), true
	default:
		return StrategyFallback, false
	}
}

// sourceAttributionSection returns the `## Source Attribution` section. The
// web-search bullet is included only for non-off modes; StrategyOff omits it so
// that no web-search guidance appears anywhere in AGENTS.md.
func sourceAttributionSection(s WebSearchStrategy) string {
	var b strings.Builder
	b.WriteString("## Source Attribution\n\n")
	b.WriteString("Always state where each piece of an answer came from, and return " +
		"every external\nsource as a **clickable markdown hyperlink** -- `[title](url)` " +
		"-- so the owner can\nopen the full website later. For blended answers, " +
		"attribute each part separately:\n\n")
	b.WriteString("- **Vault** -- you are **required** to include the page's original " +
		"source as a\n  clickable `[title](url)` hyperlink so the owner can open the " +
		"real website. This is\n  never optional, and a bare wiki slug/path is not " +
		"enough on its own. Take the URL\n  verbatim from the page's `source:` " +
		"frontmatter (or its `*Source: [title](url)*`\n  footer line) -- reuse it, " +
		"don't reconstruct it. You may add the wiki path (e.g.\n  " +
		"`wiki/raft-consensus.md`) after the link, but the original URL must always " +
		"appear.\n")
	b.WriteString("- **Model knowledge** -- say so explicitly (\"from my own knowledge, " +
		"not the vault\").\n  No link to invent; do not fabricate one.\n")
	if s != StrategyOff {
		b.WriteString("- **Web search** -- give the result as a `[title](url)` " +
			"hyperlink (and name the\n  provider). Prefer the canonical page URL over " +
			"a redirect.\n")
	}
	noVaultTail := "answering from model knowledge"
	if s != StrategyOff {
		noVaultTail = "answering from the web"
	}
	b.WriteString("- **No vault match** -- if the vault has no relevant content on the " +
		"topic, say so\n  explicitly before answering (e.g. \"Nothing in\n  your vault " +
		"covers this -- " + noVaultTail + "\").\n")
	return b.String()
}

// webSearchFirstDirective returns the mode-specific opening directive for the
// `## Web Search` section. Only this first directive differs between non-off
// modes; the trust-vs-recency, provider-agnostic, and citation rules are shared.
func webSearchFirstDirective(s WebSearchStrategy) string {
	switch s {
	case StrategyTimeSensitive:
		return "- Use your own web search when the question is time-sensitive " +
			"(latest/current/versions/news/prices/dates beyond your saved content), " +
			"or when the vault does not cover it."
	case StrategyImmediately:
		return "- For any substantive or factual question, use your own web search " +
			"and the vault in parallel."
	default: // StrategyFallback
		return "- Reach for your own web search only when the vault does not cover " +
			"the question."
	}
}

// webSearchSection returns the full `## Web Search` section for a non-off mode:
// the mode-specific first directive followed by the shared trust-vs-recency,
// provider-agnostic, and citation rules. It returns "" for StrategyOff.
func webSearchSection(s WebSearchStrategy) string {
	if s == StrategyOff {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Web Search\n\n")
	b.WriteString(webSearchFirstDirective(s))
	b.WriteString("\n")
	// Shared rules -- identical across all non-off modes; only the first
	// directive above changes per mode.
	b.WriteString("- Weight the curated vault (wiki pages) **above** web results by " +
		"default (higher trust). **But** when the question is time-sensitive and a " +
		"web result is newer than the matching wiki page, lead with the web result " +
		"and flag the vault page as possibly outdated.\n")
	b.WriteString("- Use your own configured web search -- LucidVault does not provide one.\n")
	b.WriteString("- Cite every source (see Source Attribution): vault -> its original " +
		"source URL; web -> `[title](url)`, naming the provider.\n")
	return b.String()
}
