#!/usr/bin/env python3

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple


APPROVALS_REQUIRED = 2
IN_REVIEW_LABEL = "In review"
WAITING_LABEL = "Waiting for review slot"
RUN_ONLY_ONE_SYSTEM_LABEL = "Run only one system"
COMMENT_MARKER = "<!-- pr-review-cap -->"


class GitHubError(RuntimeError):
    pass


@dataclass(frozen=True)
class Decision:
    admitted: bool
    outcome: str
    active_count: int
    blocking_prs: Tuple[int, ...] = ()


def effective_review_states(reviews: Iterable[Dict[str, Any]]) -> Dict[str, str]:
    states: Dict[str, str] = {}
    ordered = sorted(reviews, key=lambda review: review.get("submittedAt") or "")
    for review in ordered:
        author = (review.get("author") or {}).get("login")
        state = (review.get("state") or "").upper()
        if not author or state == "COMMENTED":
            continue
        if state in {"APPROVED", "CHANGES_REQUESTED", "DISMISSED"}:
            states[author] = state
    return states


def is_approved(reviews: Iterable[Dict[str, Any]], approvals_required: int = APPROVALS_REQUIRED) -> bool:
    states = effective_review_states(reviews)
    approvals = sum(state == "APPROVED" for state in states.values())
    blocking = any(state == "CHANGES_REQUESTED" for state in states.values())
    return approvals >= approvals_required and not blocking


def is_active_review(pr: Dict[str, Any]) -> bool:
    return not pr.get("isDraft", False) and not is_approved(pr.get("reviews") or [])


def decide_admission(
    candidate: Dict[str, Any],
    open_prs: Sequence[Dict[str, Any]],
    development_team_members: Dict[str, bool],
    development_team_limit: int,
) -> Decision:
    candidate_number = candidate["number"]
    author = candidate["author"]["login"]

    if is_approved(candidate.get("reviews") or []):
        return Decision(True, "already_approved", 0)

    active = [
        pr
        for pr in open_prs
        if pr["number"] != candidate_number and is_active_review(pr)
    ]

    if development_team_members[author]:
        counted = [
            pr
            for pr in active
            if development_team_members[pr["author"]["login"]]
        ]
        return Decision(
            admitted=len(counted) < development_team_limit,
            outcome=(
                "development_team_admitted"
                if len(counted) < development_team_limit
                else "development_team_cap_reached"
            ),
            active_count=len(counted),
            blocking_prs=tuple(pr["number"] for pr in counted),
        )

    counted = [pr for pr in active if pr["author"]["login"] == author]
    return Decision(
        admitted=not counted,
        outcome="external_admitted" if not counted else "external_author_cap_reached",
        active_count=len(counted),
        blocking_prs=tuple(pr["number"] for pr in counted),
    )


class GitHub:
    def __init__(self, repository: str, retries: int = 3):
        self.repository = repository
        self.retries = retries

    def run(self, *args: str, input_data: Optional[str] = None) -> str:
        delay = 2
        last_error = ""
        for attempt in range(1, self.retries + 1):
            process = subprocess.run(
                ["gh", *args],
                input=input_data,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
            if process.returncode == 0:
                return process.stdout
            last_error = process.stderr.strip()
            if attempt < self.retries:
                time.sleep(delay)
                delay *= 2
        raise GitHubError(f"gh {' '.join(args)} failed: {last_error}")

    def api_json(self, endpoint: str, *args: str) -> Any:
        output = self.run("api", endpoint, *args)
        return json.loads(output) if output.strip() else None

    def api_json_with_status(self, endpoint: str) -> Tuple[int, Any]:
        request = urllib.request.Request(
            f"https://api.github.com/{endpoint}",
            headers={
                "Accept": "application/vnd.github+json",
                "Authorization": f"Bearer {required_env('GH_TOKEN')}",
                "X-GitHub-Api-Version": "2022-11-28",
            },
        )
        delay = 2
        for attempt in range(1, self.retries + 1):
            try:
                with urllib.request.urlopen(request, timeout=30) as response:
                    return response.status, json.load(response)
            except urllib.error.HTTPError as error:
                if error.code in {403, 429, 500, 502, 503, 504} and attempt < self.retries:
                    time.sleep(delay)
                    delay *= 2
                    continue
                raw_body = error.read()
                body = json.loads(raw_body) if raw_body else None
                return error.code, body
            except urllib.error.URLError as error:
                if attempt == self.retries:
                    raise GitHubError(f"GET {endpoint} failed: {error}") from error
                time.sleep(delay)
                delay *= 2
        raise GitHubError(f"GET {endpoint} failed")

    def graphql(self, query: str, **variables: Any) -> Any:
        args = ["api", "graphql", "-f", f"query={query}"]
        for name, value in variables.items():
            args.extend(["-F", f"{name}={value}"])
        return json.loads(self.run(*args))


class ReviewCap:
    def __init__(self, github: GitHub, org: str, teams: Sequence[str], limit: int):
        self.github = github
        self.org = org
        self.teams = teams
        self.limit = limit
        self._membership: Dict[str, bool] = {}

    def ensure_labels(self) -> None:
        for name, color, description in (
            (IN_REVIEW_LABEL, "0e8a16", "Pull request is consuming an active review slot"),
            (WAITING_LABEL, "fbca04", "Pull request is waiting for an active review slot"),
        ):
            try:
                self.github.run(
                    "label",
                    "create",
                    name,
                    "--repo",
                    self.github.repository,
                    "--color",
                    color,
                    "--description",
                    description,
                )
            except GitHubError as error:
                if "already exists" not in str(error).lower():
                    raise

    def team_member(self, login: str) -> bool:
        if login in self._membership:
            return self._membership[login]

        for team in self.teams:
            endpoint = f"orgs/{self.org}/teams/{team}/memberships/{login}"
            status, membership = self.github.api_json_with_status(endpoint)
            if status == 404:
                continue
            if status != 200:
                raise GitHubError(
                    f"cannot check membership of {login} in {self.org}/{team}: HTTP {status}"
                )
            if membership.get("state") == "active":
                self._membership[login] = True
                return True

        self._membership[login] = False
        return False

    def open_prs(self) -> List[Dict[str, Any]]:
        owner, name = self.github.repository.split("/", 1)
        query = """
query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(states: OPEN, first: 100, after: $cursor, orderBy: {field: CREATED_AT, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number
        isDraft
        author { login }
        labels(first: 100) { nodes { name } }
        reviews(first: 100) {
          pageInfo { hasNextPage }
          nodes { author { login } state submittedAt }
        }
      }
    }
  }
}
"""
        prs: List[Dict[str, Any]] = []
        cursor: Optional[str] = None
        while True:
            variables = {"owner": owner, "name": name}
            if cursor:
                variables["cursor"] = cursor
            data = self.github.graphql(query, **variables)["data"]["repository"]["pullRequests"]
            for pr in data["nodes"]:
                if pr["reviews"]["pageInfo"]["hasNextPage"]:
                    pr["reviews"]["nodes"] = self.pr_reviews(pr["number"])
                pr["reviews"] = pr["reviews"]["nodes"]
                pr["labels"] = {label["name"] for label in pr["labels"]["nodes"]}
                prs.append(pr)
            if not data["pageInfo"]["hasNextPage"]:
                return prs
            cursor = data["pageInfo"]["endCursor"]

    def pr_reviews(self, number: int) -> List[Dict[str, Any]]:
        owner, name = self.github.repository.split("/", 1)
        query = """
query($owner: String!, $name: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviews(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes { author { login } state submittedAt }
      }
    }
  }
}
"""
        reviews: List[Dict[str, Any]] = []
        cursor: Optional[str] = None
        while True:
            variables = {"owner": owner, "name": name, "number": number}
            if cursor:
                variables["cursor"] = cursor
            data = self.github.graphql(query, **variables)["data"]["repository"]["pullRequest"]["reviews"]
            reviews.extend(data["nodes"])
            if not data["pageInfo"]["hasNextPage"]:
                return reviews
            cursor = data["pageInfo"]["endCursor"]

    def set_labels(self, number: int, add: Sequence[str] = (), remove: Sequence[str] = ()) -> None:
        for label in add:
            self.github.run("pr", "edit", str(number), "--repo", self.github.repository, "--add-label", label)
        for label in remove:
            try:
                self.github.run("pr", "edit", str(number), "--repo", self.github.repository, "--remove-label", label)
            except GitHubError as error:
                if "not found" not in str(error).lower():
                    raise

    def convert_to_draft(self, number: int) -> None:
        self.github.run("pr", "ready", str(number), "--repo", self.github.repository, "--undo")

    def comment(self, number: int, body: str) -> None:
        comments = self.github.api_json(
            f"repos/{self.github.repository}/issues/{number}/comments?per_page=100",
            "--paginate",
            "--slurp",
        )
        existing = next(
            (
                comment
                for page in comments
                for comment in page
                if COMMENT_MARKER in (comment.get("body") or "")
            ),
            None,
        )
        if existing:
            self.github.run(
                "api",
                f"repos/{self.github.repository}/issues/comments/{existing['id']}",
                "--method",
                "PATCH",
                "-f",
                f"body={body}",
            )
        else:
            self.github.run(
                "api",
                f"repos/{self.github.repository}/issues/{number}/comments",
                "--method",
                "POST",
                "-f",
                f"body={body}",
            )

    def reconcile(self, prs: Sequence[Dict[str, Any]]) -> None:
        for pr in prs:
            number = pr["number"]
            labels = pr.get("labels", set())
            if pr["isDraft"] or is_approved(pr["reviews"]):
                if IN_REVIEW_LABEL in labels:
                    self.set_labels(number, remove=(IN_REVIEW_LABEL,))
            elif IN_REVIEW_LABEL not in labels and WAITING_LABEL not in labels:
                self.set_labels(number, add=(IN_REVIEW_LABEL,))

    def admit(self, candidate_number: int, prs: Sequence[Dict[str, Any]]) -> Decision:
        candidate = next((pr for pr in prs if pr["number"] == candidate_number), None)
        if candidate is None:
            raise GitHubError(f"PR #{candidate_number} is not open")
        if candidate["isDraft"]:
            return Decision(False, "already_draft", 0)

        logins = {pr["author"]["login"] for pr in prs if is_active_review(pr)}
        members = {login: self.team_member(login) for login in logins}
        decision = decide_admission(candidate, prs, members, self.limit)

        if decision.admitted:
            if decision.outcome == "already_approved":
                self.set_labels(candidate_number, remove=(IN_REVIEW_LABEL, WAITING_LABEL))
            else:
                self.set_labels(candidate_number, add=(IN_REVIEW_LABEL,), remove=(WAITING_LABEL,))
            return decision

        self.convert_to_draft(candidate_number)
        self.set_labels(
            candidate_number,
            add=(WAITING_LABEL, RUN_ONLY_ONE_SYSTEM_LABEL),
            remove=(IN_REVIEW_LABEL,),
        )
        if decision.outcome == "development_team_cap_reached":
            message = (
                f"{COMMENT_MARKER}\nThis pull request was returned to draft because the shared "
                f"active-review pool for the development team is full. There are currently "
                f"{decision.active_count} pull requests awaiting approval and the configured "
                f"limit is {self.limit}. Pull requests with two effective approvals and no "
                "outstanding changes request do not consume a review slot. Mark this pull "
                "request ready for review again after a slot becomes available."
            )
        else:
            links = ", ".join(f"#{number}" for number in decision.blocking_prs)
            message = (
                f"{COMMENT_MARKER}\nThis pull request was returned to draft because external "
                "contributors may have one pull request awaiting approval at a time. Your "
                f"active-review pull request is {links}. Pull requests with two effective "
                "approvals and no outstanding changes request do not consume a review slot. "
                "Mark this pull request ready for review again after that pull request is "
                "approved, merged, closed, or converted to draft."
            )
        self.comment(candidate_number, message)
        return decision


def required_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise ValueError(f"{name} is not configured")
    return value


def main() -> int:
    try:
        required_env("GH_TOKEN")
        repository = required_env("GH_REPO")
        org = os.environ.get("PR_REVIEW_ORG", "canonical")
        teams = [
            team.strip()
            for team in os.environ.get(
                "PR_REVIEW_DEVELOPMENT_TEAMS", "snapd,ubuntu-core-devs"
            ).split(",")
            if team.strip()
        ]
        limit = int(required_env("PR_REVIEW_DEVELOPMENT_TEAM_LIMIT"))
        if limit < 1:
            raise ValueError("PR_REVIEW_DEVELOPMENT_TEAM_LIMIT must be positive")

        review_cap = ReviewCap(GitHub(repository), org, teams, limit)
        review_cap.ensure_labels()
        prs = review_cap.open_prs()
        event_name = os.environ.get("EVENT_NAME", "")
        event_action = os.environ.get("EVENT_ACTION", "")
        pr_number = os.environ.get("PR_NUMBER", "")

        admission_event = event_name == "pull_request_target" and event_action in {
            "opened",
            "ready_for_review",
            "reopened",
        }
        if admission_event and pr_number:
            decision = review_cap.admit(int(pr_number), prs)
            print(json.dumps(decision.__dict__))
            output = os.environ.get("GITHUB_OUTPUT")
            if output:
                with open(output, "a", encoding="utf-8") as stream:
                    admitted = decision.admitted and decision.outcome not in {
                        "already_approved",
                        "already_draft",
                    }
                    stream.write(f"admitted={'true' if admitted else 'false'}\n")
                    stream.write(f"outcome={decision.outcome}\n")
        else:
            review_cap.reconcile(prs)
        return 0
    except (GitHubError, KeyError, TypeError, ValueError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
