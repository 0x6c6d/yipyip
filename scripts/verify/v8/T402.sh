#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R70: HTML-to-text tests pass" <<'CMD'
go_tests ./internal/tools TestHTMLToText TestHTMLToTextLarge
CMD
expect_ok "htmlToText is defined in internal/tools/htmltext.go" <<'CMD'
grep -q -e '^func htmlToText(s string) string' internal/tools/htmltext.go
CMD
expect_ok "test covers script, style, comments and entities" <<'CMD'
f=internal/tools/htmltext_test.go
grep -q -e 'SCRIPTX' "$f" && grep -q -e 'color:red' "$f" && grep -q -e 'COMMENTX' "$f" && grep -q -F -e '&amp;' "$f" && grep -q -i -e 'noscript' "$f" && grep -q -i -e 'template' "$f"
CMD
expect_fail "negative: only standard-library imports in htmltext.go" <<'CMD'
no_match -n -e '"golang.org/' -e '"github.com/' internal/tools/htmltext.go
CMD
expect_ok "go.mod and go.sum unchanged since a99a82f" <<'CMD'
git diff --quiet a99a82f -- go.mod go.sum
CMD
expect_fail "negative: TestHTMLToText fails on a copy whose converter returns its input unchanged" <<'CMD'
m="$VTMP/mut"; mkdir -p "$m"
git ls-files -z -c -o --exclude-standard | tar --null --ignore-failed-read -T - -cf - 2>/dev/null | tar -xf - -C "$m" || exit 125
cd "$m" || exit 125
f=internal/tools/htmltext.go
grep -q -e '^func htmlToText(s string) string {' "$f" || exit 125
sed -i 's/^func htmlToText(s string) string {$/func htmlToText(s string) string {\n\tif true {\n\t\treturn s\n\t}/' "$f"
out="$(go test ./internal/tools -count=1 -run '^TestHTMLToText$' 2>&1)"
printf '%s\n' "$out" | tail -n 15
printf '%s\n' "$out" | grep -q -e '^--- FAIL: TestHTMLToText' && exit 1
exit 0
CMD
expect_ok "scope: only internal/tools/htmltext.go and its test changed" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/tools/htmltext.go$' -e '^internal/tools/htmltext_test.go$')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
