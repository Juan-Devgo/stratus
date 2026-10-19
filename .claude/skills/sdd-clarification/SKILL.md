---
name: sdd-clarification
description: Find and resolve ambiguities, gaps and missing edge cases in an existing spec, recording answers in its Clarifications section. Use when the user wants to refine a spec before planning, or when a spec raises open questions.
---

# SDD Clarification

Refine an existing `docs/specs/###-name/spec.md`. If the user did not name the spec and several exist, ask which one.

## 1. Analyze (do not edit yet)

Read `docs/constitution.md` and the spec. Look for:
- Vague words ("fast", "easy", "secure") without a measurable value.
- Stories without requirement or acceptance criterion; criteria that are not verifiable.
- Missing edge cases: empty, invalid, duplicate, concurrent, large input, permissions, failures, offline.
- Contradictions between requirements, or with the constitution.
- Unclear actors, priorities or out-of-scope items.
- Unanswered items already in section 7 Clarifications.

## 2. Ask

Use AskUserQuestion: max 4 questions per round, 2-4 options each, a recommended option first, a concrete example in each option. Most impactful gaps first. Repeat rounds until no ambiguity is left.

Never decide for the user. Never add scope without explicit confirmation; new scope is a new story.

## 3. Update the spec

- Add each answer to section 7: `- YYYY-MM-DD. Q: <question> A: <answer>`.
- Reflect answers in stories, requirements, acceptance criteria or out of scope. Keep the format of sdd-spec.
- Set `Updated` to today. Do not change Status yet.
- If a plan or tasks file exists in the same folder, list what became outdated and ask whether to update it (sdd-plan / sdd-tasks).

## 4. Approve

Last step. Ask with AskUserQuestion: "Do you accept this spec as final so its status changes to Approved?" Options: Yes, approve (recommended if no ambiguity is left) / No, keep Draft.

- Yes: set Status to `Approved` and `Updated` to today.
- No: keep Status as is and ask what is still missing.
