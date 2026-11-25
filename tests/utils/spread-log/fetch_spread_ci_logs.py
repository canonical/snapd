#!/usr/bin/env python3
"""Fetch existing snapd Spread CI logs from GitHub Actions job URLs."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path
from urllib.parse import urlparse

SNAPD_REPO = "canonical/snapd"


def parse_job_url(value: str) -> tuple[str, str]:
    parsed = urlparse(value)
    match = re.fullmatch(
        r"/canonical/snapd/actions/runs/([0-9]+)/job/([0-9]+)/?",
        parsed.path,
    )
    if parsed.scheme != "https" or parsed.netloc != "github.com" or not match:
        raise argparse.ArgumentTypeError(
            "expected a snapd job URL: "
            "https://github.com/canonical/snapd/actions/runs/<run-id>/job/<job-id>"
        )
    return match.group(1), match.group(2)


def run_gh(args: list[str]) -> str:
    try:
        result = subprocess.run(
            ["gh", *args],
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
    except FileNotFoundError as error:
        raise RuntimeError("GitHub CLI 'gh' is required") from error
    except subprocess.CalledProcessError as error:
        details = (error.stderr or "").strip()
        raise RuntimeError(
            f"gh {' '.join(args)} failed"
            + (f": {details}" if details else f" (exit {error.returncode})")
        ) from error
    return result.stdout or ""


def run_gh_to_file(args: list[str], destination: Path) -> None:
    try:
        with destination.open("w", encoding="utf-8") as output:
            subprocess.run(
                ["gh", *args],
                check=True,
                text=True,
                stdout=output,
                stderr=subprocess.PIPE,
            )
    except FileNotFoundError as error:
        raise RuntimeError("GitHub CLI 'gh' is required") from error
    except subprocess.CalledProcessError as error:
        details = (error.stderr or "").strip()
        raise RuntimeError(
            f"gh {' '.join(args)} failed"
            + (f": {details}" if details else f" (exit {error.returncode})")
        ) from error


def create_output_dir(run_id: str, session_dir: Path | None = None) -> Path:
    parent = Path.cwd() / ".logs"
    if session_dir is not None:
        if not session_dir.is_dir():
            raise RuntimeError(
                f"workflow session directory does not exist: {session_dir}"
            )
        parent = session_dir
    else:
        parent.mkdir(parents=True, exist_ok=True)
    return Path(
        tempfile.mkdtemp(prefix=f"spread-ci-logs-{run_id}-", dir=parent)
    )


def fetch_job(
    output_dir: Path, run_id: str, job_id: str, *, label: str
) -> Path:
    log_path = output_dir / "logs" / f"run-{run_id}-job-{job_id}.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    run_gh_to_file(
        [
            "run",
            "view",
            run_id,
            "--repo",
            SNAPD_REPO,
            "--job",
            job_id,
            "--log",
        ],
        log_path,
    )
    print(f"{label}: {log_path}")
    return log_path


def fetch_artifacts(output_dir: Path, run_ids: set[str]) -> list[dict]:
    outcomes = []
    for run_id in sorted(run_ids):
        destination = output_dir / "artifacts" / f"run-{run_id}"
        destination.mkdir(parents=True)
        args = [
            "run",
            "download",
            run_id,
            "--repo",
            SNAPD_REPO,
            "--dir",
            str(destination),
        ]
        try:
            details = run_gh(args)
            outcomes.append(
                {
                    "run_id": run_id,
                    "status": "downloaded",
                    "directory": str(destination),
                }
            )
            if details.strip():
                print(details.strip())
        except RuntimeError as error:
            outcome = {
                "run_id": run_id,
                "status": "unavailable",
                "error": str(error),
            }
            outcomes.append(outcome)
            print(
                f"Warning: CI artifacts unavailable for run {run_id}: {error}",
                file=sys.stderr,
            )
    return outcomes


def fetch_direct_jobs(args: argparse.Namespace) -> int:
    jobs = []
    seen = set()
    for url in args.job_url:
        job = parse_job_url(url)
        if job not in seen:
            jobs.append(job)
            seen.add(job)

    output_dir = create_output_dir("jobs", args.session_dir)
    print(f"Output directory: {output_dir}")
    run_ids = {run_id for run_id, _ in jobs}
    for run_id in run_ids:
        metadata = run_gh(
            [
                "run",
                "view",
                run_id,
                "--repo",
                SNAPD_REPO,
                "--json",
                "databaseId,displayTitle,workflowName,createdAt,conclusion,url,jobs",
            ]
        )
        (output_dir / f"run-{run_id}.json").write_text(metadata, encoding="utf-8")

    log_records = []
    for run_id, job_id in jobs:
        path = fetch_job(
            output_dir, run_id, job_id, label=f"run {run_id} job {job_id}"
        )
        log_records.append(
            {"run_id": run_id, "job_id": job_id, "log": str(path)}
        )
    artifacts = fetch_artifacts(output_dir, run_ids)
    (output_dir / "manifest.json").write_text(
        json.dumps(
            {
                "jobs": log_records,
                "artifacts": artifacts,
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Download Spread CI logs and all run artifacts for snapd GitHub "
            "Actions job URLs. By default, output is stored in .logs under "
            "the current working directory."
        )
    )
    parser.add_argument(
        "--job-url",
        nargs="+",
        required=True,
        help=(
            "One or more existing canonical/snapd Actions job URLs"
        ),
    )
    parser.add_argument(
        "--session-dir",
        type=Path,
        help=(
            "Existing workflow session directory; store this invocation's "
            "downloads in a unique subdirectory beneath it"
        ),
    )
    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    try:
        return fetch_direct_jobs(args)
    except (argparse.ArgumentTypeError, RuntimeError) as error:
        parser.error(str(error))
    return 0


if __name__ == "__main__":
    sys.exit(main())
