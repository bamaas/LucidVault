package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"lucidvault/internal/vault"
	"net/http"
	"strings"
	"time"
)

// maxResponseBytes limits the size of API responses to prevent OOM (10 MB).
const maxResponseBytes = 10 * 1024 * 1024

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	delayMs    int
}

type TagInput struct {
	Content string
	Title   string
	Index   string
	Profile string
}

type EnrichInput struct {
	Content     string
	Index       string
	UserTags    []string
	RawFilename string
	Title       string
	URL         string
	DateSaved   string
	Profile     string
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func NewClient(apiKey, model string, maxRetries, delayMs int) *Client {
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://ollama.com",
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 300 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				ForceAttemptHTTP2:     true,
			},
		},
		maxRetries: maxRetries,
		delayMs:    delayMs,
	}
}

// SetBaseURL overrides the Ollama API endpoint (used in tests).
func (c *Client) SetBaseURL(url string) { c.baseURL = url }

func (c *Client) Enrich(ctx context.Context, input *EnrichInput) (string, error) {
	prompt := buildPrompt(input)

	// Proactive delay between calls (context-aware)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(time.Duration(c.delayMs) * time.Millisecond):
	}

	// TODO: extract shared retry-with-backoff helper to reduce duplication
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		result, statusCode, err := c.callAPI(ctx, prompt)
		if err == nil {
			cleaned := cleanResponse(result)
			if err := validateResponse(cleaned); err != nil {
				if attempt < c.maxRetries {
					slog.Warn("LLM output validation failed, retrying", "attempt", attempt+1, "error", err)
					continue
				}
				slog.Error("LLM output validation failed after retries", "error", err)
				return buildMinimalPage(input), nil
			}
			return cleaned, nil
		}

		// Always propagate context cancellation immediately.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		if statusCode == 429 {
			wait := time.Second * time.Duration(1<<attempt)
			slog.Warn("rate limited, backing off", "attempt", attempt+1, "wait", wait)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		if attempt == c.maxRetries {
			slog.Error("enrichment failed after retries", "error", err)
			return buildMinimalPage(input), nil
		}

		slog.Warn("enrichment call failed, retrying", "attempt", attempt+1, "error", err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second * time.Duration(1<<attempt)):
		}
	}

	return buildMinimalPage(input), nil
}

// SuggestTags generates tags for a note using a lightweight LLM prompt.
// Returns a fallback ["untagged"] on persistent failure instead of an error,
// unless the context is cancelled.
func (c *Client) SuggestTags(ctx context.Context, input *TagInput) ([]string, error) {
	prompt := buildTagPrompt(input)

	// Proactive delay between calls (context-aware)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Duration(c.delayMs) * time.Millisecond):
	}

	// TODO: extract shared retry-with-backoff helper to reduce duplication
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		result, statusCode, err := c.callAPI(ctx, prompt)
		if err == nil {
			tags := parseTags(cleanResponse(result))
			if len(tags) > 0 {
				return tags, nil
			}
			if attempt < c.maxRetries {
				slog.Warn("SuggestTags: empty tags from LLM, retrying", "attempt", attempt+1)
				continue
			}
			slog.Warn("SuggestTags: empty tags after retries, using fallback")
			return []string{"untagged"}, nil
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if statusCode == 429 {
			wait := time.Second * time.Duration(1<<attempt)
			slog.Warn("SuggestTags: rate limited, backing off", "attempt", attempt+1, "wait", wait)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		if attempt == c.maxRetries {
			slog.Warn("SuggestTags: failed after retries, using fallback", "error", err)
			return []string{"untagged"}, nil
		}

		slog.Warn("SuggestTags: call failed, retrying", "attempt", attempt+1, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second * time.Duration(1<<attempt)):
		}
	}

	return []string{"untagged"}, nil
}

// parseTags extracts tags from an LLM response. Handles comma-separated,
// newline-separated, and bullet-list formats.
func parseTags(response string) []string {
	response = strings.TrimSpace(response)
	if response == "" {
		return nil
	}

	var raw []string
	if strings.Contains(response, ",") {
		raw = strings.Split(response, ",")
	} else {
		raw = strings.Split(response, "\n")
	}

	var tags []string
	for _, t := range raw {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "-")
		t = strings.TrimPrefix(t, "*")
		t = strings.TrimSpace(t)
		t = strings.Trim(t, "`\"'")
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func buildTagPrompt(input *TagInput) string {
	var b strings.Builder
	b.WriteString("You are a tagging assistant for a personal knowledge base.\n")
	b.WriteString("Given the note below, suggest 3-5 tags that would help find it later.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Use lowercase, hyphen-separated tags (e.g. distributed-systems, go-lang)\n")
	b.WriteString("- Include domain tags (programming, infrastructure, ai, productivity)\n")
	b.WriteString("- Be specific enough to be useful, not so generic they match everything\n")
	b.WriteString("- Output ONLY the tags as a comma-separated list, nothing else\n\n")

	if input.Profile != "" {
		fmt.Fprintf(&b, "## User Profile\n\n%s\n\n", input.Profile)
	}
	if input.Index != "" {
		fmt.Fprintf(&b, "## Existing Index\n\n%s\n\n", input.Index)
	}

	fmt.Fprintf(&b, "## Note Title\n\n%s\n\n", input.Title)
	fmt.Fprintf(&b, "## Note Content\n\n%s", input.Content)
	return b.String()
}

func (c *Client) callAPI(ctx context.Context, prompt string) (string, int, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("calling ollama API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, fmt.Errorf("ollama API returned %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", resp.StatusCode, fmt.Errorf("parsing response: %w", err)
	}

	return chatResp.Message.Content, resp.StatusCode, nil
}

func cleanResponse(content string) string {
	content = strings.TrimSpace(content)
	// Strip markdown code fences if LLM wraps output
	if strings.HasPrefix(content, "```markdown") {
		content = strings.TrimPrefix(content, "```markdown")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	return content
}

func validateResponse(content string) error {
	if !strings.HasPrefix(content, "---") {
		return fmt.Errorf("missing YAML frontmatter")
	}

	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return fmt.Errorf("invalid frontmatter structure")
	}

	frontmatter := parts[1]
	if !strings.Contains(frontmatter, "title:") {
		return fmt.Errorf("missing title in frontmatter")
	}
	if !strings.Contains(frontmatter, "tags:") {
		return fmt.Errorf("missing tags in frontmatter")
	}

	body := parts[2]
	if !strings.Contains(body, "## Summary") {
		return fmt.Errorf("missing Summary section")
	}
	if !strings.Contains(body, "## Key Takeaways") {
		return fmt.Errorf("missing Key Takeaways section")
	}

	wordCount := len(strings.Fields(body))
	if wordCount > 1200 {
		slog.Warn("wiki page body exceeds 1200 word budget", "words", wordCount)
	}

	return nil
}

func buildMinimalPage(input *EnrichInput) string {
	title := input.Title
	if title == "" {
		title = input.URL
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", vault.QuoteYAMLValue(title))
	fmt.Fprintf(&b, "source: %s\n", vault.QuoteYAMLValue(input.URL))
	fmt.Fprintf(&b, "date_saved: %s\n", input.DateSaved)
	b.WriteString("tags:\n")
	for _, t := range input.UserTags {
		fmt.Fprintf(&b, "  - %s\n", t)
	}
	b.WriteString("type: bookmark\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", title)
	b.WriteString("## Summary\n\n")
	b.WriteString("*Enrichment failed. Minimal page created from bookmark metadata.*\n\n")
	b.WriteString("## Key Takeaways\n\n")
	b.WriteString("- Content could not be enriched automatically\n\n")
	b.WriteString("## Related\n\n")
	b.WriteString("*No related notes yet*\n\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "*Source: [%s](%s) | Raw: [[%s]]*\n", input.URL, input.URL, input.RawFilename)
	return b.String()
}

func buildPrompt(input *EnrichInput) string {
	tagsStr := strings.Join(input.UserTags, ", ")

	return fmt.Sprintf(`You are enriching a web article for a personal knowledge base called a "second brain."
Your job is to transform raw scraped content into a structured, linkable note that
captures what matters and connects it to existing knowledge.

## Input

You will receive:
1. **Content** — The full scraped markdown from the web page
2. **Index** — A list of existing wiki pages in the knowledge base (title + path)
3. **User Tags** — Tags the user assigned when saving the bookmark
4. **Raw Filename** — The path to the raw scrape file for backlinking
5. **URL** — The original URL
6. **Date Saved** — When the bookmark was saved
7. **Profile** — The user's interests and preferences. Use this to prioritize what to extract and how to frame takeaways.

## Your Task

Create a wiki page that matches its depth to the source — be thorough on complex articles, concise on simple ones. Specifically:
1. Summarize the article's insights and important details (depth proportional to source complexity)
2. Extract concepts, tools, people, or ideas worth remembering
3. Link to existing wiki pages where connections exist
4. Tag appropriately for future retrieval

## Output Format

Return ONLY valid markdown with YAML frontmatter:

---
title: {Canonical title — clear, searchable, not clickbait}
source: {original URL}
date_saved: {YYYY-MM-DD}
tags:
  - {tag 1}
  - {tag 2}
  - {tag 3-5 max}
type: bookmark
---

# {Title}

## Summary
{A thorough summary of up to 600 words capturing the core insights, arguments, and important details. Not "this article discusses X" — instead, state the insights directly. Cover the main thesis, supporting evidence, and significant nuances. The summary should be useful as a standalone reference without needing to read the original article.

For long-form or complex articles, you MAY use ### sub-headings within the Summary to organize it (e.g. ### Background, ### Core Argument, ### Implications). For shorter articles, use plain prose without sub-headings.}

## Key Takeaways
{Typically 5-10 points. Each point should be 1-2 sentences. Include as many as the content warrants, but keep each concise.}
- {most important point}
- {second most important point}
- {more points as needed — practical, actionable, or thought-provoking}

## Concepts
{OPTIONAL — only include if the article introduces or defines specific terms, tools, or frameworks worth remembering. Max 5 entries. If not warranted, omit this entire section including the heading.}
- **{Term}** — {1-sentence definition or explanation}
- **{Term}** — {1-sentence definition or explanation}

## Notable Quotes
{OPTIONAL — only include if the article contains 1-2 particularly well-stated or memorable lines. Max 2 quotes. If not warranted, omit this entire section including the heading. If you cannot recall the exact wording, omit the quote rather than paraphrasing.}
> "{exact quote from the source}"

## Related
- [[{existing-wiki-page}]] — {brief reason for connection}
- [[{existing-wiki-page}]] — {brief reason for connection}

---

*Source: [{title}]({url}) | Raw: [[{raw-filename}]]*

## Tagging Rules

- Use lowercase, hyphen-separated tags: kubernetes, distributed-systems, go-lang
- Include domain tags: infrastructure, programming, ai, productivity, research
- Include type tags when relevant: tutorial, reference, opinion, case-study
- Keep the user's original tags unless clearly wrong
- Add 1-3 additional tags that improve findability

## Linking Rules

- Look at the Index provided. Find 1-3 existing pages that are genuinely related.
- Only link if there's a real conceptual connection, not just shared keywords.
- If no relevant pages exist, output the Related section with: *No related notes yet*
- Do NOT invent pages that don't exist in the Index.

## Content Handling

- If the article is a tutorial: extract the key technique or pattern, and include 1-2 concrete examples or code snippets if they illustrate the core idea
- If the article is opinion/analysis: extract the core argument and supporting points
- If the article is reference/documentation: extract the key concepts and notable API patterns, not the full API
- If the article is news: extract what's significant and why it matters
- Ignore ads, navigation, footers, "subscribe to newsletter" prompts
- If content is very long, prioritize the main argument over details
- Go deeper on topics that align with the user's Profile interests — allocate more of the word budget to these topics, but do not exceed the overall budget

## Compression Rule

Remove noise (ads, filler, repetition) but preserve meaningful detail.
For long or dense content, focus depth on the main argument and key supporting points — cut tangential details rather than compressing everything equally.
The result should be useful as a standalone reference within the word budget.

## Anti-Hallucination Rule

Only extract concepts explicitly present or strongly implied in the source.
Do not invent frameworks, ideas, or relationships.
If unsure whether a connection exists, omit it.

## Budget

The entire page body (everything after frontmatter) must stay under 1200 words.
Budget guideline: Summary up to 600 words, Key Takeaways typically 5-10 points (1-2 sentences each), Concepts max 5, Notable Quotes max 2.
Omit optional sections (Concepts, Notable Quotes) entirely — including their headings — if they would push over budget or add no value.

## Quality Bar

- Summary should be useful even without reading the full article
- Key takeaways should be things you'd want to remember in 6 months
- Tags should make this findable via search
- Links should create real knowledge graph edges
- Optional sections should earn their place — omit if they add no real value

---

## Content

%s

## Index

%s

## User Tags

%s

## Raw Filename

%s

## URL

%s

## Date Saved

%s

## Profile

%s`, input.Content, input.Index, tagsStr, input.RawFilename, input.URL, input.DateSaved, input.Profile)
}
