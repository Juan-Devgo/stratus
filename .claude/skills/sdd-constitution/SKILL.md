---
name: sdd-constitution
description: Create or amend docs/constitution.md, the project principles that every spec, plan, task and line of code must follow. Use when the user gives a prompt describing project principles or wants to change them.
---

# SDD Constitution

The user gives a prompt describing project principles. Turn it into `docs/constitution.md`. Every spec, plan, task and line of code must comply with it.

## 1. Guide (do not write yet)

If `docs/constitution.md` exists, read it and work in amend mode (step 3).

Find gaps in the prompt, then interview the user with AskUserQuestion: max 4 questions per round, 2-4 options each, a recommended option first, a concrete example in each option. Cover only what applies:

- Mission: who it serves, what problem it solves, what it must never become.
- Quality: testing, code style, review, definition of done.
- Security and privacy: data, secrets, auth, compliance.
- Performance, accessibility, compatibility limits.
- Architecture and dependency constraints (e.g. "no new runtime dependency without approval").
- Conflict rule: which principle wins when two collide.

Probe edge cases: "What if a deadline conflicts with this?", "Is there any exception?", "How would an agent verify this is met?". Replace vague words ("clean", "fast", "secure") with measurable rules; ask for the number if missing.

Stack and commands do not belong here (they go in AGENTS.md via sdd-agents).

Repeat rounds until nothing is ambiguous. Show a short summary and get confirmation before writing.

## 2. Write

Create `docs/constitution.md` in exactly this format:

```
# Constitution
Version: 1.0.0

## 1. Mission
<1-3 sentences>

## 2. Core principles
- P1. <rule>
- P2. <rule>

## 3. Amendments
| Date | Version | Change |
|---|---|---|
| YYYY-MM-DD | 1.0.0 | Initial version |
```

Rules:
- One imperative, verifiable sentence per principle, using MUST / MUST NOT.
- Add an `Exception:` clause only if the user explicitly agreed to it.
- Order = priority. Lower number wins conflicts.
- No rationale, examples or repetition. Shortest unambiguous wording.
- Write in the user's language.

## 3. Amend

- Bump version: MAJOR = principle removed or redefined, MINOR = principle added, PATCH = wording only.
- Add a row to Amendments with today's date. Never edit or delete past rows.
- Tell the user that specs referencing an older constitution version may need review.
