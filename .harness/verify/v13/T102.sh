#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named unit tests pass" <<'CMD'
go_tests ./internal/agent TestGlobalAgentsOrder TestGlobalAgentsAbsent TestGlobalAgentsTruncate TestGlobalAgentsRedact TestGlobalAgentsSymlink
CMD
expect_ok "supervisor probe test runs and writes prompts" <<'CMD'
dst=internal/agent/zz_v13probe_test.go
trap 'rm -f "$dst"' EXIT
cp .harness/verify/v13/probes/T102_probe_test.go.in "$dst"
mkdir -p "$VTMP/p"
V13_OUT="$VTMP/p" go test -count=1 -run '^TestV13Probe$' ./internal/agent
CMD
expect_ok "both notes, global section before project section" <<'CMD'
f="$VTMP/p/both.txt"
g=$(grep -n -x -F '# Global AGENTS.md' "$f" | head -1 | cut -d: -f1)
p=$(grep -n -x -F '# AGENTS.md' "$f" | head -1 | cut -d: -f1)
gn=$(grep -n -F 'GLOBAL-PROBE-1' "$f" | head -1 | cut -d: -f1)
pn=$(grep -n -F 'PROJECT-PROBE-1' "$f" | head -1 | cut -d: -f1)
[ -n "$g" ] && [ -n "$p" ] && [ -n "$gn" ] && [ -n "$pn" ] && [ "$g" -lt "$gn" ] && [ "$gn" -lt "$p" ] && [ "$p" -lt "$pn" ]
CMD
expect_ok "global-only puts the section at the end of the prompt" <<'CMD'
f="$VTMP/p/global-only.txt"
tail -n 2 "$f" | head -1 | grep -q -x -F '# Global AGENTS.md' && tail -n 1 "$f" | grep -q -x -F 'GLOBAL-PROBE-1'
CMD
expect_ok "absent global files leave the prompt byte-identical" <<'CMD'
for c in empty ws dir dangling; do cmp "$VTMP/p/none.txt" "$VTMP/p/$c.txt" || { echo "differs: $c"; exit 1; }; done
CMD
expect_fail "negative: no global heading for empty, whitespace, directory, dangling, none" <<'CMD'
no_match -l -F '# Global AGENTS.md' "$VTMP/p/none.txt" "$VTMP/p/none-noproject.txt" "$VTMP/p/empty.txt" "$VTMP/p/ws.txt" "$VTMP/p/dir.txt" "$VTMP/p/dangling.txt"
CMD
expect_ok "symlink to a regular file is read" <<'CMD'
grep -q -F 'GLOBAL-LINKED-1' "$VTMP/p/symlink.txt" && grep -q -x -F '# Global AGENTS.md' "$VTMP/p/symlink.txt"
CMD
expect_ok "secret is redacted in the global section" <<'CMD'
grep -q -F 'before [redacted] after' "$VTMP/p/secret.txt"
CMD
expect_fail "negative: raw secret never enters the prompt" <<'CMD'
no_match -F 'SEKRET-V13-PROBE' "$VTMP/p/secret.txt"
CMD
expect_ok "40,000-character file cut to exactly 30,000 characters" <<'CMD'
grep -q -x -E 'x{30000}' "$VTMP/p/long.txt"
CMD
expect_fail "negative: text past 30,000 characters never appears" <<'CMD'
no_match -F 'TAILMARK' "$VTMP/p/long.txt"
CMD
expect_ok "Reset re-reads the global file" <<'CMD'
grep -q -F 'GLOBAL-OLD-1' "$VTMP/p/reset-before.txt" && grep -q -F 'GLOBAL-NEW-2' "$VTMP/p/reset-after.txt" && ! grep -q -F 'GLOBAL-OLD-1' "$VTMP/p/reset-after.txt"
CMD
expect_fail "negative: prompt.go imports no network or exec package" <<'CMD'
no_match -n -e '"net/http"' -e '"os/exec"' -e '"net"' internal/agent/prompt.go
CMD
expect_fail "negative: init.go does not read the global file" <<'CMD'
no_match -n -e 'GlobalDir' -e 'readGlobalAgentsFile' internal/agent/init.go
CMD
expect_fail "negative: no changes in provider, tools, cmd/yip" <<'CMD'
out="$(git diff --name-only 81ebe7e -- internal/provider internal/tools cmd/yip; git ls-files --others --exclude-standard -- internal/provider internal/tools cmd/yip)"
[ -n "$out" ] && { echo "$out"; exit 0; }
exit 1
CMD
expect_ok "agent package tests, vet, gofmt" <<'CMD'
test ! -e internal/agent/zz_v13probe_test.go && go test -count=1 ./internal/agent ./internal/config && go vet ./internal/agent && test -z "$(gofmt -l internal/agent)"
CMD
finish
