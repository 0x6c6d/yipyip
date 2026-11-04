#!/usr/bin/env bash
# Verification for T606 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "startup settings tests incl. negatives" <<'CMD'
go_tests ./cmd/yip TestStartupAppliesStoredSettings TestStartupProjectConfigOverridesStoredSettings TestStartupIgnoresInvalidStoredSettings TestStartupStoredSettingsDoNotWriteConfig
CMD
expect_ok "cmd/yip, config and tui packages green" <<'CMD'
go test -count=1 ./cmd/yip ./internal/config ./internal/tui
CMD
expect_ok "effort and mode are read in cmd/yip or config" <<'CMD'
grep -rn --include='*.go' --exclude='*_test.go' -e '"effort"' -e '"mode"' cmd/yip internal/config | grep -v 'settings.go' | grep -q .
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
