#!/usr/bin/env bash
# Verification for T602 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tui settings trigger tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestModelChangeWritesSettings TestEffortAndModeChangesWriteSettings TestResumeValuesWriteSettings TestSettingsWriteFailureShowsOneLine TestSettingsWriteFailureRedactsSecretInError TestNoSettingsWriteOnNoChange TestNoSettingsWriteWhenPathEmpty
CMD
expect_ok "whole tui package" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "settings.go exists and routes through config.SaveSettings" <<'CMD'
grep -rnq 'config.SaveSettings' internal/tui/settings.go
CMD
expect_ok "the failure line text matches exactly" <<'CMD'
grep -rnq 'could not save settings' internal/tui/settings.go
CMD
expect_fail "negative: tui never writes the config file directly (os.WriteFile/json.Marshal absent from settings.go)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'os.WriteFile' -e 'json.Marshal' internal/tui/settings.go
CMD
expect_ok "SaveSettings takes four plain string args (no *Config, so no resolved secret can reach it)" <<'CMD'
grep -rn 'func SaveSettings(path string, model, effort, mode string) error' internal/config/settings.go
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
