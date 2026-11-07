#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "Config.GlobalDir exists" <<'CMD'
has_symbols ./internal/config Config.GlobalDir
CMD
expect_ok "named config tests pass" <<'CMD'
go_tests ./internal/config TestLoadGlobalDir TestLoadGlobalDirMissingConfig TestLoadGlobalDirIgnoresXDG
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe .harness/verify/v13/probes/T101_probe.go
CMD
expect_ok "probe: XDG_CONFIG_HOME ignored, missing config, dir not created" <<'CMD'
build_probe .harness/verify/v13/probes/T101_probe.go && probe_ok xdg
CMD
expect_ok "probe: empty home gives empty GlobalDir" <<'CMD'
build_probe .harness/verify/v13/probes/T101_probe.go && probe_ok nohome
CMD
expect_fail "negative: config.go must not consult XDG_CONFIG_HOME" <<'CMD'
no_match -n -e 'XDG_CONFIG_HOME' internal/config/config.go
CMD
expect_fail "negative: no changes in provider, tools, cmd/yip" <<'CMD'
out="$(git diff --name-only 81ebe7e -- internal/provider internal/tools cmd/yip; git ls-files --others --exclude-standard -- internal/provider internal/tools cmd/yip)"
[ -n "$out" ] && { echo "$out"; exit 0; }
exit 1
CMD
expect_ok "config package tests and vet" <<'CMD'
go test -count=1 ./internal/config && go vet ./internal/config && test -z "$(gofmt -l internal/config)"
CMD
finish
