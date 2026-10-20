#!/usr/bin/env bash
# Shared helpers for scripts/verify/<version>/<ID>.sh (supervisor-owned; workers never edit it).
# Source it, never execute it:   . "$(dirname "$0")/../_lib.sh"
#
#   expect_ok   "<label>" <<'CMD' ... CMD   passes when the command exits 0
#   expect_fail "<label>" <<'CMD' ... CMD   passes only on an ordinary failure (exit 1..123);
#                                           a timeout (124), a broken or missing command
#                                           (125..127) or a signal (>128) never counts
#   finish                                  prints the summary, exits 1 if any check failed
#
# Each command is read from stdin (use a quoted heredoc: nothing is expanded or escaped),
# then run with `bash -c` from the repository root under `timeout` (VERIFY_TIMEOUT seconds,
# default 600). Exported for the commands: ROOT (repo root), VTMP (a per-script temp dir,
# removed on exit; also TMPDIR) and the helper functions below.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VTMP="$(mktemp -d "${TMPDIR:-/tmp}/verify.XXXXXX")"
export ROOT VTMP
export TMPDIR="$VTMP"
trap 'chmod -R u+w "$VTMP" 2>/dev/null; rm -rf "$VTMP"' EXIT
: "${VERIFY_TIMEOUT:=600}"
_v_pass=0
_v_fail=0

_v_run() { # <ok|fail> <label>; command on stdin
	local mode="$1" label="$2" cmd out rc
	cmd="$(cat)"
	out="$(cd "$ROOT" && timeout --kill-after=10 "$VERIFY_TIMEOUT" bash -c "$cmd" 2>&1 </dev/null)"
	rc=$?
	local good=0
	if [ "$mode" = ok ]; then
		[ "$rc" -eq 0 ] && good=1
	else
		[ "$rc" -ge 1 ] && [ "$rc" -le 123 ] && good=1
	fi
	if [ "$good" -eq 1 ]; then
		_v_pass=$((_v_pass + 1))
		printf 'PASS  %s\n' "$label"
	else
		_v_fail=$((_v_fail + 1))
		local why="exit $rc"
		[ "$rc" -eq 124 ] && why="timeout after ${VERIFY_TIMEOUT}s"
		[ "$mode" = fail ] && [ "$rc" -eq 0 ] && why="exit 0, but a failure was required"
		printf 'FAIL  %s (%s)\n' "$label" "$why"
		printf '%s\n' "$out" | tail -n 40 | sed 's/^/      | /'
	fi
}

expect_ok() { _v_run ok "$1"; }
expect_fail() { _v_run fail "$1"; }

finish() {
	printf -- '-- %d passed, %d failed\n' "$_v_pass" "$_v_fail"
	[ "$_v_fail" -eq 0 ]
	exit $?
}

# go_tests <package> <TestName>...
# Runs exactly these top-level tests fresh (-count=1); fails if one fails or did not run.
go_tests() {
	local pkg="$1" re out rc t
	shift
	re="^($(
		IFS='|'
		printf '%s' "$*"
	))\$"
	out="$(go test -count=1 -v -run "$re" "$pkg" 2>&1)"
	rc=$?
	if [ "$rc" -ne 0 ]; then
		printf '%s\n' "$out" | grep -v -e '^=== ' | tail -n 60
		return 1
	fi
	for t in "$@"; do
		if ! printf '%s\n' "$out" | grep -q -e "^--- PASS: $t "; then
			echo "test did not run (missing or misnamed): $t in $pkg"
			return 1
		fi
	done
}

# has_symbols <package> <Symbol>...   e.g. has_symbols ./internal/agent Session.Submit ErrBusy
has_symbols() {
	local pkg="$1" s
	shift
	for s in "$@"; do
		go doc "$pkg" "$s" >/dev/null 2>&1 || {
			echo "missing exported symbol: $pkg $s"
			return 1
		}
	done
}

# build_probe <file.go>   builds a supervisor probe (//go:build ignore) into $VTMP/probe.
# The probe compiles against the exported API a brief prescribes, so a build failure
# means the API differs from the brief.
build_probe() {
	go build -o "$VTMP/probe" "$1"
}

# probe_ok <case>          positive case: the probe exits 0 when the required behaviour holds.
probe_ok() {
	"$VTMP/probe" "$@"
}

# probe_rejects <case>     for expect_fail: exits 1 only when the probe printed "REJECTED:" and
# exited 1 (the unsafe or invalid input was refused); exits 0 when the probe reports the input
# was accepted; exits 125 on a crash, a setup error or a missing probe (never an expected failure).
probe_rejects() {
	local out rc
	out="$("$VTMP/probe" "$@" 2>&1)"
	rc=$?
	printf '%s\n' "$out"
	if [ "$rc" -eq 1 ] && printf '%s\n' "$out" | grep -q -e '^REJECTED:'; then
		return 1
	fi
	if [ "$rc" -eq 0 ]; then
		return 0
	fi
	return 125
}

# no_match <grep args...>  exits 1 when grep finds nothing (for expect_fail: "must not appear"),
# 0 when it finds something (printed), 125 when grep itself fails (e.g. a missing path).
no_match() {
	local rc
	grep "$@"
	rc=$?
	[ "$rc" -eq 0 ] && return 0
	[ "$rc" -eq 1 ] && return 1
	return 125
}

export -f go_tests has_symbols build_probe probe_ok probe_rejects no_match
