These rules are read at run time from the trusted base branch. Edit this file
to change how findings are rated, when a human review is needed, how reviewers
are chosen and when the PR is approved; no recompile is needed.

### Rating (1–5)

Rate each verified finding as `rating = clamp(severity + occurrence, 1, 5)`.

**Severity** is the impact if the bug triggers:

| Severity | Score | Meaning |
|---|---|---|
| Cosmetic | 1 | Typos, wording of messages or comments, naming, formatting, dead code, with no effect on behavior |
| Minor | 2 | Wrong but harmless or trivially recoverable behavior, such as a misleading error message or a wrong result in a rarely used edge case with an easy workaround |
| Bug | 3 | Incorrect behavior in a supported flow, inconsistent state that the user can recover from, a regression of existing behavior |
| Severe | 4 | Data loss or corruption, a leaked or misused state lock, a deadlock or stuck change, a snap or system left unrefreshable or unremovable, wrong confinement rules |
| Critical | 5 | Crash or panic of `snapd`, `snap` or `snap-confine`, security or confinement bypass, privilege escalation, memory-safety bug, unrecoverable or unbootable system |

**Occurrence** is whether the bug can happen in the real world:

| Occurrence | Adjustment | Meaning |
|---|---|---|
| Natural | +0 | Reachable through ordinary use, supported configurations, real store responses or real device behavior |
| Uncommon | −1 | Needs a specific configuration, an unlucky race window, a restart at the wrong moment or a real but rare failure of the environment |
| Synthetic | −2 | Only reachable with mocked or forged inputs, test-only code paths, states the surrounding code makes impossible, or a malicious actor that already has more privilege than the bug would grant |

For every finding, state the concrete trigger scenario that justifies the
occurrence class. If you cannot describe a trigger, the finding is synthetic.

Other rules:

- Findings you could not confirm by reading the code are discarded, not rated.
- Findings in code the PR did not change (and does not make newly reachable) are
  discarded.
- In an incremental review, findings in code the new commits did not change (and
  do not make newly reachable) are also discarded; an earlier review covered it.
- Agreement between hunters is shown in the report but never changes the rating.
- Do not inflate ratings to make the report look useful. A PR with no real
  findings is a good outcome.

### Architecture changes

A PR that changes the architecture always needs a human review, whatever its
findings. The list below configures what counts as an architecture change;
remove all of its items to turn this rule off. Judge the whole PR from the full
PR diff, also in an incremental review. Bug fixes, refactoring and new features
that stay within the existing design are not architecture changes.

A PR changes the architecture when it:

- adds, removes or splits a state manager, changes how the managers are wired
  together in `overlord/overlord.go`, or adds a dependency or hook between
  manager packages;
- adds or removes task kinds, or changes the persisted state or the tasks of an
  existing change in a way that affects restarts, undo or upgrades from an
  older snapd;
- adds, removes or changes REST API endpoints (`daemon/`, `client/`);
- adds an interface (`interfaces/builtin/`) or changes the base declaration
  (`interfaces/policy/basedeclaration.go`);
- changes an assertion type (`asserts/`), the `snap.yaml` format (`snap/`) or
  the seed format (`seed/`);
- changes the boot, install, recovery or remodel flows (`boot/`,
  `cmd/snap-bootstrap/`, the install and remodel code in
  `overlord/devicestate/`);
- adds a top-level package or binary, or a third-party dependency (`go.mod`).

### Human review

A human review is required when at least one of the following holds:

- the PR changes the architecture (see above);
- an open finding is rated 4 or 5;
- two or more open findings are rated 3 or higher;
- an open finding rated 3 or higher is in security-sensitive code (`cmd/snap-confine`,
  `sandbox/`, `interfaces/` confinement rules, `secboot/`, `boot/`, `asserts/`,
  `overlord/` state and task handlers).

Open findings are the verified findings of this review and, in an incremental
review, the findings carried forward from earlier reviews that are still open.
When no human review is required, do not request anyone and say why in the
report.

Choose reviewers **only from the repository's CODEOWNERS file**:

1. Read `.github/CODEOWNERS` from the workspace (it is restored from the base
   branch and therefore trusted). Do not use a `CODEOWNERS` from any other
   workspace location, since that would come from the PR. If you need to check
   `CODEOWNERS` or `docs/CODEOWNERS`, fetch it from the base branch with the
   GitHub tools. If none exists, request nobody and state that no CODEOWNERS
   file was found.
2. Match with GitHub semantics: patterns follow `.gitignore` rules, the **last**
   matching pattern wins, and `#` starts a comment. Ignore e-mail owners.
3. Prefer the owners of the files that make up the architecture change, then
   the owners of the files that contain the highest-rated open findings. If
   those have no owner, fall back to the owners of the most heavily changed
   files.
4. Request at most two reviewers. Prefer individual users over teams.
5. Never request the PR author, anyone already requested, and anyone who has
   already reviewed the PR.
6. Never request anyone who is not listed in CODEOWNERS.

### Recommendation and approval

Give exactly one recommendation, using the first row that applies:

| Recommendation | When | Action |
|---|---|---|
| Human review required | The human review rules above require a human review, even if no reviewer can be requested | Request reviewers as above; do not approve |
| Address findings | An open finding is rated 3 or higher | Do not approve; the author should fix the findings and re-request a review |
| Incomplete review | Fewer than two hunters reported, or this is an incremental review and the previous review report is unavailable | Do not approve; re-request a review |
| Approve | Every open finding is rated 1 or 2 (cosmetic or minor), or there are none | Submit an approving review |

An approval only records that this review found nothing above minor. It does
not replace the reviews that branch protection or CODEOWNERS require.

### Report format

Post a single comment:

1. The review scope: "Full review" or "Incremental review of N new commits",
   with the reason from the scope file when there was an earlier review, and
   links to the compare view and the previous review when available.
2. The recommendation in bold (**Approve**, **Address findings**, **Human
   review required** or **Incomplete review**) with a one-sentence reason.
3. A one-line summary: number of open findings per rating, in an incremental
   review how many earlier findings were resolved, and which hunters
   contributed (`claude`, `kimi`, `mai`; note any that failed).
4. A table of open findings sorted by rating, highest first, with columns:
   rating, location (`path:line`), title, severity, occurrence, found by. Mark
   findings carried forward from earlier reviews with "earlier review" in the
   found-by column.
5. For each open finding rated 3 or higher: what is wrong, the concrete trigger
   scenario, the impact and a suggested fix.
6. Open findings rated 1 or 2 inside a collapsed `<details>` block, one line
   each.
7. In an incremental review, the earlier findings that are now resolved, one
   line each.
8. A "Human review" section: the decision, the reason (naming the architecture
   change, if any), and the reviewers requested (or why none).
9. A line explaining how to re-request: apply the `ai-review` label again. After
   new commits only those are reviewed; without new commits the whole PR is
   reviewed again.

If there are no open findings, say so briefly and still include the
recommendation and the human review decision.
