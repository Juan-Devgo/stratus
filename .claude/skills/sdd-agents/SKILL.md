---
name: sdd-agents
description: Set up or update AGENTS.md (single source of truth for agent instructions) and make CLAUDE.md point to it. Use when starting spec-driven development in a project or when the user wants to change the always-on agent instructions.
---

# SDD Agents

Create or update `AGENTS.md` with the instructions agents must follow on every call. Keep it small.

## 1. Gather

Inspect the repo first (package.json, pyproject.toml, Makefile, lockfiles, CI config, existing AGENTS.md / CLAUDE.md). Do not ask what you can detect.

Ask with AskUserQuestion, prefilled with what you detected:
- Stack: language, framework, key libraries, package manager.
- Commands: install, dev, build, test, lint, typecheck.
- "Do you want a test-first methodology (TDD)?" Yes / No.

Conventions: detect what the code already does (directory layout, file and symbol naming, formatter/linter configs, import style, commit style). For each convention not defined or not detectable, ask the user and recommend an option based on the stack and community standard. Ask about:
- Project structure: top-level directories and what goes in each (e.g. `src/`, `tests/`, `docs/`), and where tests live (next to code or in `tests/`).
- Naming: files and directories (snake_case, kebab-case, camelCase, PascalCase); variables, functions, classes, constants; test files.
- Formatting and imports: formatter, line length, import order, absolute vs relative imports.
- Git: branch names, commit message style (e.g. Conventional Commits).
- Others that apply to the stack: error handling, logging, comments and docstrings, language of code and comments.

Use AskUserQuestion: max 4 questions per round, 2-4 options each, recommended option first with a concrete example (e.g. `user_service.py` vs `user-service.py`). Do not ask what is already defined or enforced by a config file.

## 2. Write AGENTS.md

Use this structure. Omit empty sections.

```
# AGENTS.md

## Workflow
- All SDD files live in `docs/`.
- Hierarchy: `docs/constitution.md` > `docs/specs/###-name/spec.md` > `plan.md` > `tasks.md` > code. Each level must comply with the one above.
- Read the constitution before planning or coding. Do not restate it here.

## Stack
- <one line per item>

## Commands
- Test: `<cmd>`
- Lint: `<cmd>`

## Conventions
- Structure: `src/` code, `tests/` tests, `docs/` SDD files.
- Naming: files `kebab-case`, functions `camelCase`, classes `PascalCase`, constants `UPPER_SNAKE_CASE`.
- Commits: Conventional Commits.

## Testing
- Test-first: write a failing test, make it pass, refactor. No production code without a failing test.

## Maintaining this file
- If you find something important for every call, or a repeated workflow (e.g. a UI library like shadcn), ask the user whether to add it here. Never add without approval.
- Prefer a one-line pointer to a skill or doc over explaining. Example: "UI: use the `shadcn` skill."
- Keep this file small. When it grows, merge duplicates, delete stale lines, move details to docs or skills.
```

Rules:
- The `Testing` section only if the user said yes.
- Never copy constitution content; only point to the file.
- Commands, stack and conventions: facts only, no prose. Conventions already enforced by a formatter or linter config are not repeated; point to the config.
- The Conventions block above is an example; write only what the user decided.

## 3. CLAUDE.md

`CLAUDE.md` must contain only:

```
@AGENTS.md
```

If CLAUDE.md already has content, show it to the user, merge what is still valid into AGENTS.md, then replace it.

## 4. Updating

When AGENTS.md already exists, edit it in place, then re-optimize: remove redundancy, keep every line necessary on every call.
