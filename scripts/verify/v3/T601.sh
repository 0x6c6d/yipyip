#!/usr/bin/env bash
# Verification for T601 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "settings write tests incl. negatives" <<'CMD'
go_tests ./internal/config TestSaveSettingsThreeKeysAtomic TestSaveSettingsMissingFileCreates TestSaveSettingsInvalidJSONFileNotOverwritten TestSaveSettingsPreservesEnvTokenLiteral TestSaveSettingsNoResolvedSecretWritten TestSaveSettingsIdempotent
CMD
expect_ok "whole internal/config package (load/merge unchanged)" <<'CMD'
go test -count=1 ./internal/config
CMD
expect_ok "SaveSettings exported with the brief's signature shape" <<'CMD'
has_symbols ./internal/config SaveSettings
CMD
expect_ok "atomic rename present" <<'CMD'
grep -rnq -- 'os.Rename' internal/config/settings.go
CMD
expect_fail "negative: settings.go must not resolve env tokens (no resolver call; {env:} tokens must stay literal by construction)" <<'CMD'
no_match -rn --include='*.go' -e 'envResolver' -e 'substitute' -e 'Redactor' internal/config/settings.go
CMD
expect_fail "negative: settings.go never touches the startdir project config or tool paths" <<'CMD'
no_match -rni --include='*.go' -e 'yipyip.json' -e 'pathguard' internal/config/settings.go
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
