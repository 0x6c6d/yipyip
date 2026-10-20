#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: redactor" <<'CMD'
go_tests ./internal/config TestRedactorReplaces TestRedactorOverlapping TestRedactorNilAndEmpty TestRedactorConcurrent TestRedactorJSONEscaped TestSecretRedactor
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T201.go
CMD
expect_ok "probe: basic redaction, nil and empty" <<'CMD'
probe_ok basic
CMD
expect_fail "negative: no secret survives (adjacent, quoted, Bearer, concurrent)" <<'CMD'
probe_rejects leak
CMD
expect_fail "negative: overlapping secrets leave no fragment" <<'CMD'
probe_rejects overlap
CMD
finish
