package tui

import (
	"testing"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

// fakeClock is an injectable clock for Options.Now.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// newClockModel is newSessionModel with Options.Now set to a fake clock.
func newClockModel(t *testing.T, srv *providertest.Server) (Model, *agent.Session, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	m, sess := newSessionModelOpts(t, srv, "start", func(o *Options) { o.Now = clock.Now })
	return m, sess, clock
}
