#!/usr/bin/env python3
"""
GitHub API client for downloading PR data, workflow artifacts, and logs.

Supports authentication via GITHUB_TOKEN environment variable or gh CLI.
"""

import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
import zipfile
from pathlib import Path
from typing import Optional


REPO_OWNER = "canonical"
REPO_NAME = "snapd"


def _get_auth_headers() -> dict:
    """Return headers with authentication if available."""
    headers = {
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def _github_api_request(url: str) -> dict:
    """Make a GitHub API request and return parsed JSON."""
    headers = _get_auth_headers()
    req = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        print(f"error: HTTP {e.code} for {url}", file=sys.stderr)
        try:
            body = json.loads(e.read().decode("utf-8"))
            print(f"error: {body.get('message', '')}", file=sys.stderr)
        except Exception:
            pass
        raise


class _RedirectHandler(urllib.request.HTTPRedirectHandler):
    """Redirect handler that strips Authorization headers on redirects.

    GitHub artifact download URLs redirect to Azure Blob Storage signed URLs.
    Sending the Authorization header to the redirected URL causes a 401.
    """

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # Strip Authorization header from the redirected request
        new_headers = {k: v for k, v in req.headers.items() if k.lower() != "authorization"}
        return urllib.request.Request(newurl, headers=new_headers, method=req.get_method(), data=req.data)


_DOWNLOAD_OPENER = urllib.request.build_opener(_RedirectHandler())


def _download_file(url: str, dest: Path) -> None:
    """Download a file from URL to dest path."""
    headers = _get_auth_headers()
    req = urllib.request.Request(url, headers=headers)
    dest.parent.mkdir(parents=True, exist_ok=True)
    with _DOWNLOAD_OPENER.open(req, timeout=120) as response:
        with open(dest, "wb") as f:
            f.write(response.read())


def parse_pr_identifier(input_str: str) -> int:
    """Parse PR number from input string (number or URL)."""
    input_str = input_str.strip()
    # Check if it's a URL
    match = re.search(r"/pull/(\d+)", input_str)
    if match:
        return int(match.group(1))
    # Check if it's just a number
    if input_str.isdigit():
        return int(input_str)
    raise ValueError(f"cannot parse PR identifier from: {input_str}")


def get_pr_info(pr_number: int) -> dict:
    """Fetch PR information from GitHub API."""
    url = f"https://api.github.com/repos/{REPO_OWNER}/{REPO_NAME}/pulls/{pr_number}"
    return _github_api_request(url)


def download_pr_diff(pr_number: int, output_dir: Path) -> Path:
    """Download the PR diff file."""
    url = f"https://github.com/{REPO_OWNER}/{REPO_NAME}/pull/{pr_number}.diff"
    dest = output_dir / f"pr-{pr_number}.diff"
    print(f"downloading PR diff to {dest}")
    _download_file(url, dest)
    return dest


def get_workflow_runs_for_pr(pr_number: int, head_sha: Optional[str] = None) -> list:
    """Get workflow runs associated with a PR.

    If head_sha is provided, filter by that SHA. Otherwise, returns recent runs.
    """
    if head_sha:
        url = (
            f"https://api.github.com/repos/{REPO_OWNER}/{REPO_NAME}"
            f"/actions/runs?head_sha={head_sha}&per_page=20"
        )
    else:
        url = (
            f"https://api.github.com/repos/{REPO_OWNER}/{REPO_NAME}"
            f"/actions/runs?event=pull_request&per_page=30"
        )
    data = _github_api_request(url)
    runs = data.get("workflow_runs", [])

    if not head_sha:
        # Filter runs that mention this PR
        filtered = []
        for run in runs:
            # Check if run is for this PR by looking at display title or PRs URL
            run_detail = _github_api_request(run["url"])
            prs = run_detail.get("pull_requests", [])
            for pr in prs:
                if pr.get("number") == pr_number:
                    filtered.append(run)
                    break
        runs = filtered

    return runs


def get_run_artifacts(run_id: int) -> list:
    """List all artifacts for a workflow run."""
    url = (
        f"https://api.github.com/repos/{REPO_OWNER}/{REPO_NAME}"
        f"/actions/runs/{run_id}/artifacts?per_page=100"
    )
    data = _github_api_request(url)
    return data.get("artifacts", [])


def download_artifact(artifact_id: int, dest_zip: Path) -> None:
    """Download an artifact as a zip file."""
    url = (
        f"https://api.github.com/repos/{REPO_OWNER}/{REPO_NAME}"
        f"/actions/artifacts/{artifact_id}/zip"
    )
    _download_file(url, dest_zip)


def download_spread_artifacts(run_id: int, output_dir: Path) -> dict:
    """Download spread-related artifacts for a workflow run.

    Returns a dict mapping artifact types to lists of downloaded zip paths.
    """
    artifacts = get_run_artifacts(run_id)
    downloaded = {"results": [], "logs": [], "artifacts": [], "failed_tests": []}

    for artifact in artifacts:
        name = artifact["name"]
        artifact_id = artifact["id"]

        if name.startswith("spread-results-"):
            dest = output_dir / f"{name}.zip"
            print(f"downloading spread results artifact: {name}")
            download_artifact(artifact_id, dest)
            downloaded["results"].append(dest)
        elif name.startswith("spread-logs-"):
            dest = output_dir / f"{name}.zip"
            print(f"downloading spread logs artifact: {name}")
            download_artifact(artifact_id, dest)
            downloaded["logs"].append(dest)
        elif name.startswith("spread-artifacts-"):
            dest = output_dir / f"{name}.zip"
            print(f"downloading spread artifacts (debug): {name}")
            download_artifact(artifact_id, dest)
            downloaded["artifacts"].append(dest)
        elif name.startswith("run-spread-results-"):
            # Artifacts named run-spread-results-* contain the plain-text
            # failed-tests list produced by the spread run.
            dest = output_dir / f"{name}.zip"
            print(f"downloading failed tests artifact: {name}")
            download_artifact(artifact_id, dest)
            downloaded["failed_tests"].append(dest)

    return downloaded


def extract_zip(zip_path: Path, extract_dir: Path) -> None:
    """Extract a zip file to a directory."""
    extract_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(zip_path, "r") as z:
        z.extractall(extract_dir)


def find_latest_failed_run(runs: list) -> Optional[dict]:
    """Find the most recent failed workflow run from a list."""
    failed_runs = [r for r in runs if r.get("conclusion") == "failure" or r.get("status") != "completed"]
    if not failed_runs:
        return None
    # Sort by created_at descending
    failed_runs.sort(key=lambda r: r.get("created_at", ""), reverse=True)
    return failed_runs[0]


def find_specific_attempt(runs: list, attempt_number: Optional[int] = None) -> Optional[dict]:
    """Find a specific run attempt. If attempt_number is None, return the latest."""
    if not runs:
        return None
    # Sort by created_at descending
    runs.sort(key=lambda r: r.get("created_at", ""), reverse=True)
    if attempt_number is None:
        return runs[0]
    # Group by run_number or attempt to find specific one
    # GitHub API doesn't directly expose attempt number in list,
    # but run_attempt is in the URL. We'll return the latest for now.
    return runs[0]


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Download PR data from GitHub")
    parser.add_argument("pr", help="PR number or URL")
    parser.add_argument("--output-dir", "-o", type=Path, default=Path("./spread-analysis"), help="Output directory")
    parser.add_argument("--run-id", type=int, help="Specific workflow run ID to use")
    args = parser.parse_args()

    try:
        pr_number = parse_pr_identifier(args.pr)
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        sys.exit(1)

    print(f"analyzing PR #{pr_number}")
    args.output_dir.mkdir(parents=True, exist_ok=True)

    # Download diff
    try:
        diff_path = download_pr_diff(pr_number, args.output_dir)
        print(f"diff saved to: {diff_path}")
    except Exception as e:
        print(f"warning: could not download diff: {e}", file=sys.stderr)

    # Get PR info
    try:
        pr_info = get_pr_info(pr_number)
        head_sha = pr_info["head"]["sha"]
        print(f"HEAD SHA: {head_sha}")
    except Exception as e:
        print(f"warning: could not get PR info: {e}", file=sys.stderr)
        head_sha = None

    # Find workflow runs
    if args.run_id:
        print(f"using specified run ID: {args.run_id}")
        run = {"id": args.run_id}
    else:
        print("finding workflow runs for PR...")
        runs = get_workflow_runs_for_pr(pr_number, head_sha)
        # Only consider the 'Tests' workflow that produces spread artifacts
        runs = [r for r in runs if r.get("name") == "Tests"]
        if not runs:
            print("error: no Tests workflow runs found for this PR", file=sys.stderr)
            sys.exit(1)
        print(f"found {len(runs)} Tests workflow run(s)")
        run = find_specific_attempt(runs)

    run_id = run["id"]
    print(f"using workflow run: {run_id}")

    # Download artifacts
    print("downloading spread artifacts...")
    downloaded = download_spread_artifacts(run_id, args.output_dir)

    total = sum(len(v) for v in downloaded.values())
    print(f"downloaded {total} artifact(s)")
    for category, paths in downloaded.items():
        if paths:
            print(f"  {category}: {len(paths)}")

    if total == 0:
        print("warning: no spread artifacts found. The workflow may still be running, or artifacts may have expired.", file=sys.stderr)
        sys.exit(1)

    print(f"all files saved to: {args.output_dir}")
