# Tasks v1: yipyip (`yip`)

_Generated from versions/v1/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v1/tasks/<ID>.md`,
verifications in `scripts/verify/v1/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` (gofmt -l . empty, go vet ./..., go test ./..., go build ./...) must pass for the whole repo once the Makefile exists (T101 creates it).
- Protected (never modify): `Plan.md`, `README.md`, `versions/`, `scripts/verify/` (incl. `scripts/verify/v1/probes/`), `prompts/`, `.claude/`, and the "Delegation workflow" section of `CLAUDE.md`. Documented exceptions, only in T704: append a `## Usage` section at the end of `README.md` (README §7 and Plan.md allow exactly this append) and update the other sections of `CLAUDE.md`.
- No commits, no `git init`, no history rewriting, no `git stash`/`reset`/`clean`: the supervisor commits.
- Network: only `proxy.golang.org`, only via `go get`/`go mod download`/`go mod tidy`. Never contact `openrouter.ai` or any other host; tests run offline against `internal/provider/providertest` (httptest on 127.0.0.1). No real provider host in non-test `.go` files or in the Makefile.
- Go 1.27, module `yipyip`. Third-party modules allowed, exactly these versions: `github.com/charmbracelet/bubbletea` v1.3.10, `github.com/charmbracelet/bubbles` v0.21.1, `github.com/charmbracelet/lipgloss` v1.1.0, `github.com/alecthomas/chroma/v2` v2.27.0 (plus their indirect dependencies). Everything else is the standard library (own SSE parser, own `**` glob).
- Exported names and signatures given in a brief are a contract: later tasks and supervisor probes compile against them. Implement them exactly (parameter names may differ); add unexported helpers freely.
- Security-critical logic lives only in `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go` (the only non-test `os/exec` user), `internal/agent/approval.go`, `internal/config/redact.go`; other code calls them and never re-implements them. Non-test code never uses `http.DefaultClient`, `http.DefaultTransport`, `http.Get`/`Post`/`Head`.
- Secrets: never write a real key or any secret into a file; tests use the fake value `SECRET123` via injected getenv/`t.Setenv`. yipyip writes no logs, history, session or config files; tests use `t.TempDir()` for HOME and the start directory and never touch the real HOME.
- Tests are part of each task: name them exactly as the brief says, keep them offline, deterministic and fast (no sleeps longer than needed, every wait bounded); never delete, skip or weaken existing tests. TUI tests drive `Model.Update`/`View()` directly (no teatest).
- Install nothing globally; report a missing tool instead. Only T101/T703 run `make install`, as their briefs say.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | preflight passed |
| T101 | 1 | Go module, Makefile, .gitignore, --version | T000 | haiku | done | go.mod, Makefile, .gitignore, cmd/yip; /yip ignored (go build ./... writes it while one package exists) |
| T102 | 1 | --help usage text and slash-command list | T101 | haiku | done | internal/tui/help.go, cmd/yip --help |
| T103 | 1 | Config loader: files, deep merge, defaults | T101 | sonnet | done | internal/config/config.go (+test) |
| T104 | 1 | TUI skeleton and main wiring | T102, T103 | sonnet | done | internal/tui/model.go, cmd/yip wiring, bubbletea deps |
| T201 | 2 | Redactor for secret values | T101 | opus | done | internal/config/redact.go (+test) |
| T202 | 2 | {env:NAME} substitution, providers and models | T103, T201 | opus | done | internal/config/env.go, config.go providers (+tests) |
| T203 | 2 | Host-allowlist HTTP transport | T101 | opus | done | internal/provider/transport.go (+test); redirect cap = 10 requests total |
| T204 | 2 | providertest fake SSE provider | T101 | sonnet | done | internal/provider/providertest (+test) |
| T205 | 2 | Streaming chat client and SSE parser | T201, T203, T204 | sonnet | done | internal/provider/client.go, sse.go (+test); EOF without DONE is a normal end (T206 to handle) |
| T206 | 2 | Provider errors, dropped streams, redacted error text | T205 | opus | done | client.go errors/drop/cancel, errors_test.go; client_test.go one expectation changed to "HTTP 500: boom" per brief |
| T301 | 3 | PathGuard start-directory confinement | T101 | opus | done | internal/tools/pathguard.go (+test); also rejects NUL bytes and empty start dir |
| T302 | 3 | Bash runner | T101 | opus | todo | |
| T303 | 3 | Tool registry and the bash tool | T201, T301, T302 | opus | todo | |
| T304 | 3 | read_file, write_file, edit_file | T303 | sonnet | todo | |
| T305 | 3 | glob with ** and grep | T303 | sonnet | todo | |
| T401 | 4 | Approval gate | T101 | opus | todo | |
| T402 | 4 | Agent session core: state, events, system prompt | T202, T205, T303, T401 | opus | todo | |
| T403 | 4 | Agent loop, token accounting, cancellation | T206, T304, T305, T402 | opus | todo | |
| T404 | 4 | Subagent manager | T101 | opus | todo | |
| T405 | 4 | Subagent tools in the session | T403, T404 | opus | todo | |
| T501 | 5 | TUI connected to the agent session | T104, T403 | sonnet | todo | |
| T502 | 5 | Status line | T501 | sonnet | todo | |
| T503 | 5 | Slash-command dispatcher, completion, Shift+Tab | T502 | sonnet | todo | |
| T504 | 5 | /model picker and direct selection | T503 | sonnet | todo | |
| T505 | 5 | Approval prompt in the TUI | T503 | sonnet | todo | |
| T506 | 5 | Tool blocks, collapsible results, syntax highlighting | T501 | sonnet | todo | |
| T507 | 5 | Subagent panel and /agents | T503, T405 | sonnet | todo | |
| T601 | 6 | Undo journal | T405 | opus | todo | |
| T602 | 6 | /undo command and e2e Undo tests | T601, T503 | sonnet | todo | |
| T603 | 6 | Compaction, manual and automatic | T601 | sonnet | todo | |
| T604 | 6 | /compact command and e2e Compact tests | T603, T602 | sonnet | todo | |
| T605 | 6 | /init and e2e Init test | T602 | sonnet | todo | |
| T701 | 7 | Secret audit across all packages | T504, T505, T506, T507, T604, T605 | opus | todo | |
| T702 | 7 | config.example.json | T202 | haiku | todo | |
| T703 | 7 | CLI end to end: install, start dir, startup errors | T501, T702 | sonnet | todo | |
| T704 | 7 | Docs: docs/usage.md, README Usage append, CLAUDE.md | T701, T702, T703 | haiku | todo | |
| T799 | 7 | Global Definition of Done | T704 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v1`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v1/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v1/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim. Probes in `scripts/verify/v1/probes/<ID>.go` (`//go:build ignore`) are
   built by the script against the API the brief prescribes; a probe that no longer compiles
   means the API differs from the brief (or the row is broken: fix the row, note `row fixed`).
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v1/plan.md`
   (row T799, `scripts/verify/v1/T799.sh`); done when it passes.
