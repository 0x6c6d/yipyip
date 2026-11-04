#!/usr/bin/env bash
# v10 probe helpers (supervisor-owned; workers never edit it). Source it after _lib.sh:
#   . "$(dirname "$0")/_probe.sh"
# Same contract as v9: probe_src stores Go source from stdin as $VTMP/<name>.go;
# go_probe / go_probe_rejects copy it into a package dir as zz_verify_probe_test.go for one
# `go test` run (removed right after) so it can reach unexported code and the package's helpers.
# Probe identifiers start with TestZZV / zzv.

probe_src() { cat >"$VTMP/$1.go"; }

_probe_run() { # <pkgdir> <name> <TestName>
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

# Unchanged since the v10 start commit: go.mod, go.sum and the five security files.
v10_guard() {
	local files="go.mod go.sum internal/provider/transport.go internal/tools/pathguard.go internal/tools/bash.go internal/agent/approval.go internal/config/redact.go"
	# shellcheck disable=SC2086
	git -C "$ROOT" diff --quiet 1ccbbc7 -- $files || {
		echo "changed since 1ccbbc7:"
		# shellcheck disable=SC2086
		git -C "$ROOT" diff --stat 1ccbbc7 -- $files
		return 1
	}
}

# Only the paths a task may touch changed since HEAD (the last commit): fails when git status
# shows any other modified or untracked path. Usage: only_changed <prefix-or-file>...
only_changed() {
	local bad p ok f
	bad=""
	while IFS= read -r p; do
		[ -z "$p" ] && continue
		p="${p:3}"
		ok=0
		for f in "$@"; do
			case "$p" in "$f"*) ok=1 ;; esac
		done
		[ "$ok" -eq 0 ] && bad="$bad $p"
	done < <(git -C "$ROOT" status --porcelain)
	if [ -n "$bad" ]; then
		echo "unexpected changes:$bad"
		return 1
	fi
}

export -f probe_src _probe_run go_probe go_probe_rejects v10_guard only_changed
