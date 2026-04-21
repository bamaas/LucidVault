# ADR-005: Jina Reader over Custom Scraping

## Status
Accepted

## Context
Need to convert web pages to clean markdown. Options: custom scraper (colly, chromedp), Jina Reader API, or Mozilla Readability.

## Decision
Use Jina Reader (`r.jina.ai/{url}`) — one HTTP GET returns clean markdown.

## Consequences
- Single HTTP call, no browser automation
- Handles JavaScript-rendered pages, PDFs
- Free tier sufficient for personal use (~20 RPM)
- External dependency — if Jina goes down, scraping fails (fallback to minimal page)
- No control over parsing quality
