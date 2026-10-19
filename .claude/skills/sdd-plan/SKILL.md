---
name: sdd-plan
description: Create the technical plan (code, architecture, design patterns) for a spec at docs/specs/###-name/plan.md. Use when a spec is approved and the user wants to decide how to build it.
---

# SDD Plan

Write `docs/specs/###-name/plan.md` for an existing spec. If the user did not name the spec and several exist, ask which one.

## 1. Read

- `AGENTS.md` (stack, commands, testing), `docs/constitution.md`, the spec.
- If the spec has open ambiguities, stop and suggest sdd-clarification.
- The existing code: reuse current structure, patterns and libraries before proposing new ones.

## 2. Decide (do not write yet)

Check every decision against the constitution. Use AskUserQuestion for each real choice: max 4 questions per round, 2-4 options with trade-offs, a recommended option first. Ask about:
- Architecture: components, boundaries, data flow, where state lives.
- Design patterns: only where they solve a real problem in this spec.
- Data models, interfaces, contracts, error handling.
- New dependencies (the constitution may restrict them).
- Edge cases from the spec: how each is handled.
- If AGENTS.md says test-first: test levels (unit, integration, e2e), test locations and naming, mocks vs real dependencies.

Prefer the simplest design that satisfies the spec. No speculative abstractions. Show a short summary and get confirmation before writing.

## 3. Write

Use exactly this format. Be brief. No ambiguous words.

```
# Plan: <spec name>

## 1. Metadata
- Spec: <path>
- Updated: YYYY-MM-DD
- Constitution version: <version>

## 2. Constitution check
- P#: <how the plan complies> (one line each, only those that apply)

## 3. Architecture
<components, responsibilities, data flow; max 1 short diagram or list>

## 4. Design patterns
- <pattern> in <place>: <problem it solves>

## 5. Code structure
- create: <path> - <purpose>
- modify: <path> - <change>

## 6. Interfaces and data
<signatures, schemas, contracts; no function bodies>

## 7. Test-first (only if AGENTS.md says test-first)
- Framework and commands: <from AGENTS.md>
- Location and naming: <e.g. `tests/<module>/test_<behavior>.py`; test name starts with the AC ID>
| Test | Level | File | Covers (US / REQ / AC) | Given - when - then |
|---|---|---|---|---|
| <test name> | unit | <path> | US-1 / REQ-1 / AC-1 | <taken from the AC> |

## 8. Traceability
| Requirement | Component / file | Verified by (AC) |
|---|---|---|
| REQ-1 | <component> | AC-1 |

## 9. Decisions and risks
- <decision>: <alternatives rejected, why>
- Risk: <risk> - <mitigation>
```

Rules:
- Every REQ and AC from the spec appears in Traceability. Nothing in the plan without a REQ.
- Test-first: tests come only from the spec. Every AC has at least one test, derived from its given - when - then, so every REQ and US is covered through its ACs. Every test cites its US, REQ and AC. No test without them. Edge cases are tests because the spec turned them into ACs.
- Test-first: the plan defines which tests exist and what they verify; writing them before the code is ordered by sdd-tasks and done by sdd-implement.
- Describe the how, never tasks or order of work (that is sdd-tasks).
- Write in the user's language.
