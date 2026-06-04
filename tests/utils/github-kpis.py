#!/usr/bin/env python3

import argparse
import sys
import json
import subprocess
import os
import tempfile
import re
import time
from datetime import date, datetime, timedelta, timezone
from typing import Optional, Dict, List


def show_help():
    help_text = """Usage: github-kpis.py (--start YYYY-MM-DD [--end YYYY-MM-DD] | --input-json PATH) [--attempts] [--forced] [--skipped] [--runtime] [--test-totals] [--author-times] [--all] [--pretty]

If the script errors at any point, it will output the JSON collected before that stage.
To resume from that JSON, save it to a file and use --input-json with the path to that file. 
You can also use --input-json - to read from stdin.

Ex: 
To get all data for PRs merged starting from May 1st 2026 (included):
./github-kpis.py --start 2026-05-01 --all > pr_data.json

To calculate only force merge data on a previously calculated JSON file:
./github-kpis.py --input-json pr_data.json --forced > pr_data_with_forced.json
or
cat pr_data.json | ./github-kpis.py --input-json - --forced > pr_data_with_forced.json

Options:
  --start DATE    Required. Start merged date (inclusive).
  --end DATE      Optional. End merged date (inclusive, day granularity).
  --input-json    Resume from an existing JSON file instead of fetching PRs. Use - for stdin.
  --attempts      Add number of attempts made of running the Tests workflow on the last PR update before merging.
  --forced        Add field to specify whether or not the PR was force merged.
  --skipped       Add field to specify how many tests (excluding variants) were skipped via snapd-testing-skip.
  --runtime       Add total runtime of all attempts of the Tests workflow on the last PR update before merging.
  --test-totals   Add total number of spread tests run on the last PR update before merging.
  --author-times  Add author classification and times PR spent in non-approved and approved states before merging.
  --all           Add all of the above fields.
    --pretty        Pretty-print the final JSON output.
  -h, --help      Show this help.


Once the json is collected, here are some useful jq queries:
# The most interesting data is ones in which spread tests were run, therefore all the queries will
# look at PRs with "spread-skipped" == false.

# Get the percentage of force merges
total=$(jq '[ .[] | select(."spread-skipped" == false) ] | length' < pr_data.json)
num_force_merges=$(jq '[ .[] | select(."spread-skipped" == false) | select(."force-merged" == true) ] | length' < pr_data.json)
echo "scale=2; $num_force_merges / $total * 100" | bc

# Get the average number of minutes for the first attempt when only fundamental tests were run
jq '[ .[] | select(."spread-skipped" == false) | select(."first-attempt-only-fundamental" == true) | ."first-attempt-minutes" ] | add / length' < pr_data.json

# Get the average total runtime in minutes
jq '[ .[] | select(."spread-skipped" == false) | ."total-runtime-minutes" ] | add / length' < pr_data.json

# Get the average number of attempts
jq '[ .[] | select(."spread-skipped" == false) | ."num-attempts" ] | add / length' < pr_data.json"""
    print(help_text)


class ProgressBar:
    def __init__(self, label: str, total: int):
        self.label = label
        self.total = total
        self.done = 0
        self.width = 30

    def tick(self):
        if self.total <= 0:
            return

        self.done += 1
        filled = (self.done * self.width) // self.total
        empty = self.width - filled

        progress_str = f"\r{self.label} progress: [{'#' * filled}{'-' * empty}] {self.done}/{self.total}"
        sys.stderr.write(progress_str)
        sys.stderr.flush()

        if self.done == self.total:
            sys.stderr.write("\n")
            sys.stderr.flush()


class GhCommandError(RuntimeError):
    """Raised when a gh command fails."""


def gh_request(*args, allow_failure: bool = False) -> Optional[str]:
    """Execute a gh command, retrying transient 50x failures."""
    attempts = 0
    while attempts < 5:
        try:
            result = subprocess.run(
                ["gh"] + list(args),
                capture_output=True,
                text=True,
                check=False
            )
            if result.returncode == 0:
                return result.stdout
            
            if "HTTP 50" in result.stderr:
                attempts += 1
                sys.stderr.write(f"received: {result.stderr} (attempt {attempts}/5), retrying in 1s...\n")
                sys.stderr.flush()
                time.sleep(1)
            else:
                if allow_failure:
                    return None
                sys.stderr.write(result.stderr)
                raise GhCommandError(f"gh command failed: gh {' '.join(args)}\nError: {result.stderr}")
        except Exception as e:
            if isinstance(e, GhCommandError):
                raise
            if allow_failure:
                return None
            sys.stderr.write(f"Error running gh command: {e}\n")
            raise GhCommandError(f"error running gh command: {e}") from e
    
    if allow_failure:
        return None
    sys.stderr.write(f"gh command failed after 5 retries: gh {' '.join(args)}\n")
    raise GhCommandError(f"gh command failed after 5 retries: gh {' '.join(args)}")


def ensure_attempt_metadata(pr: Dict) -> None:
    """Populate attempt metadata for a PR when it is missing."""
    if pr.get("spread-skipped") == True:
        pr.setdefault("num-attempts", 1)
        pr.setdefault("databaseId", None)
        return

    if "num-attempts" in pr and "databaseId" in pr:
        return

    commit = pr.get("headRefOid")
    output = gh_request("run", "list", "--repo", "canonical/snapd",
                           "--commit", commit, "--workflow", "ci-test.yaml",
                           "--json", "attempt,databaseId",
                           "--jq", "first(.[] | {attempt: (.attempt // 0), databaseId: .databaseId}) // {attempt: 0, databaseId: null}")
    run_data = json.loads(output)
    pr["num-attempts"] = run_data.get("attempt", 0)
    pr["databaseId"] = run_data.get("databaseId")


def classify_author(login: Optional[str], association: Optional[str]) -> str:
    """Classify an author as snapd, canonical, or external."""
    if not login:
        return None
    
    if association not in {"MEMBER", "OWNER", "COLLABORATOR"}:
        return "external"

    permission = gh_request("api", f"/repos/canonical/snapd/collaborators/{login}/permission", "--jq", ".permission", allow_failure=True)
    if permission and permission.strip() in {"admin", "maintain", "write"}:
        return "snapd"

    membership = gh_request("api", f"/orgs/canonical/memberships/{login}", "--jq", ".state", allow_failure=True)
    if membership and membership.strip() == "active":
        return "canonical"

    return None


def get_author_and_times(prs_json: List[Dict]) -> List[Dict]:
    """Populate author and state-time fields using one PR details fetch."""
    total_prs = len(prs_json)
    progress = ProgressBar("Author+state", total_prs)

    result = []
    classification_cache: Dict[str, str] = {}
    for pr in prs_json:
        number = pr.get("number")
        pr_meta = json.loads(gh_request("api", f"/repos/canonical/snapd/pulls/{number}"))

        login = pr.get("_author_login") or (pr_meta.get("user") or {}).get("login")
        association = pr_meta.get("author_association")

        if not login:
            raise GhCommandError(f"failed to get author login for PR #{number}")

        if login not in classification_cache:
            classification_cache[login] = classify_author(login, association)

        pr["author-type"] = classification_cache[login]

        try:
            created_at_s = pr.get("createdAt") or pr_meta.get("created_at")
            merged_at_s = pr.get("mergedAt") or pr_meta.get("merged_at")

            if not created_at_s or not merged_at_s:
                pr["time-to-approved-hours"] = None
                pr["time-approved-to-merged-hours"] = None
                result.append(pr)
                progress.tick()
                continue

            created_at = datetime.fromisoformat(created_at_s.replace("Z", "+00:00"))
            merged_at = datetime.fromisoformat(merged_at_s.replace("Z", "+00:00"))

            reviews = pr.get("_reviews")
            if reviews is None:
                reviews = json.loads(gh_request("api", f"/repos/canonical/snapd/pulls/{number}/reviews?per_page=100"))

            # Collect approval events in chronological order, tracking the
            # latest state per reviewer so dismissed approvals don't count.
            review_timeline = []
            for rv in reviews:
                rv_time = rv.get("submitted_at") or rv.get("submittedAt")
                reviewer = (rv.get("user") or rv.get("author") or {}).get("login")
                state = (rv.get("state") or "").upper()
                if not rv_time or not reviewer or state not in {"APPROVED", "CHANGES_REQUESTED", "DISMISSED"}:
                    continue
                review_timeline.append((
                    datetime.fromisoformat(rv_time.replace("Z", "+00:00")),
                    reviewer,
                    state,
                ))

            review_timeline.sort(key=lambda x: x[0])

            # Replay reviews to find the earliest moment at which two distinct
            # reviewers have current APPROVED state with no blocking reviews.
            reviewer_states: Dict[str, str] = {}
            two_approvals_at = None
            for ev_time, reviewer, state in review_timeline:
                if ev_time > merged_at:
                    break
                reviewer_states[reviewer] = state
                approvals = sum(1 for s in reviewer_states.values() if s == "APPROVED")
                has_blocking = any(s == "CHANGES_REQUESTED" for s in reviewer_states.values())
                if approvals >= 2 and not has_blocking:
                    two_approvals_at = ev_time
                    break

            if two_approvals_at is None:
                pr["time-to-approved-hours"] = None
                pr["time-approved-to-merged-hours"] = None
            else:
                pr["time-to-approved-hours"] = round((two_approvals_at - created_at).total_seconds() / 3600.0, 2)
                pr["time-approved-to-merged-hours"] = round((merged_at - two_approvals_at).total_seconds() / 3600.0, 2)
        except (GhCommandError, json.JSONDecodeError, ValueError):
            pr["time-to-approved-hours"] = None
            pr["time-approved-to-merged-hours"] = None

        result.append(pr)
        progress.tick()

    return result


def get_total_tests_run(prs_json: List[Dict]) -> List[Dict]:
    """Get total number of spread tests run for each PR."""
    total_prs = len(prs_json)
    progress = ProgressBar("Test totals", total_prs)
    
    result = []
    for pr in prs_json:
        if pr.get("spread-skipped") == True:
            pr["total-tests-run"] = 0
            result.append(pr)
        else:
            try:
                ensure_attempt_metadata(pr)
            except (GhCommandError, json.JSONDecodeError):
                pr["total-tests-run"] = None
                result.append(pr)
                progress.tick()
                continue

            db_id = pr.get("databaseId")
            num_attempts = pr.get("num-attempts", 0)
            
            if not db_id or num_attempts == 0:
                pr["total-tests-run"] = None
                result.append(pr)
                progress.tick()
                continue
            
            with tempfile.TemporaryDirectory() as tmpdir:
                try:
                    gh_request("run", "download", str(db_id), "--repo", "canonical/snapd",
                                "--dir", tmpdir, "--pattern", "spread-results-*")
                except GhCommandError:
                    pr["total-tests-run"] = None
                    result.append(pr)
                    progress.tick()
                    continue
                
                total_tests = 0
                first_attempt_for_system = {}
                first_file_for_system = {}
                
                # Find all results.json files
                for root, _, files in os.walk(tmpdir):
                    if "results.json" in files:
                        json_file = os.path.join(root, "results.json")
                        dir_name = os.path.basename(root)
                        
                        match = re.match(r"^spread-results-[0-9]+-([0-9]+)-(.*)$", dir_name)
                        if match:
                            attempt_num = int(match.group(1))
                            system = match.group(2)
                            
                            if system not in first_attempt_for_system or attempt_num < first_attempt_for_system[system]:
                                first_attempt_for_system[system] = attempt_num
                                first_file_for_system[system] = json_file
                
                for system, json_file in first_file_for_system.items():
                    if os.path.isfile(json_file):
                        try:
                            with open(json_file) as f:
                                data = json.load(f)
                                results = data.get("results", {})
                                count = (results.get("task-passed", 0) + 
                                       results.get("task-failed", 0) +
                                       results.get("task-aborted", 0) +
                                       results.get("task-restore-failed", 0))
                                total_tests += count
                        except (OSError, json.JSONDecodeError):
                            pass
                
                pr["total-tests-run"] = total_tests
                result.append(pr)
        
        progress.tick()
    
    return result


def get_total_runtime(prs_json: List[Dict]) -> List[Dict]:
    """Calculate total runtime and first attempt runtime for each PR."""
    total_prs = len(prs_json)
    progress = ProgressBar("Runtime", total_prs)
    
    result = []
    for pr in prs_json:
        if pr.get("spread-skipped") == True:
            pr["total-runtime-minutes"] = None
            pr["first-attempt-minutes"] = None
            pr["first-attempt-only-fundamental"] = None
            result.append(pr)
        else:
            try:
                ensure_attempt_metadata(pr)
            except (GhCommandError, json.JSONDecodeError):
                pr["total-runtime-minutes"] = None
                pr["first-attempt-minutes"] = None
                pr["first-attempt-only-fundamental"] = None
                result.append(pr)
                progress.tick()
                continue

            db_id = pr.get("databaseId")
            num_attempts = pr.get("num-attempts", 0)
            
            if not db_id or num_attempts == 0:
                pr["total-runtime-minutes"] = None
                pr["first-attempt-minutes"] = None
                pr["first-attempt-only-fundamental"] = None
                result.append(pr)
                progress.tick()
                continue
            
            total_runtime = 0
            first_attempt_runtime = None
            first_attempt_only_fundamental = False
            
            for attempt in range(1, num_attempts + 1):
                json_fields = "startedAt,updatedAt"
                if attempt == 1:
                    json_fields += ",jobs"
                
                output = gh_request("run", "view", str(db_id), "--repo", "canonical/snapd",
                                     "--attempt", str(attempt), "--json", json_fields)
                data = json.loads(output)
                
                started = datetime.fromisoformat(data["startedAt"].replace("Z", "+00:00"))
                updated = datetime.fromisoformat(data["updatedAt"].replace("Z", "+00:00"))
                runtime = int((updated - started).total_seconds() // 60)
                
                if attempt == 1:
                    first_attempt_runtime = runtime
                    jobs = data.get("jobs", [])
                    job_names = [job.get("name", "") for job in jobs]
                    job_count = {}
                    for name in job_names:
                        job_count[name] = job_count.get(name, 0) + 1
                    
                    if job_count.get("spread ${{ matrix.group }}", 0) == 2:
                        first_attempt_only_fundamental = True
                
                total_runtime += runtime
            
            pr["total-runtime-minutes"] = total_runtime
            pr["first-attempt-minutes"] = first_attempt_runtime
            pr["first-attempt-only-fundamental"] = first_attempt_only_fundamental
            result.append(pr)
        
        progress.tick()
    
    return result


def get_skipped_tests(prs_json: List[Dict]) -> List[Dict]:
    """Get number of skipped tests for each PR."""
    total_prs = len(prs_json)
    progress = ProgressBar("Skipped tests", total_prs)
    
    result = []
    for pr in prs_json:
        if pr.get("spread-skipped") == True:
            pr["num-skipped"] = 0
            result.append(pr)
        else:
            number = pr.get("number")
            
            try:
                comments = pr.get("_comments")
                if comments is None:
                    output = gh_request("pr", "view", str(number), "--repo", "canonical/snapd",
                                         "--json", "comments", "--jq",
                                         '.comments.[] | select(.author.login == "github-actions") | .body')
                    lines = output.split("\n")
                else:
                    lines = []
                    for comment in comments:
                        author_login = ((comment.get("author") or {}).get("login"))
                        if author_login == "github-actions":
                            lines.extend((comment.get("body") or "").splitlines())

                skipped_section = False
                skipped_tests = set()
                
                for line in lines:
                    if "## Skipped" in line:
                        skipped_section = True
                    elif skipped_section and line.startswith("- "):
                        test_name = line[2:]
                        parts = test_name.split(":")
                        if len(parts) >= 3:
                            test_id = f"{parts[0]}:{parts[1]}:{parts[2]}"
                            skipped_tests.add(test_id)
                    elif skipped_section and len(skipped_tests) > 0:
                        skipped_section = False
                
                pr["num-skipped"] = len(skipped_tests)
            except GhCommandError:
                pr["num-skipped"] = 0
            
            result.append(pr)
        
        progress.tick()
    
    return result


def get_force_merged(prs_json: List[Dict]) -> List[Dict]:
    """Determine if each PR was force merged."""
    total_prs = len(prs_json)
    progress = ProgressBar("Force-merged", total_prs)
    
    result = []
    for pr in prs_json:
        if pr.get("spread-skipped") == True:
            pr["force-merged"] = False
            result.append(pr)
        else:
            number = pr.get("number")
            
            try:
                output = gh_request("pr", "checks", str(number), "--repo", "canonical/snapd",
                                     "--required", "--json", "bucket", "--jq",
                                     '[.[].bucket | select(. != "pass")] | length')
                num_not_passed = int(output.strip())
                pr["force-merged"] = num_not_passed > 0
            except (GhCommandError, ValueError):
                pr["force-merged"] = False
            
            result.append(pr)
        
        progress.tick()
    
    return result


def get_num_attempts(prs_json: List[Dict]) -> List[Dict]:
    """Get number of attempts for each PR."""
    total_prs = len(prs_json)
    progress = ProgressBar("Attempts", total_prs)
    
    result = []
    for pr in prs_json:
        try:
            ensure_attempt_metadata(pr)
        except (GhCommandError, json.JSONDecodeError):
            pr["num-attempts"] = 0
            pr["databaseId"] = None

        result.append(pr)
        
        progress.tick()
    
    return result


def list_prs_in_range(start_date: date, end_date_exclusive: date) -> List[Dict]:
    """Recursively list PRs in a date range, handling pagination.

    GitHub search qualifiers for merged dates are day-based, so paginate by
    day ranges to avoid repeatedly hitting the same 1000-result window.
    """
    start_iso = start_date.isoformat()
    end_iso = end_date_exclusive.isoformat()
    end_inclusive = (end_date_exclusive - timedelta(days=1)).isoformat()
    
    output = gh_request("pr", "list", "--repo", "canonical/snapd", "--limit", "1000",
                         "--search", f"merged:{start_iso}..{end_inclusive}",
                         "--json", "number,createdAt,mergedAt,headRefOid,labels,author,comments,reviews")
    
    prs = json.loads(output)
    count = len(prs)
    
    if count < 1000:
        # Process and return
        result = []
        for pr in prs:
            labels = pr.get("labels", [])
            label_names = [l.get("name", "") for l in labels]
            pr["spread-skipped"] = "Skip spread" in label_names
            pr["nested"] = "Run nested" in label_names
            pr["_author_login"] = (pr.get("author") or {}).get("login")
            pr["_comments"] = pr.get("comments")
            pr["_reviews"] = pr.get("reviews")
            del pr["labels"]
            if "author" in pr:
                del pr["author"]
            if "comments" in pr:
                del pr["comments"]
            if "reviews" in pr:
                del pr["reviews"]
            result.append(pr)
        return result
    
    # Too many results, need to paginate
    total_days = (end_date_exclusive - start_date).days
    if total_days <= 1:
        sys.stderr.write(f"cannot safely paginate: >1000 PRs in one day between {start_iso} and {end_iso}\n")
        sys.exit(1)

    mid_date = start_date + timedelta(days=total_days // 2)
    if mid_date <= start_date or mid_date >= end_date_exclusive:
        sys.stderr.write(f"cannot safely paginate: invalid split between {start_iso} and {end_iso}\n")
        sys.exit(1)

    left = list_prs_in_range(start_date, mid_date)
    right = list_prs_in_range(mid_date, end_date_exclusive)
    return left + right


def fetch_prs(start_date: str, end_date: Optional[str]) -> List[Dict]:
    """Fetch PRs merged in the given date range."""
    start_day = datetime.strptime(start_date, "%Y-%m-%d").date()
    
    if end_date:
        end_day_exclusive = datetime.strptime(end_date, "%Y-%m-%d").date() + timedelta(days=1)
    else:
        end_day_exclusive = datetime.now(timezone.utc).date() + timedelta(days=1)
    
    return list_prs_in_range(start_day, end_day_exclusive)


def run_stage(stage_label: str, stage_func, current_json: List[Dict], pending_steps: List[str]) -> List[Dict]:
    """Execute a stage function with error handling."""
    try:
        return stage_func(current_json)
    except Exception as e:
        sys.stderr.write(f"error: failed during step: {stage_label}\n")
        if pending_steps:
            sys.stderr.write(f"missing requested steps: {stage_label} {' '.join(pending_steps)}\n")
        else:
            sys.stderr.write("missing requested steps: none\n")
        print(json.dumps(current_json))
        sys.exit(1)


def main():
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--start", type=str, help="Start merged date (inclusive)")
    parser.add_argument("--end", type=str, help="End merged date (inclusive, day granularity)")
    parser.add_argument("--input-json", type=str, help="Resume from an existing JSON file or stdin (-)")
    parser.add_argument("--attempts", action="store_true", help="Add number of attempts")
    parser.add_argument("--forced", action="store_true", help="Add force-merged field")
    parser.add_argument("--skipped", action="store_true", help="Add skipped tests field")
    parser.add_argument("--runtime", action="store_true", help="Add runtime field")
    parser.add_argument("--test-totals", action="store_true", help="Add test totals field")
    parser.add_argument("--author-times", action="store_true", help="Add author classification and PR time fields")
    parser.add_argument("--all", action="store_true", help="Add all fields")
    parser.add_argument("--pretty", action="store_true", help="Pretty-print the final JSON output")
    parser.add_argument("-h", "--help", action="store_true", help="Show help")
    
    args = parser.parse_args()
    
    if args.help or len(sys.argv) == 1:
        show_help()
        sys.exit(0 if args.help else 1)
    
    if args.start and args.input_json:
        sys.stderr.write("use either --start/--end or --input-json, not both\n")
        sys.exit(1)
    
    if not args.start and not args.input_json:
        sys.stderr.write("either --start or --input-json is required\n")
        sys.exit(1)
    
    # Load or fetch PRs
    if args.input_json:
        if args.input_json == "-":
            sys.stderr.write("Loading input JSON from stdin...\n")
            result = json.loads(sys.stdin.read())
        else:
            sys.stderr.write(f"Loading input JSON from {args.input_json}...\n")
            with open(args.input_json) as f:
                result = json.load(f)
        sys.stderr.write(f"Input PRs loaded: {len(result)}\n")
    else:
        sys.stderr.write(f"Fetching PRs merged between {args.start} and {args.end or 'now'}...\n")
        result = fetch_prs(args.start, args.end)
        sys.stderr.write(f"PRs fetched: {len(result)}\n")
    
    # Determine which stages to run
    stages = []
    if args.all:
        args.attempts = True
        args.forced = True
        args.skipped = True
        args.runtime = True
        args.test_totals = True
        args.author_times = True
    
    if args.attempts:
        stages.append(("attempts", get_num_attempts))
    if args.forced:
        stages.append(("forced", get_force_merged))
    if args.skipped:
        stages.append(("skipped", get_skipped_tests))
    if args.runtime:
        stages.append(("runtime", get_total_runtime))
    if args.test_totals:
        stages.append(("test-totals", get_total_tests_run))
    if args.author_times:
        stages.append(("author-times", get_author_and_times))
    
    # Run stages
    for i, (stage_name, stage_func) in enumerate(stages):
        pending = [s[0] for s in stages[i+1:]]
        
        if stage_name == "attempts":
            sys.stderr.write("Fetching number of attempts for each PR...\n")
        elif stage_name == "forced":
            sys.stderr.write("Determining whether each PR was force merged...\n")
        elif stage_name == "skipped":
            sys.stderr.write("Determining number of skipped tests for each PR...\n")
        elif stage_name == "runtime":
            sys.stderr.write("Calculating total runtime for each PR...\n")
        elif stage_name == "test-totals":
            sys.stderr.write("Calculating test totals for each PR...\n")
        elif stage_name == "author":
            sys.stderr.write("Classifying PR authors...\n")
        elif stage_name == "state-times":
            sys.stderr.write("Calculating PR state times...\n")
        elif stage_name == "author-times":
            sys.stderr.write("Classifying PR authors and calculating PR times...\n")
        
        result = run_stage(stage_name, stage_func, result, pending)
        sys.stderr.write("Done.\n")

    # Remove internal fields used to avoid extra API calls.
    for pr in result:
        pr.pop("_author_login", None)
        pr.pop("_comments", None)
        pr.pop("_reviews", None)
    
    # Output result
    if args.pretty:
        print(json.dumps(result, indent=2))
    else:
        print(json.dumps(result))


if __name__ == "__main__":
    main()
