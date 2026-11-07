#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "help test passes" <<'CMD'
go_tests ./internal/tui TestHelpGlobalAgents
CMD
expect_ok "help.go names the global path and says global" <<'CMD'
grep -q -F '~/.config/yipyip/AGENTS.md' internal/tui/help.go && grep -q -i -e 'global' internal/tui/help.go
CMD
expect_ok "yip --help shows the global file" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip && HOME="$VTMP" "$VTMP/yip" --help > "$VTMP/help.txt" 2>&1; grep -q -F '~/.config/yipyip/AGENTS.md' "$VTMP/help.txt"
CMD
expect_fail "negative: /init entry no longer describes the project AGENTS.md" <<'CMD'
grep -q -F '{Name: "/init", Help: "write or update AGENTS.md"}' internal/tui/help.go && exit 1
echo "/init help entry changed"; exit 0
CMD
expect_fail "negative: only internal/tui changed in production code" <<'CMD'
out="$(git diff --name-only 81ebe7e -- internal/provider internal/tools cmd/yip; git ls-files --others --exclude-standard -- internal/provider internal/tools cmd/yip)"
[ -n "$out" ] && { echo "$out"; exit 0; }
exit 1
CMD
expect_ok "tui tests, vet, gofmt" <<'CMD'
go test -count=1 ./internal/tui ./cmd/yip && go vet ./internal/tui && test -z "$(gofmt -l internal/tui)"
CMD
finish
