# Task handler review rubric

Use this rubric with the current `overlord/handlers-quality.md`. The checklist
is canonical; this file defines only how to classify and score its bullets.

## Criterion ratings

Rate every applicable checklist bullet independently:

| Rating | Points | Meaning |
|---|---:|---|
| `pass` | 1 | The implementation and applicable tests provide positive evidence. |
| `partial` | 0.5 | The intent is present but incomplete, weakly tested, or safe only under narrower conditions than required. |
| `fail` | 0 | A concrete counterexample violates the criterion, or required behavior is absent. |
| `N/A` | excluded | The criterion does not apply to this task. Explain why when it is not obvious. |

Do not award `pass` merely because no defect was found. Use `partial` when the
code looks plausible but an important recovery property lacks enough evidence.
Count direct top-level bullets in the current checklist section. In every
category row, `pass + partial + fail + N/A` must equal that section's current
top-level bullet count. Recount before calculating if it does not.

Use [the scoring helper](../scripts/score.py) to validate counts and calculate
the score. From the repository root, send a JSON object on standard input as
shown below. The counts are illustrative; always recount them against the live
checklist.

```bash
python3 .agents/skills/review-task-handler/scripts/score.py <<'JSON'
{
  "ratings": {
    "General": {"pass": 5, "partial": 1, "fail": 1, "na": 0},
    "Coordination": {"pass": 3, "partial": 0, "fail": 0, "na": 1},
    "State and locking": {"pass": 6, "partial": 1, "fail": 1, "na": 0},
    "Slow operation locking": {"pass": 1, "partial": 0, "fail": 1, "na": 0},
    "Do handler": {"pass": 6, "partial": 0, "fail": 1, "na": 0},
    "Undo handler": {"pass": 0, "partial": 0, "fail": 0, "na": 8},
    "Tests": {"pass": 5, "partial": 2, "fail": 0, "na": 3}
  },
  "confirmed_severity": "medium"
}
JSON
```

For no confirmed behavioral defect, use `null` for `confirmed_severity` even if
there are coverage-only findings. Valid severities are `low`, `medium`, `high`,
and `critical`.

Copy the helper's table and score summary into the report. Do not manually
override valid helper output.

## Scoring

The helper owns all scoring arithmetic: category weights, redistribution of the
weight of wholly `N/A` categories, severity caps, rounding, and the letter
grade. It prints the normalized weights, per-category contributions, raw score,
binding cap, final score, and grade. Report those values verbatim; do not
recompute or adjust them.

The base category weights are:

| Category | Weight |
|---|---:|
| General | 9% |
| Coordination | 14% |
| State and locking | 19.5% |
| Slow operation locking | 4% |
| Do handler | 19.5% |
| Undo handler | 15% |
| Tests | 19% |

`Slow operation locking` is fixed at 4% whenever at least one of its criteria
applies. Weight from other wholly `N/A` categories is redistributed only among
the other applicable categories, so it cannot increase this category above 4%.
When the slow-operation category is wholly `N/A`, its own weight is
redistributed normally.

Decide only what the helper cannot: each criterion's rating, whether a category
is genuinely inapplicable, and the highest confirmed severity.

If the task legitimately has no undo, mark the Undo Handler bullets `N/A`. Apply
the same rule if another entire category is genuinely inapplicable.

For a coordinator with no undo, also mark undo-specific test criteria `N/A`
when they cannot apply to the coordinator itself. Evaluate child-task rollback
under coordination and observable workflow tests instead; do not award a pass
for a test that cannot exist.

## Severity

Assign severity from impact, not from the checklist section:

| Severity | Definition |
|---|---|
| Critical | Plausible execution causes systemic compromise, unrecoverable shared-state corruption, or loss of bootability without a practical recovery path. |
| High | Plausible normal failure, crash, retry, or undo leaves incorrect shared state or external effects requiring manual repair, or creates a security correctness failure. |
| Medium | Behavior is incorrect or recovery is incomplete in a bounded or uncommon path; impact is limited, automatically recoverable, or compatibility-specific. |
| Low | Maintainability, diagnostics, minor resource residue, or test weakness with no demonstrated semantic failure. |

Coverage-only findings describe the risk left unverified, but are not confirmed
behavioral defects. Deduct them through the Tests category; they do not impose a
severity cap unless a failing test or code trace confirms the underlying defect.
A concern explicitly labeled unresolved does not impose a cap either.

A slow-operation locking finding that only demonstrates responsiveness impact
is category-limited: report and rate it, but do not use it to choose
`confirmed_severity`. If the same code also causes a distinct correctness
failure such as stale-state overwrite, incompatible replay, corruption, or
unsafe overlap, rate that consequence under its owning State and locking, Do
handler, or Coordination criterion and let that consequence determine severity.

Report the highest confirmed severity to the helper, which applies the matching
cap. State both the raw score and the cap only when the helper reports the cap
as binding. If the computed grade feels wrong, revisit criterion ratings,
severity, or applicability and document the correction; do not adjust the letter
by intuition.

## Consistency rules

- Score one root defect against every independently affected criterion, but
  report it as one finding. Multiple failed criteria may legitimately lower the
  numeric score. A lock-duration responsiveness issue belongs only to Slow
  operation locking; do not duplicate it under State and locking or Do handler.
- Keep correctness findings separate from test findings even when the missing
  test would have exposed the defect.
- Base `N/A` on task semantics, not missing implementation.
- A passing test supports only the scenario it executes.
- Absence of explicit `DoneStatus` or `UndoneStatus` is a failure only when
  replay after the identified interruption is incompatible. Safe convergent
  replay satisfies the recovery intent.
- Do not fail ownership/concurrency criteria without accounting for applicable
  conflict checks and task-runner blocking.
- Existing behavior and historical intent do not override the checklist.
- Record enough category detail that another reviewer can reproduce the score.
