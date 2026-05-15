# ADR-010: Supadata API for YouTube Transcripts

## Status

Accepted

## Context

YouTube video URLs saved as Raindrop bookmarks get scraped by Jina Reader, which returns the YouTube page HTML — not the video transcript. We need a way to extract the actual transcript content for YouTube videos so the enrichment pipeline can produce useful wiki pages.

Options considered: pure Go libraries that scrape YouTube's innertube API directly (fragile, break when YouTube changes internals), yt-dlp via os/exec (external binary dependency), or a managed transcript API service.

## Decision

Use the Supadata API (`api.supadata.ai/v1/transcript`) as an external service for YouTube transcript extraction. The `SUPADATA_API_KEY` environment variable is optional — when absent, YouTube URLs fall through to Jina Reader like any other URL.

## Consequences

- Reliable transcript extraction without depending on undocumented YouTube internals
- Handles async transcription (202 + polling) for long videos and AI-generated fallback when no captions exist
- Free tier (100 credits/month) sufficient for personal use
- External dependency — if Supadata is down, YouTube URLs fall back to Jina scraping
- API key is optional, so the feature degrades gracefully
