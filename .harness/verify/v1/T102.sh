#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: help text" <<'CMD'
go_tests ./internal/tui TestHelpTextListsCommands TestHelpTextNoCost
CMD
expect_ok "tests: TestHelpFlag" <<'CMD'
go_tests ./cmd/yip TestHelpFlag TestVersionFlag
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "yip --help in an empty dir, empty HOME, no key: lists the slash commands" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip || exit 1
d="$(mktemp -d)"; h="$(mktemp -d)"
o="$(cd "$d" && env -u OPENROUTER_API_KEY HOME="$h" "$VTMP/yip" --help)" || exit 1
for c in /model /effort /init /new /exit /compact /undo /mode /agents /help; do
	printf '%s\n' "$o" | grep -qF -- "$c" || { echo "missing $c"; exit 1; }
done
CMD
expect_fail "negative: help text mentions no cost, price or \$" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip || exit 125
o="$("$VTMP/yip" --help)" || exit 125
printf '%s\n' "$o" | no_match -i -e cost -e price -e '\$'
CMD
finish
