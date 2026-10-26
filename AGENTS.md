# AGENTS.md

## Workflow
- All SDD files live in `docs/`.
- Hierarchy: `docs/constitution.md` > `docs/specs/###-name/spec.md` > `plan.md` > `tasks.md` > code. Each level must comply with the one above.
- Read the constitution before planning or coding. It is not repeated here.

## Stack
- Monorepo: `stratus-srv/` (Go), `stratus-ui/` (React), `scripts/` (bash), `docs/`.
- Backend: Go 1.24+, `net/http` with `ServeMux` (standard library).
- Persistence: SQLite for the operation history. Dependency approved by the user as an exception to constitution P9.
- SSH: system `ssh` client invoked with `os/exec`.
- Scripts: Bash 5+ and Bats; macOS system Bash 3.2 is not supported.
- Frontend: React + React Router in declarative mode (`<BrowserRouter>`/`<Routes>`), Vite, TypeScript, Tailwind, shadcn/ui (see `shadcn` skill), pnpm.
- Tests: Go `testing` (table-driven), `bats` for bash, Vitest + Testing Library.
- Deploy: Go backend native on the host (needs VBoxManage); UI in Docker + docker-compose. No CI.

## Commands
- Backend, run: `cd stratus-srv && go run ./cmd/server`
- Backend, tests: `cd stratus-srv && go test ./...`
- Backend, lint: `cd stratus-srv && gofmt -l . && golangci-lint run`
- Frontend, install: `cd stratus-ui && pnpm install`
- Frontend, dev: `cd stratus-ui && pnpm dev`
- Frontend, build: `cd stratus-ui && pnpm build`
- Frontend, tests: `cd stratus-ui && pnpm test`
- Frontend, types: `cd stratus-ui && pnpm typecheck`
- Scripts, tests: `bats -r scripts/`
- Deploy: backend native on the host (`cd stratus-srv && go run ./cmd/server`, needs VBoxManage), UI with `docker compose up --build`

## Conventions
- Language: Markdown files (`AGENTS.md`, `docs/`, specs, plans, tasks), code and identifiers in English. Code comments, commit messages, bash script output and UI text in Spanish.
- Structure:
  - `stratus-srv/cmd/server/main.go` and `stratus-srv/internal/{handler,service,repository}/`; each layer is split by module: `prepare`, `users`, `services`, `haproxy`.
  - `stratus-ui/src/features/<module>/`, `stratus-ui/src/routes/`, `stratus-ui/src/components/`.
  - `scripts/<module>/` with one script per operation.
  - Tests next to the code: `_test.go`, `*.test.tsx`, `.bats` next to the script.
- Go: lowercase single-word packages; `snake_case.go` files; `ErrXxx` sentinel errors wrapped with `fmt.Errorf("...: %w", err)`; format with `gofmt`, lint with `golangci-lint`.
- React: components `PascalCase.tsx`; hooks `useXxx.ts`; other files `kebab-case`; routes `kebab-case`.
- API: REST at `/api/<module>/<kebab-case-path>`; JSON fields `snake_case`.
- Environment variables: `APP_UPPER_SNAKE_CASE`.
- Bash scripts: `kebab-case.sh`; each script documents its exit-code table in the header.
- Commits: Conventional Commits, message in Spanish (`feat(haproxy): agregar backend`).
- Branches: `feat/<module>-<name>`, `fix/<module>-<name>`.

## Testing
- Test-first: write a failing test, make it pass, refactor. No production code without a prior failing test.

## Maintaining this file
- If you find something important for every call, or a repeated flow (for example a UI library), ask the user whether to add it here. Never add without approval.
- Prefer a one-line pointer to a skill or doc over explaining.
- Keep the file small: merge duplicates, delete stale lines, move details to docs or skills.
