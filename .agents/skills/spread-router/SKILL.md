---
name: spread-router
description: Route snapd Spread work to the right workflow for running tests, investigating CI failures, fixing test defects, or updating test specifications. Use when deciding which Spread skill to follow or coordinating multiple Spread stages.
metadata:
  project: snapd
  task-type: spread-workflow-routing
---

# Spread workflow router

Use this skill as the entry point for snapd Spread tasks. The focused
procedures are kept in `.agents/skills/spread/`; load the matching
`SKILL.md` before carrying out that workflow. When a focused skill refers to
another Spread skill, resolve it to the sibling skill in that directory.
Shared execution, debug-socket, and CI evidence procedures are documented in
[the common procedures reference](../spread/references/common-procedures.md).

| Need | Follow |
|---|---|
| Choose a suite, backend, system, or run/debug a Spread test | [`spread-runner`](../spread/spread-runner/SKILL.md) |
| Investigate CI job evidence and reproduce a reported failure | [`spread-reproducer`](../spread/spread-reproducer/SKILL.md) |
| Fix a confirmed defect in a Spread test or its helpers | [`spread-fixer`](../spread/spread-fixer/SKILL.md) |
| Handle an end-to-end failure investigation, reproduction, fix, and verification | [`spread-failure-workflow`](../spread/spread-failure-workflow/SKILL.md) |
| Make a planned test migration or update from a specification | [`spread-update-workflow`](../spread/spread-update-workflow/SKILL.md) |

Use the smallest workflow that meets the request. The runner owns execution
and test selection; the common procedures reference owns shared run-context,
debug-socket, and CI evidence mechanics. The reproducer owns CI investigation
and reproduction handoffs; the fixer makes focused test-code changes. The
failure workflow coordinates those stages when end-to-end resolution is
requested. The update workflow is for planned test changes and does not
require an existing failure.
