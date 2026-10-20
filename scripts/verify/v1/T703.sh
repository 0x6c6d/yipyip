#!/usr/bin/env bash
# Verification for T703 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "e2e tests: CLI" <<'CMD'
go_tests ./e2e TestCLIVersionAnyDir TestCLIHelpListsCommands TestCLIMissingEnvExit1 TestCLIInvalidConfigExit1
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "make install into a temp GOBIN; --version from / without key or config" <<'CMD'
GOBIN="$VTMP/gobin" make install >/dev/null || exit 1
(cd / && env -u OPENROUTER_API_KEY HOME="$VTMP" "$VTMP/gobin/yip" --version) | grep -q '^yip '
CMD
expect_ok "DoD D11: make install, then yip --version from /" <<'CMD'
env -u OPENROUTER_API_KEY sh -c 'make install && cd / && command -v yip && yip --version' | tail -1 | grep -q '^yip '
CMD
expect_ok "DoD D12: yip --help from an empty temp dir lists the commands" <<'CMD'
sh -c 'o="$(cd "$(mktemp -d)" && yip --help)" || exit 1; for c in /model /effort /init /new /exit /compact /undo /mode /agents /help; do printf %s "$o" | grep -qF -- "$c" || exit 1; done'
CMD
expect_fail "negative: installed yip with an unset variable exits 1 without leaking the other value" <<'CMD'
[ -x "$VTMP/gobin/yip" ] || GOBIN="$VTMP/gobin" make install >/dev/null || exit 125
h="$VTMP/home"; d="$VTMP/start"; mkdir -p "$h/.config/yipyip" "$d" || exit 125
printf '%s\n' '{"provider":{"p":{"name":"{env:FAKE_KEY}","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:YIP_UNSET_PROBE}"}}}}' > "$h/.config/yipyip/config.json"
err="$(cd "$d" && env -i PATH="$PATH" HOME="$h" FAKE_KEY=SECRET123 "$VTMP/gobin/yip" 2>&1 </dev/null)"; rc=$?
echo "rc=$rc $err"
if [ "$rc" -eq 1 ] && printf '%s' "$err" | grep -q YIP_UNSET_PROBE && ! printf '%s' "$err" | grep -q SECRET123; then exit 1; fi
exit 0
CMD
expect_fail "negative (D13): no non-test Go file names the real provider host" <<'CMD'
no_match -rn 'openrouter.ai' --include='*.go' --exclude='*_test.go' .
CMD
expect_fail "negative (D13): Makefile names no proxy or provider" <<'CMD'
no_match -n -e 'proxy.golang.org' -e 'openrouter' Makefile
CMD
expect_fail "negative: no default HTTP client/transport in non-test code" <<'CMD'
no_match -rnF -e 'http.DefaultClient' -e 'http.DefaultTransport' -e 'http.Get(' -e 'http.Post(' --include='*.go' --exclude='*_test.go' cmd internal
CMD
finish
