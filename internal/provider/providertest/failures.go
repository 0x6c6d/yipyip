package providertest

import (
	"net/http"
)

// FailMode selects a failure that happens before the first SSE byte.
type FailMode int

const (
	FailReset FailMode = iota + 1 // the server closes the TCP connection before writing anything
	Fail429                       // 429 Too Many Requests
	Fail503                       // 503 Service Unavailable
)

// Failures scripts a run of pre-first-byte failures: the first Count requests
// fail in Mode, later requests get the scripted responses as in New.
type Failures struct {
	Mode       FailMode
	Count      int
	RetryAfter string // optional Retry-After header value for Fail429/Fail503
}

// response is the scripted reply for one failing attempt.
func (f Failures) response() Response {
	switch f.Mode {
	case FailReset:
		return Response{Reset: true}
	case Fail429:
		return Response{Status: http.StatusTooManyRequests, Body: "rate limited", RetryAfter: f.RetryAfter}
	default:
		return Response{Status: http.StatusServiceUnavailable, Body: "unavailable", RetryAfter: f.RetryAfter}
	}
}

// NewWithFailures starts a fake provider whose first f.Count requests fail as
// scripted; request f.Count+i then gets responses[i] (afterwards Response{Text: "ok"}).
// Every attempt, failed or not, is recorded and returned by Requests.
func NewWithFailures(f Failures, responses ...Response) *Server {
	rs := append([]Response(nil), responses...)
	return NewFunc(func(n int, _ Request) Response {
		if n < f.Count {
			return f.response()
		}
		if i := n - f.Count; i < len(rs) {
			return rs[i]
		}
		return Response{Text: "ok"}
	})
}

// resetConn closes the underlying TCP connection without writing a byte.
func resetConn(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			_ = conn.Close()
			return
		}
	}
	panic(http.ErrAbortHandler)
}
