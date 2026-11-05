package main

import "testing"

func TestExitFor(t *testing.T) {
	cases := []struct {
		s      exitState
		code   int
		reason string
	}{
		{exitState{}, 0, "done"},
		{exitState{err: true}, 1, "error"},
		{exitState{maxCalls: true}, 5, "max_calls"},
		{exitState{maxCalls: true, err: true}, 5, "max_calls"},
		{exitState{question: true}, 3, "question"},
		{exitState{question: true, maxCalls: true, err: true}, 3, "question"},
		{exitState{timeout: true}, 4, "timeout"},
		{exitState{timeout: true, err: true}, 4, "timeout"},
		{exitState{timeout: true, maxCalls: true}, 4, "timeout"},
		{exitState{timeout: true, question: true, maxCalls: true, err: true}, 4, "timeout"},
	}
	for _, c := range cases {
		code, reason := exitFor(c.s)
		if code != c.code || reason != c.reason {
			t.Errorf("exitFor(%+v) = %d %q, want %d %q", c.s, code, reason, c.code, c.reason)
		}
	}
}
