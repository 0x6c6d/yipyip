#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "resume picker + restore tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestResumePickerShowsRowsNewestFirst TestResumePickerEnterRestores TestResumePickerEscExitsWithoutStarting TestResumePickerEmptyDirFreshStart TestResumePickerScrollIndicator TestRestoreDropsLiveMarkers
CMD
expect_ok "whole tui package (older Resume/ExitSave tests stay green)" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "picker title text present" <<'CMD'
grep -rnq 'resume session' internal/tui/resume.go
CMD
expect_ok "restore routes through the agent restore setters" <<'CMD'
grep -rn 'RestoreHistory' internal/tui/resume.go | grep -q -e 'RestoreHistory'
CMD
expect_fail "negative: tui reads the sessions dir only via internal/session (no direct walking)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'os.ReadDir' -e 'ioutil.ReadDir' -e 'filepath.Glob' internal/tui
CMD
expect_fail "negative: internal/agent does not import internal/session" <<'CMD'
no_match -rn --include='*.go' -e '"yipyip/internal/session"' internal/agent
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
