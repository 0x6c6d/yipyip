#!/usr/bin/env bash
# Verification for T701 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "DoD D10: go test ./... -run Secret" <<'CMD'
go test -count=1 ./... -run Secret
CMD
expect_ok "every secret-handling package ran a passing TestSecret* test" <<'CMD'
for p in internal/config internal/provider internal/tools internal/agent internal/tui e2e; do
	out="$(go test -count=1 -v -run '^TestSecret' "./$p" 2>&1)"
	printf '%s\n' "$out" | grep -q -e '^--- PASS: TestSecret' || { echo "no passing TestSecret* in $p"; printf '%s\n' "$out" | tail -n 20; exit 1; }
done
CMD
expect_ok "named secret tests" <<'CMD'
go_tests ./internal/agent TestSecretAgentEvents && go_tests ./e2e TestSecretEndToEnd TestSecretMissingEnvError
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: startup with an unset variable exits 1, names it, leaks no value" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip || exit 125
h="$VTMP/home"; d="$VTMP/start"; mkdir -p "$h/.config/yipyip" "$d" || exit 125
printf '%s\n' '{"provider":{"p":{"name":"{env:FAKE_KEY}","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:YIP_UNSET_PROBE}"},"models":{"m":{}}}}}' > "$h/.config/yipyip/config.json"
err="$(cd "$d" && env -i PATH="$PATH" HOME="$h" FAKE_KEY=SECRET123 "$VTMP/yip" 2>&1 </dev/null)"; rc=$?
echo "rc=$rc $err"
if [ "$rc" -eq 1 ] && printf '%s' "$err" | grep -q YIP_UNSET_PROBE && ! printf '%s' "$err" | grep -q SECRET123; then exit 1; fi
exit 0
CMD
expect_fail "negative: no file under the temp HOME or start dir contains the key" <<'CMD'
[ -d "$VTMP/home" ] || exit 125
no_match -rl SECRET123 "$VTMP/home" "$VTMP/start"
CMD
finish
