#!/usr/bin/env bash
# Verification for T303 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: registry" <<'CMD'
go_tests ./internal/tools TestRegistryNoWebTools TestRegistryUnknownTool TestRegistryInvalidArgs TestBashToolOutput TestToolSchemasValidJSON TestSecretToolResult
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T303.go
CMD
expect_ok "probe: bash tool output format" <<'CMD'
probe_ok exit
CMD
expect_fail "negative: no web/fetch/search tool registered; bash gated" <<'CMD'
probe_rejects names
CMD
expect_fail "negative: unknown tool refused" <<'CMD'
probe_rejects unknown
CMD
expect_fail "negative: invalid arguments refused" <<'CMD'
probe_rejects badargs
CMD
expect_fail "negative: secret printed by bash is redacted" <<'CMD'
probe_rejects leak
CMD
expect_fail "negative: StripEnv applied to the bash tool" <<'CMD'
probe_rejects strip
CMD
finish
