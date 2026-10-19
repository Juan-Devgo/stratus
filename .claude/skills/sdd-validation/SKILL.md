---
name: sdd-validation
description: Validate that constitution, spec, plan and tasks are consistent, then suggest fixes and upgrades and add the approved ones to docs/. Use before implementing, after a phase, or when the user asks to review SDD documents.
---

# SDD Validation

Validate `docs/constitution.md` > `docs/specs/###-name/spec.md` > `plan.md` > `tasks.md` > code. If the user did not name the spec and several exist, ask which one (or validate all if asked). Read-only until step 3.

## 1. Check

Constitution and spec
- Spec `Constitution version` equals the current version; if older, check the amendments in between.
- Each principle in "Constitution relationship" exists, and the spec does not contradict any principle.

Spec
- Format of sdd-spec is followed; IDs are unique.
- Every story has at least one REQ and one AC; every AC is verifiable.
- No vague words without measurable values.

Plan
- Complies with the constitution; matches AGENTS.md stack.
- Every REQ and AC appears in Traceability; nothing in the plan lacks a REQ.

Tasks
- Every REQ and AC is covered by a task; every task traces to the plan.
- Test tasks come before code tasks if AGENTS.md says test-first.

Code (last check: compare the code against the tasks so everything is consistent)
- Every checked task `[x]` is implemented in code as written: the file in the task exists and does what the task says.
- Every unchecked task `[ ]` is not implemented yet; code that does it means the task was not checked.
- No code, file or behavior exists without a task, and every code change traces to a REQ in the spec.
- Tests exist for the checked test tasks, cite their US, REQ and AC, and pass (run the test command from AGENTS.md).

Missing files are reported, not assumed.

## 2. Report

Short table: `Severity (Blocker | Warning) | Where | Problem | Suggested fix`. If all is consistent, say so in one line.

Then suggest upgrades: simplifications, missing edge cases, missing tests, constitution amendments, items for AGENTS.md. Each one line.

## 3. Apply

Ask with AskUserQuestion (multi-select) which fixes and upgrades to add to `docs/`. Apply only the selected ones, following the format of the matching skill (sdd-constitution, sdd-spec, sdd-plan, sdd-tasks). Update `Updated` dates and, for the constitution, version and Amendments. Never change anything not approved. Never modify code here: a code and tasks mismatch is fixed by correcting tasks.md (with approval) or by running sdd-implement.
