---
name: sdd-implement
description: Implement one phase of docs/specs/###-name/tasks.md, checking off tasks as they finish, and stop when the phase is done. Use when the user asks to implement a spec, a phase or the next phase.
---

# SDD Implement

Implement tasks phase by phase.

## 1. Select

- Spec: the one named by the user; if unclear and several exist, ask.
- Phase: the one named by the user ("phase 2"); if none is named, the first phase with unchecked tasks.
- If tasks.md is missing, stop and suggest sdd-tasks.

## 2. Read

`AGENTS.md`, `docs/constitution.md`, the spec, the plan, the tasks. Code must comply with all of them. Use the commands in AGENTS.md.

## 3. Loop

Repeat until every task in the phase is checked:
1. Take the next unchecked task in the phase, in order.
2. Implement it as the plan describes. Test-first if AGENTS.md says so.
3. Run the relevant checks (test, lint, typecheck from AGENTS.md). Fix failures before continuing.
4. Mark it `- [x]` in tasks.md immediately.

## 4. Stop

When the phase is finished, run the full test command, then stop. Do not start the next phase. Report in 2-3 lines: tasks done, check results, the next phase name.

## Stop and ask when
- The spec, plan or tasks are ambiguous, contradictory or missing something needed.
- A task would violate the constitution.
- The plan needs a change. Propose it, and update the plan only after the user approves.
- A check keeps failing after a real fix attempt.

Never invent requirements, add scope, skip tasks or check a task that is not done and verified.
