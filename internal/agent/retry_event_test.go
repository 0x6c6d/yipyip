package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// retryTurn runs one turn against a provider that fails once with a 503
// (the body "unavailable" is the provider's reason text) and then answers.
func retryTurn(t *testing.T) (*Session, []Event) {
	t.Helper()
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 1},
		fakeResp{Text: "final answer"})
	t.Cleanup(srv.Close)
	s, _ := newLoopSession(t, srv)
	s.SetRetryWait(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	return s, submitAndWait(t, s, "hi")
}

func TestRetryEventForwarded(t *testing.T) {
	_, evs := retryTurn(t)
	retryAt, textAt, n := -1, -1, 0
	for i, ev := range evs {
		switch ev.Kind {
		case EvRetry:
			n++
			retryAt = i
			if ev.Retry == nil || ev.Retry.Attempt != 2 || ev.Retry.Max != 10 || ev.Retry.Wait != time.Second {
				t.Errorf("retry info = %+v, want attempt 2, max 10, wait 1s", ev.Retry)
			}
			if ev.Text != "retrying (2/10) in 1s" {
				t.Errorf("retry text = %q", ev.Text)
			}
		case EvText:
			if textAt < 0 {
				textAt = i // first streamed delta of the answer
			}
		case EvError:
			t.Errorf("unexpected error event %q", ev.Text)
		}
	}
	if n != 1 {
		t.Fatalf("%d EvRetry events, want 1: %+v", n, evs)
	}
	if textAt < 0 || retryAt > textAt {
		t.Errorf("retry at %d, final text at %d: want the retry first", retryAt, textAt)
	}
}

func TestRetryEventNotInHistory(t *testing.T) {
	s, _ := retryTurn(t)
	for _, h := range s.History() {
		if strings.Contains(h.Content, "retrying") || strings.Contains(h.Content, "unavailable") {
			t.Errorf("history entry %q mentions the retry", h.Content)
		}
	}
}
