#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: env substitution, models, secrets" <<'CMD'
go_tests ./internal/config TestEnvSubstitution TestMissingEnvNamesVariable TestEmptyEnvIsMissing TestCostIgnored TestModelParsing TestDefaultModelID TestSecretConfig
CMD
expect_ok "earlier config tests still pass" <<'CMD'
go_tests ./internal/config TestLoadGlobal TestProjectOverride TestLoadMissingFiles TestLoadInvalidJSON TestSecretRedactor
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T202.go
CMD
expect_ok "probe: load, override, {env:}, models, cost ignored, defaults" <<'CMD'
probe_ok load
CMD
expect_fail "negative: unset variable is an error naming it, without values" <<'CMD'
probe_rejects missing
CMD
expect_fail "negative: empty variable is an error" <<'CMD'
probe_rejects empty
CMD
expect_fail "negative: malformed project JSON is an error naming the file" <<'CMD'
probe_rejects malformed
CMD
finish
