---
name: sdd-spec
description: Create a feature specification at docs/specs/###-name/spec.md from a detailed prompt, checking it against the constitution and clarifying ambiguity. Use when the user describes a new feature or change to specify.
---

# SDD Spec

The user gives a prompt describing a new specification. Turn it into `docs/specs/###-<spec_name>/spec.md`.

## 1. Check the constitution

- Read `docs/constitution.md`. If missing, tell the user to run sdd-constitution first and stop.
- Compare the prompt against every principle. List conflicts, if any.
- For each conflict, ask the user with AskUserQuestion: change the spec, or amend the constitution (use sdd-constitution). Do not continue with an unresolved conflict.
- List `docs/specs/` to find overlap with existing specs and the next number `###`.

## 2. Clarify (do not write yet)

Interview with AskUserQuestion: max 4 questions per round, 2-4 options each, a recommended option first, a concrete example in each option. Look for:
- Unclear actors, goals and priorities.
- Missing edge cases: empty, invalid, duplicate, concurrent, large input, permissions, failures, offline.
- Vague words ("fast", "easy", "secure"): ask for measurable values.
- Scope: what is explicitly NOT included.

Repeat until nothing is ambiguous. Show a short summary and get confirmation before writing.

## 3. Write

Create the file using exactly this format. Be brief. No ambiguous words.

```
# <Spec name>

## 1. Metadata
- Status: Draft | Approved | Implemented
- Updated: YYYY-MM-DD
- Constitution version: <version from docs/constitution.md>
- Required by: <spec IDs that depend on this one, or "none">

## 2. Constitution relationship
- Applies: <P# list, one short line each on how the spec complies>
- Conflicts: none | <conflict and agreed resolution>

## 3. User stories
| ID | Priority | Actor | Story |
|---|---|---|---|
| US-1 | P1 | <actor> | As a <actor>, I need <capability> for <benefit> |

## 4. Requirements
- REQ-1 (US-1): <one testable sentence>

## 5. Acceptance criteria
- AC-1 (US-1): Given <state>, when <action>, then <observable result>

## 6. Out of scope
- <item>

## 7. Clarifications
- YYYY-MM-DD. Q: <question> A: <answer>
```

Rules:
- Priority: P1 = must have, P2 = should have, P3 = nice to have.
- Every story has at least one requirement and one acceptance criterion; every criterion is verifiable.
- Edge cases found in step 2 become acceptance criteria.
- Describe what and why, never how (no implementation details; those go in the plan).
- Write in the user's language.
