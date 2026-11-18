# Task handler review report

Use the following order and headings. Omit empty optional sections, but never
omit checklist findings, coverage, verification, or the final grade.

## Checklist Findings

List findings in descending severity and then execution order. Use this form:

```markdown
1. **High - Short finding title.** Concrete failure scenario and resulting
   incorrect state or effect. Violates: `Do Handler - Is idempotent` and
   `State And Locking - Couples persistent working-state mutations...`.
   [handlers.go](overlord/example/handlers.go#L123-L145)
```

Requirements:

- Use workspace-relative clickable links with the narrowest useful lines.
- Name the checklist section and criterion in each finding.
- Say `No checklist findings` when none are confirmed.
- Label uncertainty explicitly as `Unresolved` and list it separately from
  confirmed findings.

## Checklist Coverage

Include the coverage table emitted by `scripts/score.py`, with one row per
weighted category:

```markdown
| Category | Weight | Pass | Partial | Fail | N/A | Raw contribution |
|---|---:|---:|---:|---:|---:|---:|
| General | 10% | 5 | 1 | 1 | 0 | 0.8/1.0 |
```

After the table, list every `partial`, `fail`, and non-obvious `N/A` criterion by
short checklist wording. This criterion ledger must reproduce the counts and
make the arithmetic reviewable. Then briefly note important positive evidence,
especially working conflicts, blocking, recovery, or meaningful undo tests.
When an entire category is `N/A`, state that all of its top-level criteria are
`N/A` and give one category-level justification instead of repeating each one.
Identify responsiveness-only Slow Operation Locking findings as category-limited;
do not attribute a severity cap to them unless a separately rated correctness
consequence determines severity.

## Other Observations

Put non-checklist observations here. Keep them distinct from scored findings.
Examples include stale comments, confusing names, duplicated work, or nearby
opportunities that do not violate the checklist.

## Verification

State exactly what was run and observed:

```markdown
`LANG=C.UTF-8 go test -v ./overlord/example -check.v -check.f 'pattern'`
passed 6 focused tests.
```

If tests could not run, give the blocker. Also identify significant scenarios
that remain untested; do not describe static inspection as test verification.

## Grade

Use this form:

```markdown
**Grade: C+ (7.9/10)**

Raw score: 8.6. Capped at 7.9 because the highest confirmed finding is Medium.
One or two sentences explain the dominant strengths and deductions.
```

If the helper reports that no cap is binding, omit the cap sentence. Copy the
displayed score and letter from the helper.