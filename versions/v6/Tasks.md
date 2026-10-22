# Tasks v6: yipyip (web search via Brave, reads outside the start directory)

_Generated from versions/v6/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v6/tasks/<ID>.md`,
verifications in `scripts/verify/v6/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`).
- Protected (never modify): `Plan.md`, `README.md` (the whole file; its sections 1–9 are edited only by the supervisor row T402), `CLAUDE.md`, `prompts/`, `.claude/`, `versions/`, `scripts/verify/`.
- Security-critical files: `internal/provider/transport.go` is changed only by T102, `internal/agent/approval.go` only by T201, `internal/tools/pathguard.go` only by T301; `internal/tools/bash.go` and `internal/config/redact.go` are changed by no task. All other code calls them and never re-implements host checks, path confinement, exec, approval or redaction. HTTP clients are built only in `transport.go`.
- `go.mod` and `go.sum` stay unchanged against the baseline commit `ede3886` (no new modules; standard library and the existing dependencies only).
- Offline only: tests use the `httptest` fakes in `internal/provider/providertest` (fake provider, fake System One `NewJev`, fake Brave `NewBrave` once T103 is done) and temp dirs (`t.TempDir()`, temp HOME); no test or Makefile target contacts a real host (never `api.search.brave.com`, `api.typesafe.ai` or OpenRouter); no test needs `BRAVE_API_KEY`, `TYPESAFE_API_KEY` or any real secret. The only host a worker may contact is `proxy.golang.org` (via `go mod download`).
- Non-test code never uses `http.DefaultClient`, `http.DefaultTransport` or `http.Get`/`Post`/`Head`; `os/exec` only in `internal/tools/bash.go` and `internal/tui/clipboard.go`.
- Secrets: every text shown in the UI, sent to a model, jev or the search service, or written to a file passes the config Redactor; the test secret `SECRET123` must never appear in a view, request body, search query or written file.
- No commits, no `git init`/`reset`/`stash`/`checkout` of other revisions (reading old revisions with `git show <rev>:<path>` is allowed); leave changes in the working tree for the supervisor. Change only the files your brief names (plus new `_test.go` files in the named package).
- Test names are exactly those in the brief (top-level `func TestXxx`); TUI tests drive `Model.Update` / `Model.Command` and `View()` (no teatest); every new test must finish in well under 10 s.
- Older tests that assert behaviour v6 changes (read confinement of `read_file`/`glob`/`grep`, "You have no web access" in every system prompt) are updated only by the task whose brief says so (T302, T303).

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | verify T000.sh 13/13 |
| T101 | 1 | Config: optional search block | T000 | opus | done | config/search.go: Search block, https/loopback rule; 8 tests |
| T102 | 1 | Search transport: search-only HTTP client (transport.go) | T101 | opus | done | transport.go: NewSearchHTTPClient (search host only, 20s, redirect check shared); 6 tests |
| T103 | 1 | Brave search client and fake Brave endpoint | T102 | sonnet | done | provider/search.go SearchClient, providertest/brave.go fake; 6 tests |
| T104 | 1 | web_search tool and registry name rule | T103 | opus | done | tools/websearch.go + Options.Search, name rule exact web_search; 12 tests |
| T201 | 2 | Approval: web_search in the gated set (approval.go) | T000 | opus | done | approval.go: web_search in gatedTools (+2 lines); 6 gate tests |
| T202 | 2 | Session wiring of web_search, agent gate and Secret tests | T104, T201 | opus | done | session.go: search client wired to tools.Options.Search; 7 tests |
| T203 | 2 | e2e: web_search Secret tests | T202 | opus | done | e2e/websearch_secret_test.go: 2 Secret tests, BRAVE_API_KEY in getenv stub |
| T204 | 2 | TUI: approval summary shows the query, /help line | T000 | sonnet | done | tui approval summary = query, /help web_search block; 3 tests |
| T301 | 3 | PathGuard read mode ResolveRead (pathguard.go) | T000 | opus | todo | |
| T302 | 3 | read_file, glob, grep read anywhere; writes stay confined | T301 | sonnet | todo | |
| T303 | 3 | System prompt read/write rule and web_search; undo stays inside | T202, T301 | sonnet | todo | |
| T401 | 4 | Docs: docs/usage.md for search and reads | T104, T202, T204, T302, T303 | haiku | todo | |
| T402 | 4 | README sections 1-9: R3, R4, R7 and related lines (supervisor edit) | T102, T202, T302, T401 | supervisor | todo | |
| T799 | 4 | Global Definition of Done | all | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v6`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v6/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v6/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v6/plan.md`; done when it passes.
