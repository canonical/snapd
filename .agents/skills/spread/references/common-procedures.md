# Shared Spread procedures

Use the relevant procedure from this file in addition to the task-specific
workflow. A workflow may add constraints for its task, but should not duplicate
these shared mechanics.

## Reproducible run context

Before a baseline or reproduction run, record the current commit and dirty
state, selector (backend, system, test, and variant), scenario and relevant
environment, Spread flags and test arguments, snapd artifact source and
revision, image or snap revisions, and whether the machine is fresh or reused.
Keep the exact executable command with that record.

For verification, preserve the relevant baseline conditions. If a condition
must change, record the difference and do not describe the result as an exact
comparison. Do not claim a failure is fixed or a change is verified unless the
requested behavior was reached and the result supports that conclusion.

## Debug-socket inspection

Use `-debug-socket` for headless remote inspection when Spread should pause on a
task failure. Copy the exact socket path from Spread's ready message; do not
reconstruct it. The path has the form
`.spread-<jobid>-<machine>-<socket-id>.socket`.

While the Spread process is still paused, run focused commands from the project
directory:

```bash
spread -exec SOCKET -- 'COMMAND'
```

Each `spread -exec` runs a separate remote command and does not share process
state with the task. After collecting the needed evidence, resume with:

```bash
spread -continue SOCKET
```

This lets Spread continue with restore and cleanup. Do not resume before
capturing the evidence needed for diagnosis. An abruptly terminated run may
leave a socket file behind; its presence alone does not show that a session is
still live. Use `-debug` when a human-operated terminal is needed after failure
or `-shell` when an interactive shell is needed before task execution.

## CI evidence and session directory

Read existing CI evidence only from user-provided logs or by running the
bundled `tests/utils/spread-log/fetch_spread_ci_logs.py` from the snapd
repository. Do not use `gh`, `curl`, browser/network tools, or another script
to fetch CI data. Do not trigger CI, write to GitHub, or upload CI data. The
fetcher requires `gh` to be authenticated for the relevant Canonical
repository.

From the snapd repository root, create one unique session directory under the
ignored `.logs` directory and use it throughout the workflow:

```bash
PROJECT_ROOT="$(git rev-parse --show-toplevel)"
mkdir -p "$PROJECT_ROOT/.logs"
SESSION_DIR="$(mktemp -d "$PROJECT_ROOT/.logs/spread-investigation-XXXXXX")"
printf '%s\n' "$SESSION_DIR"
```

Pass `--session-dir "$SESSION_DIR"` to every fetcher invocation. Store fetched
logs, artifacts, run metadata, notes, and the default handoff in this
directory; do not use `/tmp` or create another temporary directory for the
same workflow. Keep the directory while the workflow is running and ensure
`.logs` remains excluded from Spread worker archives. Redact credentials and
tokens in notes and final output.
