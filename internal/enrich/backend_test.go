package enrich_test

// Black-box tests for the OpenAI-compatible Enrichment Backend contract
// (docs/adr/031-openai-compatible-enrichment-backend.md,
// docs/plans/plan-openai-compatible-enrichment.md).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lucidvault/internal/enrich"
)

const validWikiPage = `---
title: "Test"
source: "http://example.com"
date_saved: 2024-01-01
tags:
  - test
type: bookmark
---

# Test

## Summary
This is a test summary.

## Key Takeaways
- Test takeaway
`

// minimalPageMarker is the text the minimal fallback page carries.
const minimalPageMarker = "Enrichment failed"

// secretKey must never appear in any error message.
const secretKey = "sk-secret-do-not-leak-4242"

// completionJSON builds an OpenAI-compatible chat completions response whose
// choices[0].message.content is content.
func completionJSON(content string) []byte {
	return mustJSON(map[string]any{
		"id":     "chatcmpl-test",
		"object": "chat.completion",
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
	})
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func enrichInput() *enrich.EnrichInput {
	return &enrich.EnrichInput{
		Title:     "Shape Title",
		URL:       "http://example.com/shape-check",
		DateSaved: "2024-01-01",
	}
}

func tagInput() *enrich.TagInput {
	return &enrich.TagInput{
		Title:   "Shape Note",
		Content: "# Shape Note\n\nunique-shape-note-content",
	}
}

// operation abstracts over Enrich and SuggestTags so each contract is checked
// for both entry points. It returns the produced output (page or joined tags).
type operation struct {
	name string
	// reply is a model output that the operation accepts as valid.
	reply string
	// promptMarker is a string from the input that must appear in the prompt.
	promptMarker string
	call         func(ctx context.Context, c *enrich.Client) (string, error)
}

func operations() []operation {
	return []operation{
		{
			name:         "Enrich",
			reply:        validWikiPage,
			promptMarker: "http://example.com/shape-check",
			call: func(ctx context.Context, c *enrich.Client) (string, error) {
				return c.Enrich(ctx, enrichInput())
			},
		},
		{
			name:         "SuggestTags",
			reply:        "golang, testing, tdd",
			promptMarker: "unique-shape-note-content",
			call: func(ctx context.Context, c *enrich.Client) (string, error) {
				tags, err := c.SuggestTags(ctx, tagInput())
				return strings.Join(tags, ","), err
			},
		},
	}
}

// --- Request shape ---

type capturedRequest struct {
	method      string
	path        string
	contentType string
	authValues  []string
	body        []byte
}

func TestClient_RequestShape(t *testing.T) {
	baseSuffixes := []struct {
		name     string
		suffix   string
		wantPath string
	}{
		{name: "root base URL", suffix: "", wantPath: "/chat/completions"},
		{name: "base URL ending in /v1", suffix: "/v1", wantPath: "/v1/chat/completions"},
	}

	for _, op := range operations() {
		for _, bs := range baseSuffixes {
			t.Run(op.name+"/"+bs.name, func(t *testing.T) {
				t.Parallel()

				reqs := make(chan capturedRequest, 10)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					reqs <- capturedRequest{
						method:      r.Method,
						path:        r.URL.Path,
						contentType: r.Header.Get("Content-Type"),
						authValues:  r.Header.Values("Authorization"),
						body:        body,
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(completionJSON(op.reply))
				}))
				defer srv.Close()

				c := enrich.NewClient("sk-test", "test-model", 0, 0)
				c.SetBaseURL(srv.URL + bs.suffix)

				if _, err := op.call(context.Background(), c); err != nil {
					t.Fatalf("%s: unexpected error: %v", op.name, err)
				}
				close(reqs)

				var got []capturedRequest
				for r := range reqs {
					got = append(got, r)
				}
				if len(got) != 1 {
					t.Fatalf("expected exactly 1 request, got %d", len(got))
				}
				req := got[0]

				if req.method != http.MethodPost {
					t.Errorf("method = %q, want POST", req.method)
				}
				if req.path != bs.wantPath {
					t.Errorf("path = %q, want %q", req.path, bs.wantPath)
				}
				if !strings.HasPrefix(req.contentType, "application/json") {
					t.Errorf("Content-Type = %q, want application/json", req.contentType)
				}

				var raw map[string]json.RawMessage
				if err := json.Unmarshal(req.body, &raw); err != nil {
					t.Fatalf("request body is not a JSON object: %v\nbody: %s", err, req.body)
				}

				var model string
				if err := json.Unmarshal(raw["model"], &model); err != nil || model != "test-model" {
					t.Errorf("body.model = %s, want \"test-model\"", raw["model"])
				}

				streamRaw, ok := raw["stream"]
				if !ok {
					t.Errorf("body.stream missing, want false")
				} else {
					var stream bool
					if err := json.Unmarshal(streamRaw, &stream); err != nil || stream {
						t.Errorf("body.stream = %s, want false", streamRaw)
					}
				}

				var messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}
				if err := json.Unmarshal(raw["messages"], &messages); err != nil {
					t.Fatalf("body.messages is not an array of messages: %v (%s)", err, raw["messages"])
				}
				if len(messages) != 1 {
					t.Fatalf("expected 1 message, got %d: %+v", len(messages), messages)
				}
				if messages[0].Role != "user" {
					t.Errorf("messages[0].role = %q, want \"user\"", messages[0].Role)
				}
				if !strings.Contains(messages[0].Content, op.promptMarker) {
					t.Errorf("messages[0].content does not carry the prompt (missing %q)", op.promptMarker)
				}
			})
		}
	}
}

func TestClient_AuthorizationHeader(t *testing.T) {
	keys := []struct {
		name     string
		apiKey   string
		wantAuth []string
	}{
		{name: "key set sends bearer", apiKey: "sk-test-123", wantAuth: []string{"Bearer sk-test-123"}},
		{name: "empty key sends no header", apiKey: "", wantAuth: nil},
	}

	for _, op := range operations() {
		for _, k := range keys {
			t.Run(op.name+"/"+k.name, func(t *testing.T) {
				t.Parallel()

				authCh := make(chan []string, 1)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					authCh <- r.Header.Values("Authorization")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(completionJSON(op.reply))
				}))
				defer srv.Close()

				c := enrich.NewClient(k.apiKey, "test-model", 0, 0)
				c.SetBaseURL(srv.URL)

				if _, err := op.call(context.Background(), c); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				gotAuth := <-authCh

				if len(gotAuth) != len(k.wantAuth) {
					t.Fatalf("Authorization headers = %q, want %q", gotAuth, k.wantAuth)
				}
				for i := range k.wantAuth {
					if gotAuth[i] != k.wantAuth[i] {
						t.Errorf("Authorization = %q, want %q", gotAuth[i], k.wantAuth[i])
					}
				}
			})
		}
	}
}

// --- Response parsing ---

func TestEnrich_ParsesFirstChoiceContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mustJSON(map[string]any{
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": validWikiPage}},
				{"index": 1, "message": map[string]any{"role": "assistant", "content": "second choice must be ignored"}},
			},
		}))
	}))
	defer srv.Close()

	c := enrich.NewClient("", "test-model", 0, 0)
	c.SetBaseURL(srv.URL)

	page, err := c.Enrich(context.Background(), enrichInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(page, "This is a test summary.") {
		t.Errorf("expected page built from choices[0].message.content, got: %s", page)
	}
	if strings.Contains(page, minimalPageMarker) {
		t.Errorf("valid response must not produce a minimal page, got: %s", page)
	}
	if strings.Contains(page, "second choice") {
		t.Errorf("only choices[0] may be used, got: %s", page)
	}
}

func TestSuggestTags_ParsesFirstChoiceContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(completionJSON("golang, concurrency, channels"))
	}))
	defer srv.Close()

	c := enrich.NewClient("", "test-model", 0, 0)
	c.SetBaseURL(srv.URL)

	tags, err := c.SuggestTags(context.Background(), tagInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"golang", "concurrency", "channels"}
	if strings.Join(tags, ",") != strings.Join(want, ",") {
		t.Errorf("tags = %v, want %v", tags, want)
	}
}

// emptyOutputResponses are 200 responses that carry no usable model output.
// Each one is a failed call: retried, then minimal page / ["untagged"].
func emptyOutputResponses() []struct {
	name string
	body []byte
} {
	return []struct {
		name string
		body []byte
	}{
		{
			name: "empty choices array",
			body: mustJSON(map[string]any{"choices": []any{}}),
		},
		{
			name: "empty content",
			body: completionJSON(""),
		},
		{
			name: "empty content with reasoning text",
			body: mustJSON(map[string]any{
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":      "assistant",
							"content":   "",
							"reasoning": "Let me think about this article step by step...",
						},
						"finish_reason": "stop",
					},
				},
			}),
		},
		{
			// A 200 response whose body is not valid JSON at all (client.go
			// parse-error branch): still a failed call, not panic/crash.
			name: "malformed json body",
			body: []byte("not json"),
		},
	}
}

func TestEnrich_EmptyModelOutputRetriesThenMinimalPage(t *testing.T) {
	for _, tc := range emptyOutputResponses() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()

			c := enrich.NewClient("", "test-model", 1, 0)
			c.SetBaseURL(srv.URL)

			page, err := c.Enrich(context.Background(), enrichInput())
			if err != nil {
				t.Fatalf("empty output is not backend-unavailable, want nil error, got: %v", err)
			}
			if calls.Load() != 2 {
				t.Errorf("expected 2 calls (1 + 1 retry), got %d", calls.Load())
			}
			if !strings.Contains(page, minimalPageMarker) {
				t.Errorf("expected minimal page after retries, got: %s", page)
			}
		})
	}
}

func TestSuggestTags_EmptyModelOutputRetriesThenUntagged(t *testing.T) {
	for _, tc := range emptyOutputResponses() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()

			c := enrich.NewClient("", "test-model", 1, 0)
			c.SetBaseURL(srv.URL)

			tags, err := c.SuggestTags(context.Background(), tagInput())
			if err != nil {
				t.Fatalf("empty output is not backend-unavailable, want nil error, got: %v", err)
			}
			if calls.Load() != 2 {
				t.Errorf("expected 2 calls (1 + 1 retry), got %d", calls.Load())
			}
			if len(tags) != 1 || tags[0] != "untagged" {
				t.Errorf("tags = %v, want [untagged]", tags)
			}
		})
	}
}

// --- Backend unavailable ---

func TestClient_BackendUnavailableStatuses(t *testing.T) {
	statuses := []int{
		http.StatusUnauthorized,    // 401
		http.StatusPaymentRequired, // 402
		http.StatusForbidden,       // 403
		http.StatusNotFound,        // 404
		http.StatusGone,            // 410
	}

	for _, op := range operations() {
		for _, status := range statuses {
			t.Run(op.name+"/"+http.StatusText(status), func(t *testing.T) {
				t.Parallel()

				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":"backend refuses this request"}`))
				}))
				defer srv.Close()

				// maxRetries > 0 so any retry would be visible in the call count.
				c := enrich.NewClient(secretKey, "test-model", 2, 0)
				c.SetBaseURL(srv.URL)

				out, err := op.call(context.Background(), c)

				if err == nil {
					t.Fatalf("HTTP %d: expected an error, got nil (output: %q)", status, out)
				}
				if !errors.Is(err, enrich.ErrBackendUnavailable) {
					t.Errorf("HTTP %d: errors.Is(err, ErrBackendUnavailable) = false; err = %v", status, err)
				}
				if got := calls.Load(); got != 1 {
					t.Errorf("HTTP %d: expected exactly 1 request (no retries), got %d", status, got)
				}
				if out != "" {
					t.Errorf("HTTP %d: expected no output (no minimal page / fallback tags), got %q", status, out)
				}
				if strings.Contains(out, minimalPageMarker) {
					t.Errorf("HTTP %d: minimal page must not be produced", status)
				}
			})
		}
	}
}

// TestClient_OtherFourXXStatusesAreNotBackendUnavailable checks statuses
// that look similar to the backend-unavailable set (401/402/403/404/410) but
// are not in it: isBackendUnavailableStatus must return false for these, so
// the call still retries and ends in the normal fallback, never
// ErrBackendUnavailable.
func TestClient_OtherFourXXStatusesAreNotBackendUnavailable(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest,            // 400
		http.StatusRequestTimeout,        // 408
		http.StatusRequestEntityTooLarge, // 413
		http.StatusUnprocessableEntity,   // 422
	}

	for _, op := range operations() {
		for _, status := range statuses {
			t.Run(op.name+"/"+http.StatusText(status), func(t *testing.T) {
				t.Parallel()

				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":"bad request"}`))
				}))
				defer srv.Close()

				c := enrich.NewClient("", "test-model", 1, 0)
				c.SetBaseURL(srv.URL)

				// A nil error here already rules out ErrBackendUnavailable (or
				// any other error), so there is nothing left for a separate
				// errors.Is check to catch.
				out, err := op.call(context.Background(), c)
				if err != nil {
					t.Fatalf("HTTP %d: not backend-unavailable, want nil error, got: %v", status, err)
				}
				if calls.Load() != 2 {
					t.Errorf("HTTP %d: expected 2 calls (1 + 1 retry), got %d", status, calls.Load())
				}
				if op.name == "Enrich" {
					if !strings.Contains(out, minimalPageMarker) {
						t.Errorf("HTTP %d: expected minimal page after retries, got: %q", status, out)
					}
				} else if out != "untagged" {
					t.Errorf("HTTP %d: expected fallback tag \"untagged\", got: %q", status, out)
				}
			})
		}
	}
}

func TestClient_BackendUnavailableConnectionRefused(t *testing.T) {
	for _, op := range operations() {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()

			// Start and immediately close a server to get an address nothing
			// listens on.
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			deadURL := srv.URL
			srv.Close()

			c := enrich.NewClient(secretKey, "test-model", 2, 0)
			c.SetBaseURL(deadURL)

			// The existing retry backoff waits at least one second between
			// attempts, so finishing well under that proves no retry happened.
			start := time.Now()
			out, err := op.call(context.Background(), c)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("expected an error, got nil (output: %q)", out)
			}
			if !errors.Is(err, enrich.ErrBackendUnavailable) {
				t.Errorf("errors.Is(err, ErrBackendUnavailable) = false; err = %v", err)
			}
			if out != "" {
				t.Errorf("expected no output (no minimal page / fallback tags), got %q", out)
			}
			if elapsed >= time.Second {
				t.Errorf("took %v: a connection error must not be retried", elapsed)
			}
			if strings.Contains(err.Error(), secretKey) {
				t.Errorf("error leaks the API key: %v", err)
			}
		})
	}
}

func TestClient_BackendUnavailableTLSError(t *testing.T) {
	for _, op := range operations() {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()

			var conns, handled atomic.Int32
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handled.Add(1)
			}))
			// Count connection attempts; the handshake fails before any request
			// reaches the handler because the client does not trust the test CA.
			srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
				if s == http.StateNew {
					conns.Add(1)
				}
			}
			srv.Config.ErrorLog = log.New(io.Discard, "", 0)
			srv.StartTLS()
			defer srv.Close()

			c := enrich.NewClient(secretKey, "test-model", 2, 0)
			c.SetBaseURL(srv.URL)

			out, err := op.call(context.Background(), c)

			if err == nil {
				t.Fatalf("expected an error, got nil (output: %q)", out)
			}
			if !errors.Is(err, enrich.ErrBackendUnavailable) {
				t.Errorf("errors.Is(err, ErrBackendUnavailable) = false; err = %v", err)
			}
			if got := conns.Load(); got != 1 {
				t.Errorf("expected exactly 1 connection attempt (no retries), got %d", got)
			}
			if handled.Load() != 0 {
				t.Errorf("handler must not be reached on a TLS failure")
			}
			if out != "" {
				t.Errorf("expected no output (no minimal page / fallback tags), got %q", out)
			}
		})
	}
}

// TestClient_BackendUnavailableConnectionReset covers a connection that
// resets mid-request (the backend process crashing after accepting the
// connection but before writing a response), rather than refusing the
// connection outright. Per ADR-031's connection-level-failure consequence,
// this is treated the same as connection-refused: ErrBackendUnavailable,
// stop-cycle, no retry — there is no reliable way at the transport level to
// tell "backend crashed mid-response" apart from "backend truly down".
func TestClient_BackendUnavailableConnectionReset(t *testing.T) {
	for _, op := range operations() {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()

			var conns atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conns.Add(1)
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Fatal("ResponseWriter does not support hijacking")
				}
				conn, _, err := hj.Hijack()
				if err != nil {
					t.Fatalf("Hijack: %v", err)
				}
				// Close the raw connection without writing any response,
				// simulating the backend process dying mid-request.
				_ = conn.Close()
			}))
			defer srv.Close()

			c := enrich.NewClient(secretKey, "test-model", 2, 0)
			c.SetBaseURL(srv.URL)

			out, err := op.call(context.Background(), c)

			if err == nil {
				t.Fatalf("expected an error, got nil (output: %q)", out)
			}
			if !errors.Is(err, enrich.ErrBackendUnavailable) {
				t.Errorf("errors.Is(err, ErrBackendUnavailable) = false; err = %v", err)
			}
			if got := conns.Load(); got != 1 {
				t.Errorf("expected exactly 1 connection attempt (no retries), got %d", got)
			}
			if out != "" {
				t.Errorf("expected no output (no minimal page / fallback tags), got %q", out)
			}
			if strings.Contains(err.Error(), secretKey) {
				t.Errorf("error leaks the API key: %v", err)
			}
		})
	}
}

func TestClient_BackendUnavailableErrorMessage(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantContains []string
		maxLen       int
	}{
		{
			name:         "includes status code and body",
			status:       http.StatusPaymentRequired,
			body:         `{"error":"insufficient credits for this account"}`,
			wantContains: []string{"402", "insufficient credits for this account"},
		},
		{
			name:         "long body is truncated",
			status:       http.StatusNotFound,
			body:         "model not found " + strings.Repeat("x", 100_000),
			wantContains: []string{"404", "model not found"},
			maxLen:       10_000,
		},
	}

	for _, op := range operations() {
		for _, tt := range tests {
			t.Run(op.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer srv.Close()

				c := enrich.NewClient(secretKey, "test-model", 0, 0)
				c.SetBaseURL(srv.URL)

				_, err := op.call(context.Background(), c)
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !errors.Is(err, enrich.ErrBackendUnavailable) {
					t.Errorf("errors.Is(err, ErrBackendUnavailable) = false; err = %v", err)
				}

				msg := err.Error()
				for _, want := range tt.wantContains {
					if !strings.Contains(msg, want) {
						t.Errorf("error message missing %q: %.300s", want, msg)
					}
				}
				if strings.Contains(msg, secretKey) {
					t.Errorf("error message leaks the API key: %.300s", msg)
				}
				if tt.maxLen > 0 && len(msg) > tt.maxLen {
					t.Errorf("error message is %d bytes, want the body truncated to at most %d", len(msg), tt.maxLen)
				}
			})
		}
	}
}

// --- Existing behaviour kept ---

func TestClient_RateLimitThenSuccess(t *testing.T) {
	for _, op := range operations() {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte("rate limited"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(completionJSON(op.reply))
			}))
			defer srv.Close()

			c := enrich.NewClient("", "test-model", 1, 0)
			c.SetBaseURL(srv.URL)

			out, err := op.call(context.Background(), c)
			if err != nil {
				t.Fatalf("429 is not backend-unavailable, want nil error, got: %v", err)
			}
			if calls.Load() != 2 {
				t.Errorf("expected 2 calls (429 then 200), got %d", calls.Load())
			}
			if strings.Contains(out, minimalPageMarker) || out == "untagged" {
				t.Errorf("expected real output after backoff, got fallback: %q", out)
			}
		})
	}
}

func TestEnrich_ServerErrorEveryAttemptEndsInMinimalPage(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := enrich.NewClient("", "test-model", 1, 0)
	c.SetBaseURL(srv.URL)

	page, err := c.Enrich(context.Background(), enrichInput())
	if err != nil {
		t.Fatalf("5xx is not backend-unavailable, want nil error, got: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (1 + 1 retry), got %d", calls.Load())
	}
	if !strings.Contains(page, minimalPageMarker) {
		t.Errorf("expected minimal page, got: %s", page)
	}
}

func TestSuggestTags_ServerErrorEveryAttemptEndsInUntagged(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := enrich.NewClient("", "test-model", 1, 0)
	c.SetBaseURL(srv.URL)

	tags, err := c.SuggestTags(context.Background(), tagInput())
	if err != nil {
		t.Fatalf("5xx is not backend-unavailable, want nil error, got: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (1 + 1 retry), got %d", calls.Load())
	}
	if len(tags) != 1 || tags[0] != "untagged" {
		t.Errorf("tags = %v, want [untagged]", tags)
	}
}
