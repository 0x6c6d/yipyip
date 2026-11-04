#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go.mod: module yipyip, go 1.27" <<'CMD'
grep -qx 'module yipyip' go.mod && grep -qx 'go 1.27' go.mod
CMD
expect_ok ".gitignore ignores bin/" <<'CMD'
grep -qx 'bin/' .gitignore
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "tests: TestVersionFlag TestUnknownFlag" <<'CMD'
go_tests ./cmd/yip TestVersionFlag TestUnknownFlag
CMD
expect_ok "phase 1 exit criterion: go run ./cmd/yip --version prints ^yip" <<'CMD'
go run ./cmd/yip --version | grep -q '^yip '
CMD
expect_ok "make build: bin/yip carries the git describe version" <<'CMD'
make build >/dev/null || exit 1
out="$(bin/yip --version)" || exit 1
echo "$out"
[ "$out" = "yip $(git describe --tags --always --dirty)" ]
CMD
expect_ok "make install honours GOBIN" <<'CMD'
GOBIN="$VTMP/gobin" make install >/dev/null && "$VTMP/gobin/yip" --version | grep -q '^yip '
CMD
expect_ok "make install falls back to GOPATH/bin when GOBIN is empty" <<'CMD'
env GOBIN= GOPATH="$VTMP/gopath" GOMODCACHE="$(go env GOMODCACHE)" make install >/dev/null && "$VTMP/gopath/bin/yip" --version | grep -q '^yip '
CMD
expect_fail "negative: unknown flag is rejected" <<'CMD'
make build >/dev/null || exit 125
bin/yip --bogus-flag
CMD
expect_fail "negative: Makefile names no proxy or provider host" <<'CMD'
no_match -n -e 'proxy.golang.org' -e 'openrouter' Makefile
CMD
finish
