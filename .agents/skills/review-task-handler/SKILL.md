---
name: review-task-handler
description: Evaluate and review a snapd state task handler against overlord/handlers-quality.md. Use when given a state-manager package and task kind to assess do, undo, cleanup, locking, restart safety, coordination, and tests, then produce findings and a consistent weighted grade.
metadata:
  project: snapd
  task-type: review
---

# Review a task handler

Review one snapd task kind using the current `overlord/handlers-quality.md` as
the canonical checklist. This is an evaluation workflow: do not modify code
unless the user separately requests fixes.

## Inputs

Require both:

1. A state-manager package, preferably a workspace-relative path such as
   `overlord/snapstate`. An unambiguous package name such as `snapstate` is also
   acceptable.
2. The exact task kind registered with `state.TaskRunner`, such as
   `download-snap`.

Resolve a package name only under `overlord/`. If either input is missing or
ambiguous, ask for clarification. Do not infer a task kind from a function name.
If the exact task kind has no registration, constructor, or reference in the
resolved package, report that no such task kind was found and stop without a
review or grade.

## Review boundary

Include only code that controls the requested task kind:

- do, undo, and cleanup handler registration and implementation
- task construction, summaries, state keys, dependencies, and lanes
- operation-construction conflict checks and task-runner blocking
- direct helper calls that control locking, persistent state, external effects,
  cancellation, retry, restart, or recovery
- focused unit tests and directly applicable integration tests

Exclude unrelated package quality and adjacent task kinds unless they directly
control ordering or recovery for the requested task.

Review the current working tree. Check the relevant files with `git diff` and
mention material uncommitted changes; never discard them.

## Evidence gathering

Keep discovery bounded and follow the controlling path rather than exhaustively
mapping the package.

1. Read `overlord/handlers-quality.md` on every invocation. Do not rely on a
   remembered or copied version.
2. Search the package for the exact quoted task kind. Start with
   `AddHandler`, `AddCleanup`, and task creation. If registration is indirect,
   follow the local registration helper.
3. From registration, identify the exact do, undo, and cleanup symbols. Do not
   assume Go names derived from the task kind.
4. Read the handlers and direct helpers that own external effects or unlock the
   state. Record:
   - task inputs and persisted working data
   - validation before external effects
   - every lock release and state read or write across it
   - external mutations in execution order
   - every error exit after an external mutation
   - explicit retry, wait, cancellation, status, restart, and cleanup behavior
5. Locate every constructor for the task kind. Trace prerequisite ordering,
   task edges, lanes, conflict checks, summaries, and setup keys.
6. Inspect manager-level `AddBlocked` callbacks and package conflict logic only
   as far as they apply to the task's resources.
7. Find tests by task-kind strings, handler names, setup keys, external boundary
   calls, and expected statuses. Do not rely only on test filenames or names.
8. When intent remains unclear, use nearby history (`git blame` or `git log -S`)
   narrowly. History is context, not proof that current behavior is correct.

Prefer parallel read-only searches when independent. Once the controlling path
is known, stop broad discovery and evaluate it.

## Evaluation method

Load [the scoring rubric](./references/rubric.md). Evaluate every bullet in each
applicable section of `overlord/handlers-quality.md` as `pass`, `partial`,
`fail`, or `N/A`.

After assigning ratings, use [the scoring helper](./scripts/score.py). It reads
the live checklist, rejects category counts that do not reconcile, normalizes
weights for wholly `N/A` categories, and calculates the grade. Slow operation
locking is the exception: while applicable, its weight remains fixed at 4% and
other `N/A` weight is redistributed around it. The helper does not decide
ratings or severity.

The Slow Operation Locking category owns responsiveness caused by holding the
`State` lock around slow or blocking work, including indirect helper, fallback,
retry, and error paths. Do not also deduct the same lock-duration issue under
State and locking or Do handler. Continue to rate distinct correctness
consequences, such as stale-state overwrite or unsafe overlap, under their
owning criteria.

### Rebut before reporting

For each suspected issue, construct a concrete scenario such as:

- crash after an external mutation but before task state or status is persisted
- handler retry after each interruption point
- failure after the first of several external effects
- undo after complete do, partial do, and partial undo
- concurrent compatible changes while the state lock is released
- absent or already-completed target
- cancellation during supported network or polling work

Trace the scenario through actual code, then argue against it with the strongest
nearby counter-evidence before reporting it:

- inspect the directly called store/backend/helper implementation when it owns
  resumability, idempotency, or artifact lifecycle
- treat repeated external work as safe when it is demonstrably idempotent or
  convergent; inefficiency alone is not an incompatible replay
- treat `tomb.Context(nil)` as a tomb-cancelled context unless code proves that
  cancellation is discarded downstream
- account for operation-construction conflicts, `AddBlocked` serialization, and
  package-level garbage collection before claiming that concurrent state can
  overwrite undo or working data, or that an artifact leaks
- distinguish intentionally retained resumable/cache artifacts from leaked
  task-owned artifacts

A missing explicit `SetStatus` is not automatically a defect: explain the
incompatible replay it permits. Likewise, an unlock is not automatically unsafe:
identify mutable state that can change and the coordination that does or does
not protect it.

A responsiveness-only Slow Operation Locking finding is category-limited and
does not supply `confirmed_severity`. A distinct correctness consequence may
still supply severity under its owning criterion.

Remove the finding if the rebuttal cannot be answered by code. Downgrade it to
`Unresolved` when one controlling boundary cannot be inspected.

### Finding rules

- Findings lead; do not begin the report with a summary.
- A finding must identify the violated checklist criterion, severity, concrete
  failure mode, and workspace-relative file evidence.
- Lead with root causes. Do not count one defect again under every checklist
  consequence, and do not repeat it in the grade rationale.
- Distinguish verified defects from missing tests and unresolved concerns.
- Treat cleanup as best-effort only when failure still leaves the system
  semantically correct and another intentional lifecycle handles leftovers.
- Do not penalize a missing undo when the task is deliberately final or
  irreversible and the omission is justified by construction. A coordinator that
  only creates or injects child tasks is such a case; see the rubric for how to
  mark it.
- Give credit for relevant coordination and tests, but never use positive
  coverage to hide a correctness finding.
- Put checklist-derived findings before observations outside the checklist, and
  keep the two separate.
- Do not infer a defect from an `XXX`, `TODO`, unusual pattern, or absent test;
  trace an actual failure scenario or report only a test/clarity gap.
- Do not recommend broad refactoring unless it is necessary to address a
  finding.
- Do not claim comprehensive coverage when focused tests or relevant paths were
  unavailable.

## Verification

Run focused tests by default after completing the static review.

1. Identify the narrowest gocheck test names or suite covering the handler.
2. Run:

   ```bash
   LANG=C.UTF-8 go test -v ./<package> -check.v -check.f '<pattern>'
   ```

3. If VS Code test discovery cannot find gocheck tests, use the command above.
4. Report the exact number of passing, failing, or skipped focused tests. A
   passing suite does not disprove untested interruption scenarios.

Do not run all package tests, `./run-checks`, or spread tests unless focused
selection is impossible, the task's risk requires it, or the user asks.

## Output

Load and follow [the report template](./references/report-template.md). Compute
the score with `scripts/score.py` and state any severity cap that changed it.
Keep the report concise enough to scan, while preserving concrete evidence for
every finding.
