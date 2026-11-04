#!/usr/bin/env bash
# v9 probe helpers (supervisor-owned; workers never edit it). Source it after _lib.sh:
#   . "$(dirname "$0")/_probe.sh"
#
# A probe is a supervisor-written Go test file. For exactly one `go test` run it is copied into a
# package directory as zz_verify_probe_test.go, so it can reach unexported code (renderMarkdown,
# resetTab, Model fields) and the package's test helpers; it is removed right after the run, also
# on a timeout or an interrupt. Probe identifiers start with TestZZV / zzv.
#
#   probe_src <name>                         stores Go source from stdin as $VTMP/<name>.go
#                                            (call it at the script's top level, before the checks)
#   go_probe <pkgdir> <name> <TestName>      0 = the probe test passed; 1 = it failed;
#                                            125 = build failure, panic or the test did not run
#   go_probe_rejects <pkgdir> <name> <Test>  for expect_fail: 1 only when the test failed with a
#                                            "REJECTED:" message (the unsafe or invalid case was
#                                            refused); 0 when it passed (it was accepted); else 125

probe_src() { cat >"$VTMP/$1.go"; }

_probe_run() { # <pkgdir> <name> <TestName>; output kept in $VTMP/probe-<name>.out
	local dir="$ROOT/$1" src="$VTMP/$2.go" test="$3" out="$VTMP/probe-$2.out" dst rc
	dst="$dir/zz_verify_probe_test.go"
	if [ ! -d "$dir" ] || [ ! -f "$src" ]; then
		echo "probe setup: missing $dir or $src"
		return 125
	fi
	if [ -e "$dst" ]; then
		echo "probe setup: $dst already exists"
		return 125
	fi
	trap "rm -f '$dst'" EXIT
	trap "rm -f '$dst'; exit 125" INT TERM
	cp "$src" "$dst" || return 125
	(cd "$ROOT" && go test -count=1 -v -run "^${test}\$" "./$1") >"$out" 2>&1
	rc=$?
	rm -f "$dst"
	trap - EXIT INT TERM
	grep -v -e '^=== ' "$out" | tail -n 40
	if grep -q -e '\[build failed\]' -e '\[setup failed\]' -e '^panic: ' "$out"; then
		echo "probe $test: build failure or panic"
		return 125
	fi
	if [ "$rc" -eq 0 ] && grep -q -e "^--- PASS: ${test} " "$out"; then
		return 0
	fi
	if [ "$rc" -eq 1 ] && grep -q -e "^--- FAIL: ${test} " "$out"; then
		return 1
	fi
	echo "probe $test did not run cleanly (exit $rc)"
	return 125
}

go_probe() { _probe_run "$@"; }

go_probe_rejects() {
	local rc
	_probe_run "$@"
	rc=$?
	if [ "$rc" -eq 0 ]; then
		return 0
	fi
	if [ "$rc" -eq 1 ]; then
		if grep -q -e 'REJECTED:' "$VTMP/probe-$2.out"; then
			return 1
		fi
		echo "probe $3 failed without a REJECTED: message"
	fi
	return 125
}

# Unchanged since the v9 start commit: go.mod, go.sum and the five security files (A17, spec §5).
v9_guard() {
	git -C "$ROOT" diff --quiet a4e4cbb -- go.mod go.sum internal/provider/transport.go \
		internal/tools/pathguard.go internal/tools/bash.go internal/agent/approval.go \
		internal/config/redact.go || {
		echo "changed since a4e4cbb:"
		git -C "$ROOT" diff --stat a4e4cbb -- go.mod go.sum internal/provider/transport.go \
			internal/tools/pathguard.go internal/tools/bash.go internal/agent/approval.go \
			internal/config/redact.go
		return 1
	}
}

export -f probe_src _probe_run go_probe go_probe_rejects v9_guard
