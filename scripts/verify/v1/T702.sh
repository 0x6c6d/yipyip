#!/usr/bin/env bash
# Verification for T702 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "config.example.json equals the README example" <<'CMD'
cat > "$VTMP/want.json" <<'JSON'
{
  "max_subagents": 4,
  "auto_compact": true,
  "provider": {
    "openrouter": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "OpenRouter",
      "options": {
        "baseURL": "https://openrouter.ai/api/v1",
        "apiKey": "{env:OPENROUTER_API_KEY}"
      },
      "models": {}
    }
  }
}
JSON
cmp config.example.json "$VTMP/want.json"
CMD
expect_ok "tests: TestExampleConfigLoads" <<'CMD'
go_tests ./internal/config TestExampleConfigLoads
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative (D13): no non-test Go file names the real provider host" <<'CMD'
no_match -rn 'openrouter.ai' --include='*.go' --exclude='*_test.go' .
CMD
finish
