package enrich

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	delayMs    int
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

func (c *Client) Enrich(input *EnrichInput) (string, error) {
	prompt := buildPrompt(input)

	// Proactive delay between calls
	time.Sleep(time.Duration(c.delayMs) * time.Millisecond)

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		result, statusCode, err := c.callAPI(prompt)
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

		if statusCode == 429 {
			wait := time.Second * time.Duration(1<<attempt)
			slog.Warn("rate limited, backing off", "attempt", attempt+1, "wait", wait)
			time.Sleep(wait)
			continue
		}

		if attempt == c.maxRetries {
			slog.Error("enrichment failed after retries", "error", err)
			return buildMinimalPage(input), nil
		}

		slog.Warn("enrichment call failed, retrying", "attempt", attempt+1, "error", err)
		time.Sleep(time.Second * time.Duration(1<<attempt))
	}

	return buildMinimalPage(input), nil
}

func (c *Client) callAPI(prompt string) (string, int, error) {
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

	req, err := http.NewRequest("POST", c.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("calling ollama API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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

	return nil
}

func buildMinimalPage(input *EnrichInput) string {
	title := input.Title
	if title == "" {
		title = input.URL
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %q\n", title)
	fmt.Fprintf(&b, "source: %q\n", input.URL)
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

Create a wiki page that:
1. Distills the article into its essential insights (not a summary of everything)
2. Extracts concepts, tools, people, or ideas worth remembering
3. Links to existing wiki pages where connections exist
4. Tags appropriately for future retrieval

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
{2-3 sentences capturing the core insight or argument. Not "this article discusses X" — instead, state the insight directly.}

## Key Takeaways
- {most important point}
- {second most important point}
- {third point — practical, actionable, or thought-provoking}
- {optional fourth point}
- {optional fifth point}

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

- If the article is a tutorial: extract the key technique or pattern, not step-by-step instructions
- If the article is opinion/analysis: extract the core argument and supporting points
- If the article is reference/documentation: extract the key concepts, not the full API
- If the article is news: extract what's significant and why it matters
- Ignore ads, navigation, footers, "subscribe to newsletter" prompts
- If content is very long, prioritize the main argument over details

## Compression Rule

If the content is verbose or repetitive, aggressively compress it.
Prefer losing detail over keeping noise.
A good wiki page is shorter than the source, not longer.

## Anti-Hallucination Rule

Only extract concepts explicitly present or strongly implied in the source.
Do not invent frameworks, ideas, or relationships.
If unsure whether a connection exists, omit it.

## Quality Bar

- Summary should be useful even without reading the full article
- Key takeaways should be things you'd want to remember in 6 months
- Tags should make this findable via search
- Links should create real knowledge graph edges

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
