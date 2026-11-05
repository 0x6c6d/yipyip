#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "FromAgent/ApplyTo tests" <<'CMD'
go_tests ./internal/session/ TestFromAgent TestFromAgentRoundTrip TestApplyToKeepsModelAndMode
CMD
expect_ok "exported helpers" <<'CMD'
has_symbols ./internal/session FromAgent ApplyTo
CMD
expect_ok "tui and session packages unchanged in behaviour" <<'CMD'
go test -count=1 ./internal/session/ ./internal/tui/ ./cmd/yip/
CMD
expect_ok "e2e session, autosave and retention" <<'CMD'
go test -count=1 -run 'Session|Autosave|Retention' ./e2e/
CMD
expect_ok "tui uses the shared helpers" <<'CMD'
grep -q -e 'session.FromAgent(' internal/tui/save.go && grep -q -e 'session.ApplyTo(' internal/tui/resume.go
CMD
expect_fail "negative: internal/agent does not import internal/session" <<'CMD'
no_match -rn -e '"yipyip/internal/session"' internal/agent/
CMD
finish
