#!/usr/bin/env python3
"""
Fetch spread test results and failure logs from GitHub Actions for a snapd PR.

This script handles the deterministic operations of:
1. Finding workflow runs associated with a PR via the GitHub API
2. Downloading spread-results-* artifacts (JSON summaries)
3. Downloading failure log artifacts matching a configurable pattern
4. Extracting all artifacts to a local directory
5. Producing a manifest JSON for downstream analysis

Usage:
    GITHUB_TOKEN=$GITHUB_TOKEN python3 scripts/fetch_spread_results.py --pr 12345
    GITHUB_TOKEN=$GITHUB_TOKEN python3 scripts/fetch_spread_results.py --pr 12345 --repo canonical/snapd --log-pattern "*logs*"
"""

import argparse
import json
import os
import re
import sys
import tempfile
import zipfile
from fnmatch import fnmatch
from pathlib import Path
from urllib.parse import urljoin

try:
    import urllib.request
except ImportError:
    print("error: urllib.request is required (part of Python standard library)", file=sys.stderr)
    sys.exit(1)


GITHUB_API_BASE = "https://api.github.com"
GITHUB_API_VERSION = "2022-11-28"
USER_AGENT = "snapd-spread-results-fetcher/1.0"


def github_api_request(path: str, token: str, accept_zip: bool = False) -> tuple:
    """
    Make an authenticated request to the GitHub API.

    Returns (data, headers) where data is bytes.
    """
    url = urljoin(GITHUB_API_BASE, path)
    req = urllib.request.Request(url)
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("X-GitHub-Api-Version", GITHUB_API_VERSION)
    req.add_header("Accept", "application/vnd.github+json")
    req.add_header("User-Agent", USER_AGENT)

    try:
        with urllib.request.urlopen(req) as resp:
            return resp.read(), dict(resp.headers)
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        print(f"error: GitHub API request failed: {e.code} {e.reason}", file=sys.stderr)
        print(f"error: URL: {url}", file=sys.stderr)
        print(f"error: Response: {body[:500]}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"error: cannot reach GitHub API: {e}", file=sys.stderr)
        sys.exit(1)


def github_api_json(path: str, token: str) -> dict:
    """Make a GitHub API request and parse JSON response."""
    data, _ = github_api_request(path, token)
    try:
        return json.loads(data.decode("utf-8"))
    except json.JSONDecodeError as e:
        print(f"error: invalid JSON from GitHub API: {e}", file=sys.stderr)
        sys.exit(1)


def download_artifact_zip(artifact_id: int, repo: str, token: str) -> bytes:
    """
    Download an artifact ZIP from GitHub Actions.

    GitHub artifact download requires a special redirect flow.
    """
    class _NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None

    path = f"/repos/{repo}/actions/artifacts/{artifact_id}/zip"
    url = urljoin(GITHUB_API_BASE, path)
    api_req = urllib.request.Request(url, method="GET")
    api_req.add_header("Authorization", f"Bearer {token}")
    api_req.add_header("Accept", "application/vnd.github+json")
    api_req.add_header("X-GitHub-Api-Version", GITHUB_API_VERSION)
    api_req.add_header("User-Agent", USER_AGENT)

    opener = urllib.request.build_opener(_NoRedirect)

    try:
        try:
            with opener.open(api_req) as resp:
                return resp.read()
        except urllib.error.HTTPError as e:
            # GitHub returns a redirect to a time-limited artifact URL.
            if e.code not in (301, 302, 303, 307, 308):
                body = e.read().decode("utf-8", errors="replace")
                print(f"error: artifact download failed: {e.code} {e.reason}", file=sys.stderr)
                print(f"error: Response: {body[:500]}", file=sys.stderr)
                sys.exit(1)

            redirect_url = e.headers.get("Location")
            if not redirect_url:
                print("error: artifact download redirect missing Location header", file=sys.stderr)
                sys.exit(1)

            # Download from the redirect target without GitHub Authorization header.
            # The redirected URL contains its own short-lived credentials.
            zip_req = urllib.request.Request(redirect_url, method="GET")
            zip_req.add_header("User-Agent", USER_AGENT)
            with urllib.request.urlopen(zip_req) as zip_resp:
                return zip_resp.read()
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        print(f"error: artifact download failed: {e.code} {e.reason}", file=sys.stderr)
        print(f"error: Response: {body[:500]}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"error: cannot download artifact: {e}", file=sys.stderr)
        sys.exit(1)


def find_workflow_runs(repo: str, head_sha: str, token: str, workflow_name_filter: str | None = None) -> list:
    """Find workflow runs for a given head SHA."""
    # GitHub allows filtering by head_sha via query param
    path = f"/repos/{repo}/actions/runs?head_sha={head_sha}&per_page=100"
    data = github_api_json(path, token)
    runs = data.get("workflow_runs", [])

    # Filter to runs that likely contain spread tests
    # Either by workflow name or by the presence of spread-results artifacts
    if workflow_name_filter:
        runs = [r for r in runs if workflow_name_filter.lower() in r.get("name", "").lower()]

    return runs


def find_artifacts(repo: str, run_id: int, token: str) -> list:
    """List artifacts for a workflow run."""
    path = f"/repos/{repo}/actions/runs/{run_id}/artifacts?per_page=100"
    data = github_api_json(path, token)
    return data.get("artifacts", [])


def download_and_extract_artifact(artifact: dict, dest_dir: Path, repo: str, token: str) -> Path:
    """Download an artifact ZIP and extract it to dest_dir."""
    artifact_id = artifact["id"]
    artifact_name = artifact["name"]
    zip_bytes = download_artifact_zip(artifact_id, repo, token)

    # Create a sub-directory for this artifact
    extract_dir = dest_dir / f"{artifact_name}_{artifact_id}"
    extract_dir.mkdir(parents=True, exist_ok=True)

    zip_path = extract_dir / "artifact.zip"
    zip_path.write_bytes(zip_bytes)

    try:
        with zipfile.ZipFile(zip_path, "r") as zf:
            zf.extractall(extract_dir)
    except zipfile.BadZipFile:
        print(f"warning: artifact {artifact_name} ({artifact_id}) is not a valid ZIP", file=sys.stderr)
        return extract_dir

    # Remove the zip file after extraction to keep things tidy
    zip_path.unlink()
    return extract_dir


def scan_json_files(directory: Path) -> list:
    """Recursively find JSON files under directory."""
    results = []
    for path in directory.rglob("*.json"):
        try:
            content = json.loads(path.read_text(encoding="utf-8"))
            results.append({
                "path": str(path),
                "relative_path": str(path.relative_to(directory)),
                "top_level_keys": list(content.keys()) if isinstance(content, dict) else None,
                "is_array": isinstance(content, list),
                "array_length": len(content) if isinstance(content, list) else None,
            })
        except (json.JSONDecodeError, UnicodeDecodeError, OSError) as e:
            results.append({
                "path": str(path),
                "relative_path": str(path.relative_to(directory)),
                "error": str(e),
            })
    return results


def scan_log_files(directory: Path) -> list:
    """Recursively find probable log files under directory."""
    results = []
    for path in directory.rglob("*"):
        if path.is_file() and not path.suffix == ".json":
            # Treat any non-JSON file as a potential log
            results.append({
                "path": str(path),
                "relative_path": str(path.relative_to(directory)),
                "size_bytes": path.stat().st_size,
            })
    return results


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Fetch spread test artifacts from a GitHub PR"
    )
    parser.add_argument("--pr", type=int, required=True, help="Pull request number")
    parser.add_argument(
        "--repo",
        default="canonical/snapd",
        help="Repository in owner/repo format (default: canonical/snapd)",
    )
    parser.add_argument(
        "--log-pattern",
        default="*logs*",
        help="Glob pattern for failure log artifact names (default: *logs*)",
    )
    parser.add_argument(
        "--workflow-name",
        default="",
        help="Optional substring to filter workflow runs by name",
    )
    parser.add_argument(
        "--output-dir",
        default=None,
        help="Directory to store downloaded artifacts (default: auto-created temp dir)",
    )
    parser.add_argument(
        "--manifest",
        default="-",
        help="Path to write manifest JSON (default: '-' for stdout)",
    )

    args = parser.parse_args()

    token = os.environ.get("GITHUB_TOKEN")
    if not token:
        print("error: GITHUB_TOKEN environment variable is required", file=sys.stderr)
        return 1

    # Validate repo format
    if not re.match(r"^[\w.-]+/[\w.-]+$", args.repo):
        print("error: --repo must be in owner/repo format", file=sys.stderr)
        return 1

    # 1. Get PR details
    pr_data = github_api_json(f"/repos/{args.repo}/pulls/{args.pr}", token)
    head_sha = pr_data.get("head", {}).get("sha")
    if not head_sha:
        print("error: cannot determine head SHA for PR", file=sys.stderr)
        return 1

    pr_info = {
        "number": args.pr,
        "title": pr_data.get("title"),
        "head_sha": head_sha,
        "head_ref": pr_data.get("head", {}).get("ref"),
        "html_url": pr_data.get("html_url"),
    }

    # 1b. Get changed files
    changed_files = []
    page = 1
    while True:
        files_page = github_api_json(
            f"/repos/{args.repo}/pulls/{args.pr}/files?per_page=100&page={page}", token
        )
        if not files_page:
            break
        for f in files_page:
            changed_files.append({
                "filename": f.get("filename"),
                "status": f.get("status"),
                "additions": f.get("additions"),
                "deletions": f.get("deletions"),
                "changes": f.get("changes"),
                "patch": f.get("patch"),
            })
        if len(files_page) < 100:
            break
        page += 1

    pr_info["changed_files"] = changed_files
    pr_info["changed_files_count"] = len(changed_files)

    # 2. Prepare output directory
    if args.output_dir:
        output_dir = Path(args.output_dir)
        output_dir.mkdir(parents=True, exist_ok=True)
    else:
        output_dir = Path(tempfile.mkdtemp(prefix="spread-results-"))

    # 3. Find workflow runs
    workflow_runs = find_workflow_runs(
        args.repo, head_sha, token, args.workflow_name or None
    )

    if not workflow_runs:
        print(f"warning: no workflow runs found for PR {args.pr} (sha {head_sha})", file=sys.stderr)

    # 4. Process each workflow run
    manifest = {
        "pr": pr_info,
        "repository": args.repo,
        "output_directory": str(output_dir.absolute()),
        "workflow_runs": [],
    }

    for run in workflow_runs:
        run_id = run["id"]
        run_entry = {
            "run_id": run_id,
            "run_number": run.get("run_number"),
            "name": run.get("name"),
            "conclusion": run.get("conclusion"),
            "status": run.get("status"),
            "html_url": run.get("html_url"),
            "artifacts": {
                "spread_results": [],
                "failure_logs": [],
            },
        }

        artifacts = find_artifacts(args.repo, run_id, token)

        # Categorize artifacts
        spread_artifacts = [a for a in artifacts if fnmatch(a.get("name", ""), "spread-results-*")]
        log_artifacts = [a for a in artifacts if fnmatch(a.get("name", ""), args.log_pattern)]

        # Download spread results
        for art in spread_artifacts:
            extract_dir = download_and_extract_artifact(art, output_dir, args.repo, token)
            json_files = scan_json_files(extract_dir)
            run_entry["artifacts"]["spread_results"].append({
                "artifact_name": art["name"],
                "artifact_id": art["id"],
                "size_in_bytes": art.get("size_in_bytes"),
                "extracted_path": str(extract_dir.absolute()),
                "json_files": json_files,
            })

        # Download failure logs
        for art in log_artifacts:
            extract_dir = download_and_extract_artifact(art, output_dir, args.repo, token)
            log_files = scan_log_files(extract_dir)
            run_entry["artifacts"]["failure_logs"].append({
                "artifact_name": art["name"],
                "artifact_id": art["id"],
                "size_in_bytes": art.get("size_in_bytes"),
                "extracted_path": str(extract_dir.absolute()),
                "log_files": log_files,
            })

        manifest["workflow_runs"].append(run_entry)

    # 5. Write manifest
    manifest_json = json.dumps(manifest, indent=2)
    if args.manifest == "-":
        print(manifest_json)
    else:
        Path(args.manifest).write_text(manifest_json, encoding="utf-8")

    return 0


if __name__ == "__main__":
    sys.exit(main())
