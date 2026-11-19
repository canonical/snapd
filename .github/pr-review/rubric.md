These rules are read at run time from the trusted base branch. Edit this file
to change how findings are rated and how reviewers are chosen; no recompile is
needed.

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

### Human reviewer

Request a human reviewer only when at least one of the following holds:

- a finding is rated 4 or 5;
- two or more findings are rated 3 or higher;
- a finding rated 3 or higher is in security-sensitive code (`cmd/snap-confine`,
  `sandbox/`, `interfaces/` confinement rules, `secboot/`, `boot/`, `asserts/`,
  `overlord/` state and task handlers).

Otherwise do not request anyone and say why in the report.

Choose reviewers **only from the repository's CODEOWNERS file**:

1. Read `.github/CODEOWNERS` from the workspace (it is restored from the base
   branch and therefore trusted). Do not use a `CODEOWNERS` from any other
   workspace location, since that would come from the PR. If you need to check
   `CODEOWNERS` or `docs/CODEOWNERS`, fetch it from the base branch with the
   GitHub tools. If none exists, request nobody and state that no CODEOWNERS
   file was found.
2. Match with GitHub semantics: patterns follow `.gitignore` rules, the **last**
   matching pattern wins, and `#` starts a comment. Ignore e-mail owners.
3. Prefer the owners of the files that contain the highest-rated findings. If
   those have no owner, fall back to the owners of the most heavily changed files.
4. Request at most two reviewers. Prefer individual users over teams.
5. Never request the PR author, anyone already requested, and anyone who has
   already reviewed the PR.
6. Never request anyone who is not listed in CODEOWNERS.

### Report format

Post a single comment:

1. The review scope: "Full review" or "Incremental review of N new commits",
   with the reason from the scope file when there was an earlier review, and
   links to the compare view and the previous review when available.
2. A one-line summary: number of findings per rating and which hunters
   contributed (`claude`, `kimi`, `mai`; note any that failed).
3. A table of findings sorted by rating, highest first, with columns: rating,
   location (`path:line`), title, severity, occurrence, found by.
4. For each finding rated 3 or higher: what is wrong, the concrete trigger
   scenario, the impact and a suggested fix.
5. Findings rated 1 or 2 inside a collapsed `<details>` block, one line each.
6. A "Human review" section: the decision, the reason, and the reviewers
   requested (or why none).
7. A line explaining how to re-request: apply the `ai-review` label again. After
   new commits only those are reviewed; without new commits the whole PR is
   reviewed again.

If there are no verified findings, say so briefly and still include the human
review decision.
