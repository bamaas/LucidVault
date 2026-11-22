package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// chatJSON builds an OpenAI-compatible chat completions response whose
// choices[0].message.content is content. It uses plain maps so the tests do
// not depend on the client's internal response types.
func chatJSON(content string) []byte {
	resp := map[string]any{
		"id":     "chatcmpl-test",
		"object": "chat.completion",
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

const validContent = `---
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

func TestEnrich_Success(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatJSON(validContent))
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "This is a test summary.") {
		t.Errorf("expected result to contain Summary section, got: %s", result)
	}
}

func TestEnrich_RetryOnValidationFailure(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First call: return invalid content (no frontmatter)
			_, _ = w.Write(chatJSON("no frontmatter here"))
		} else {
			// Second call: return valid content
			_, _ = w.Write(chatJSON(validContent))
		}
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "This is a test summary.") {
		t.Errorf("expected valid result after retry, got: %s", result)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 API calls, got %d", calls.Load())
	}
}

func TestEnrich_FallbackOnPersistentValidationFailure(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatJSON("garbage output"))
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Fallback Test",
		URL:       "http://example.com/fallback",
		DateSaved: "2024-01-01",
		UserTags:  []string{"test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Enrichment failed") {
		t.Errorf("expected minimal fallback page, got: %s", result)
	}
}

func TestEnrich_RetryOnHTTPError(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(chatJSON(validContent))
		}
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "This is a test summary.") {
		t.Errorf("expected valid result after retry, got: %s", result)
	}
}

func TestEnrich_RateLimitBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(chatJSON(validContent))
		}
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "This is a test summary.") {
		t.Errorf("expected valid result after rate limit retry")
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 API calls, got %d", calls.Load())
	}
}

func TestEnrich_ContextCancellation(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		// Slow server — never responds
		<-r.Context().Done()
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := c.Enrich(ctx, &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestEnrich_TimeoutRetriesThenMinimalPage covers the timeout branch
// (client.go callAPI: a net.Error with Timeout() == true is treated like a
// 5xx, not ErrBackendUnavailable).
func TestEnrich_TimeoutRetriesThenMinimalPage(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Sleep well past the client's timeout so httpClient.Do fails with a
		// timeout error instead of succeeding.
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)
	c.httpClient.Timeout = 50 * time.Millisecond

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("timeout is not backend-unavailable, want nil error, got: %v", err)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Error("timeout must not be classified as ErrBackendUnavailable")
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (1 + 1 retry), got %d", calls.Load())
	}
	if !strings.Contains(result, "Enrichment failed") {
		t.Errorf("expected minimal page after retries, got: %s", result)
	}
}

// TestSuggestTags_TimeoutRetriesThenUntagged is the SuggestTags counterpart
// of TestEnrich_TimeoutRetriesThenMinimalPage.
func TestSuggestTags_TimeoutRetriesThenUntagged(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)
	c.httpClient.Timeout = 50 * time.Millisecond

	tags, err := c.SuggestTags(context.Background(), &TagInput{
		Content: "# Test",
		Title:   "Test",
	})
	if err != nil {
		t.Fatalf("timeout is not backend-unavailable, want nil error, got: %v", err)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Error("timeout must not be classified as ErrBackendUnavailable")
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (1 + 1 retry), got %d", calls.Load())
	}
	if len(tags) != 1 || tags[0] != "untagged" {
		t.Errorf("tags = %v, want [untagged]", tags)
	}
}

// TestEnrich_CancellationDuringRequest covers a context deadline that
// expires while httpClient.Do is in flight (callAPI's ctx.Err() guard right
// after httpClient.Do fails, and Enrich's own ctx.Err() recheck after
// callAPI returns) — distinct from cancelling before the call is ever made.
//
// Honesty note: this test cannot isolate callAPI's ctx.Err() guard from the
// netErr.Timeout() branch right after it. A context.WithTimeout deadline
// produces context.DeadlineExceeded, which itself satisfies net.Error's
// Timeout() == true — so even if the ctx.Err() check were deleted, the
// Timeout() branch would classify the same underlying error identically
// (wrapped, not ErrBackendUnavailable) and this test would still pass. It
// documents the deadline-based behaviour, not a regression guard for that
// specific check. See TestEnrich_CancellationMidFlight below for a
// context.WithCancel variant that does isolate it.
func TestEnrich_CancellationDuringRequest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewClient("key", "model", 2, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Enrich(ctx, &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false; err = %v", err)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Error("a cancellation mid-request must not be classified as ErrBackendUnavailable")
	}
}

// TestSuggestTags_CancellationDuringRequest is the SuggestTags counterpart
// of TestEnrich_CancellationDuringRequest (SuggestTags' own ctx.Err()
// recheck after callAPI returns). Same honesty note applies: a deadline, not
// a plain cancel, can't isolate callAPI's ctx.Err() guard from its
// neighboring netErr.Timeout() branch.
func TestSuggestTags_CancellationDuringRequest(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewClient("key", "model", 2, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.SuggestTags(ctx, &TagInput{
		Content: "# Test",
		Title:   "Test",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false; err = %v", err)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Error("a cancellation mid-request must not be classified as ErrBackendUnavailable")
	}
}

// TestEnrich_CancellationMidFlight covers a genuine mid-flight cancellation
// (context.WithCancel, triggered by the test only once the handler has
// actually received the request) rather than a deadline — distinct from
// TestEnrich_CancellationDuringRequest above.
//
// Honesty note: this still cannot isolate the ctx.Err() guard inside
// callAPI (right after httpClient.Do fails) from Enrich's own redundant
// ctx.Err() recheck a few lines after callAPI returns. Verified empirically
// (by temporarily deleting callAPI's guard):
// this test still passes even with that line removed, because Enrich's
// outer recheck sees the same still-canceled ctx and returns ctx.Err()
// regardless of what callAPI itself returned. Isolating callAPI's guard
// specifically requires calling it directly, which
// TestCallAPI_CancellationMidFlight below does.
//
// The handler blocks on a test-controlled channel, not on r.Context().Done():
// verified empirically that Go's net/http server does not reliably cancel
// the server-side request context when a client-initiated context
// cancellation aborts a POST with a body still in flight — httpClient.Do()
// on the client side still returns immediately with context.Canceled either
// way, which is the only thing this test needs. Waiting on r.Context().Done()
// instead left the handler blocked forever and hung httptest.Server.Close().
func TestEnrich_CancellationMidFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})
	defer srv.Close()

	c := NewClient("key", "model", 2, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	_, err := c.Enrich(ctx, &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	close(release) // let the handler return so srv.Close() doesn't block

	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Error("a mid-flight cancellation must not be classified as ErrBackendUnavailable")
	}
}

// TestSuggestTags_CancellationMidFlight is the SuggestTags counterpart of
// TestEnrich_CancellationMidFlight (same honesty note: it cannot isolate
// callAPI's ctx.Err() guard from SuggestTags' own redundant recheck either;
// same note on why the handler blocks on a test-controlled channel rather
// than r.Context().Done()).
func TestSuggestTags_CancellationMidFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})
	defer srv.Close()

	c := NewClient("key", "model", 2, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	_, err := c.SuggestTags(ctx, &TagInput{
		Content: "# Test",
		Title:   "Test",
	})
	close(release) // let the handler return so srv.Close() doesn't block

	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Error("a mid-flight cancellation must not be classified as ErrBackendUnavailable")
	}
}

// TestCallAPI_CancellationMidFlight genuinely isolates the ctx.Err() guard
// inside callAPI (immediately after httpClient.Do fails, immediately before
// the connected/netErr.Timeout() checks) from Enrich/SuggestTags' own
// redundant outer recheck, by calling callAPI directly (this file is package enrich,
// not enrich_test, so the unexported method is reachable). Confirmed by
// temporarily deleting that guard: this test fails (the error becomes
// ErrBackendUnavailable) while TestEnrich_CancellationMidFlight and
// TestSuggestTags_CancellationMidFlight above still pass, which is exactly
// why those two cannot be relied on to catch a regression in this
// particular line.
func TestCallAPI_CancellationMidFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	_, _, err := c.callAPI(ctx, "prompt")
	close(release) // let the handler return so srv.Close() doesn't block

	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Error("a mid-flight cancellation inside callAPI must not be classified as ErrBackendUnavailable")
	}
}

// TestCallAPI_RedactsAPIKeyFromBody covers callAPI's plain non-200 branch
// (status codes outside the backend-unavailable set, e.g. 500), which is
// retried and logged on every attempt. A backend or intermediary that
// echoes the presented key in its error body must never leak it into that
// logged error.
func TestCallAPI_RedactsAPIKeyFromBody(t *testing.T) {
	const key = "sk-secret-leak-test"
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream rejected key " + key))
	})
	defer srv.Close()

	c := NewClient(key, "model", 0, 0)
	c.SetBaseURL(srv.URL)

	_, _, err := c.callAPI(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("error leaks the API key: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("expected the redaction marker in the error, got: %v", err)
	}
}

// fakeTimeoutError simulates a timed-out net.Error without any real network
// wait, so dial-phase-timeout classification can be tested deterministically
// and fast.
type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "i/o timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return false }

// fakeDialTimeoutTransport always fails as if the dial phase itself timed
// out (e.g. a firewall silently dropping SYN packets) — a real connect
// timeout against an unroutable address would also work, but takes minutes
// on a default Linux TCP stack, which is too slow for a unit test.
type fakeDialTimeoutTransport struct{ calls *atomic.Int32 }

func (tr *fakeDialTimeoutTransport) RoundTrip(*http.Request) (*http.Response, error) {
	tr.calls.Add(1)
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: fakeTimeoutError{}}
}

// TestEnrich_DialTimeoutIsBackendUnavailable covers the case the plain
// "any Timeout() == true is retried like a 5xx" rule used to get wrong: a
// dial-phase timeout never produces a usable connection (no httptrace
// GotConn), so it belongs with connection-refused/DNS/TLS as
// ErrBackendUnavailable, not as a retryable slow response. This fake
// RoundTripper never calls the httptrace hooks at all (it bypasses the real
// dialer entirely), which has the same "never connected" effect as a real
// one that times out before GotConn fires.
func TestEnrich_DialTimeoutIsBackendUnavailable(t *testing.T) {
	var calls atomic.Int32
	c := NewClient("key", "model", 2, 0)
	c.SetBaseURL("http://enrich.invalid")
	c.httpClient.Transport = &fakeDialTimeoutTransport{calls: &calls}

	_, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("errors.Is(err, ErrBackendUnavailable) = false; err = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 dial attempt (no retries), got %d", got)
	}
}

// TestClient_BackendUnavailableTLSHandshakeTimeout covers a TLS handshake
// that never completes (e.g. a backend that accepts the TCP connection but
// hangs before sending a ServerHello). net/http reports this with
// Timeout() == true, same as a slow-to-answer backend, but no usable
// connection was ever established (httptrace's GotConn never fires), so it
// must be classified as ErrBackendUnavailable, not retried like a slow
// response.
func TestClient_BackendUnavailableTLSHandshakeTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Accept in a loop (not just once): a retried dial would otherwise
	// complete silently in the kernel's accept backlog without this test
	// ever noticing, making the "exactly 1 attempt" assertion below
	// vacuous. Accepting in a loop means a real retry shows up as a second
	// accepted connection.
	var accepted atomic.Int32
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			// Never writes a TLS ServerHello, so the client's handshake
			// times out instead of completing or failing outright.
		}
	}()
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()

	c := NewClient("key", "model", 2, 0)
	c.SetBaseURL("https://" + ln.Addr().String())
	c.httpClient.Transport.(*http.Transport).TLSHandshakeTimeout = 50 * time.Millisecond

	_, err = c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})

	if !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("errors.Is(err, ErrBackendUnavailable) = false; err = %v", err)
	}
	// Give a would-be retry time to reach the listener before checking the
	// count: ErrBackendUnavailable means no retry, so nothing more should
	// arrive after Enrich has already returned.
	time.Sleep(50 * time.Millisecond)
	if got := accepted.Load(); got != 1 {
		t.Errorf("expected exactly 1 connection attempt (no retries), got %d", got)
	}
}

// TestCallAPI_RedactsAPIKeyEvenWhenKeySpansTruncationBoundary guards
// against redacting after truncating: if the body were truncated to
// maxErrorBodyBytes before the key was replaced, a key straddling that cut
// would have its surviving prefix leak into the error unredacted.
func TestCallAPI_RedactsAPIKeyEvenWhenKeySpansTruncationBoundary(t *testing.T) {
	const key = "sk-boundary-leak-0123456789"
	// The key starts 10 bytes before the truncation cutoff, so a
	// truncate-then-redact implementation would cut it mid-string.
	body := strings.Repeat("x", maxErrorBodyBytes-10) + key

	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	defer srv.Close()

	c := NewClient(key, "model", 0, 0)
	c.SetBaseURL(srv.URL)

	_, _, err := c.callAPI(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if strings.Contains(err.Error(), key[:10]) {
		t.Errorf("error leaks a prefix of the API key across the truncation boundary: %v", err)
	}
}

func TestCleanResponse_StripMarkdownFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fences",
			input: "---\ntitle: test\n---",
			want:  "---\ntitle: test\n---",
		},
		{
			name:  "markdown fence",
			input: "```markdown\n---\ntitle: test\n---\n```",
			want:  "---\ntitle: test\n---",
		},
		{
			name:  "generic fence",
			input: "```\n---\ntitle: test\n---\n```",
			want:  "---\ntitle: test\n---",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanResponse(tt.input)
			if got != tt.want {
				t.Errorf("cleanResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateResponse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "valid",
			content: validContent,
			wantErr: false,
		},
		{
			name:    "no frontmatter",
			content: "# Just a heading",
			wantErr: true,
		},
		{
			name:    "missing title",
			content: "---\ntags:\n  - test\n---\n\n## Summary\nfoo\n\n## Key Takeaways\nbar",
			wantErr: true,
		},
		{
			name:    "missing tags",
			content: "---\ntitle: test\n---\n\n## Summary\nfoo\n\n## Key Takeaways\nbar",
			wantErr: true,
		},
		{
			name:    "missing summary",
			content: "---\ntitle: test\ntags:\n  - x\n---\n\n## Key Takeaways\nbar",
			wantErr: true,
		},
		{
			name:    "missing key takeaways",
			content: "---\ntitle: test\ntags:\n  - x\n---\n\n## Summary\nfoo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResponse(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSuggestTags_Success(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatJSON("golang, concurrency, channels"))
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	tags, err := c.SuggestTags(context.Background(), &TagInput{
		Content: "# Go Concurrency\n\nGoroutines and channels are great.",
		Title:   "Go Concurrency",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags[0] != "golang" || tags[1] != "concurrency" || tags[2] != "channels" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

func TestSuggestTags_StripsWhitespaceAndBullets(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatJSON("- golang\n- testing\n- tdd"))
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	tags, err := c.SuggestTags(context.Background(), &TagInput{
		Content: "# Testing\n\nTDD is great.",
		Title:   "Testing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags[0] != "golang" || tags[1] != "testing" || tags[2] != "tdd" {
		t.Errorf("unexpected tags: %v", tags)
	}
}

func TestSuggestTags_FallbackOnError(t *testing.T) {
	// 5xx is not "backend unavailable" (ADR-031): it still ends in the
	// ["untagged"] fallback without an error.
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	tags, err := c.SuggestTags(context.Background(), &TagInput{
		Content: "# Test",
		Title:   "Test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 || tags[0] != "untagged" {
		t.Errorf("expected fallback [untagged], got %v", tags)
	}
}

func TestSuggestTags_ContextCancellation(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.SuggestTags(ctx, &TagInput{
		Content: "# Test",
		Title:   "Test",
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestBuildMinimalPage(t *testing.T) {
	input := &EnrichInput{
		Title:       "Test Title",
		URL:         "http://example.com",
		DateSaved:   "2024-01-01",
		UserTags:    []string{"go", "testing"},
		RawFilename: "2024-01-01-test-title.md",
	}

	result := buildMinimalPage(input)

	if !strings.Contains(result, `title: Test Title`) {
		t.Error("expected title in frontmatter")
	}
	if !strings.Contains(result, "## Summary") {
		t.Error("expected Summary section")
	}
	if !strings.Contains(result, "## Key Takeaways") {
		t.Error("expected Key Takeaways section")
	}
	if !strings.Contains(result, "Enrichment failed") {
		t.Error("expected fallback message")
	}
	if !strings.Contains(result, "- go") {
		t.Error("expected user tags")
	}
}
