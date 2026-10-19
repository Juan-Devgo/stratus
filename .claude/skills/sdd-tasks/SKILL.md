---
name: sdd-tasks
description: Turn a plan into docs/specs/###-name/tasks.md, a phased checklist. Use when a plan is ready and the user wants the work broken into implementable steps.
---

# SDD Tasks

Write `docs/specs/###-name/tasks.md` from the spec and plan. If the user did not name the spec and several exist, ask which one.

## 1. Read

`AGENTS.md` (check whether test-first is on), `docs/constitution.md`, the spec, the plan. If the plan is missing or the spec has open ambiguities, stop and suggest sdd-plan or sdd-clarification.

## 2. Write

The file is only a title and a checklist divided into phases. Nothing else.

```
# Tasks: <spec name>

## Phase 1: <name>
- [ ] T001 <action> in `<path>` (REQ-1)
- [ ] T002 <action> in `<path>` (REQ-1, AC-1)

## Phase 2: <name>
- [ ] T003 ...
```

Rules:
- Order phases by dependency (e.g. setup, foundations, one phase per P1 story, then P2, P3, polish). Each phase ends in a working, verifiable state, so it can be implemented and stopped on its own.
- One task = one small, concrete change an agent can finish and verify in one go. Name the file path.
- Cite the REQ and AC it satisfies. Setup tasks may cite none.
- If AGENTS.md says test-first, put each test task before the code task it verifies.
- Every REQ and AC of the spec is covered by at least one task. Every task traces to the plan.
- No descriptions, notes or metadata. Write in the user's language.
- Task IDs are sequential across phases and never reused.
