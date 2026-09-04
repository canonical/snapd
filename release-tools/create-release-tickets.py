#!/usr/bin/env python3
"""Create the SNAPDENG Jira epic and tasks for a snapd release.

Dry-run is the default. Pass --apply to create or update issues on
warthogs.atlassian.net using JIRA_EMAIL and a classic unscoped JIRA_API_TOKEN.

Version X.YY is a major release; X.YY.Z is a bugfix, or a security release
with --security. Jira Fix Version is always "snapd X.YY". Ubuntu devel and
LTS series default from ubuntu-distro-info.

Created issues are labeled snapd-release-tickets. --apply creates the epic
and tasks when none exist for this version and variant. If the matching epic
already exists, --apply lists it and does not create or edit issues; pass
--force to create missing tasks and rewrite all fields on the script-generated
epic and labeled tasks. Unlabeled tasks with a matching title are listed as
placeholders but never modified.

The epic and non-cross-distro tasks use team SnapD EMEA unless --team is
set to a short name (AMER, Cross-distro) or the full Jira team name.
Cross-distro tasks always use SnapD Cross-distro.
"""

# Hyphenated filename; this is a script, not a library.
# pylint: disable=invalid-name,missing-function-docstring
# pylint: disable=too-many-arguments,too-many-positional-arguments,too-many-locals
# pylint: disable=too-many-lines,too-many-statements,import-outside-toplevel

import argparse
import inspect
import os
import re
import subprocess
import sys
import uuid
from typing import NamedTuple

DEFAULT_JIRA_URL = "https://warthogs.atlassian.net"
DEFAULT_PROJECT = "SNAPDENG"
DEFAULT_PARENT_EPIC = "SNAPDENG-34819"
REQUEST_TIMEOUT_SECONDS = 30
SEARCH_PAGE_SIZE = 100
# Bound for searches that must be complete, such as the children of an epic.
MAX_SEARCH_ISSUES = 1000

RELEASE_MD_URL = "https://github.com/canonical/snapd/blob/master/RELEASE.md"
SRU_PACKAGE_NOTES_URL = (
    "https://ubuntu.com/project/docs/SRU/reference/package-specific/#snapd"
)
SRU_SNAPD_UPDATES_URL = (
    "https://documentation.ubuntu.com/project/SRU/reference/exception-Snapd-Updates/"
)

VARIANT_LABELS = {
    "major": "Major",
    "bugfix": "Bugfix",
    "security": "Security",
}

VERSION_MAJOR_RE = re.compile(r"^\d+\.\d+$")
VERSION_PATCH_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)$")
ISSUE_KEY_RE = re.compile(r"^([A-Z][A-Z0-9]+-\d+)\s+")

UNEXPECTED_LABEL = "not-in-release-plan"
UNEXPECTED_PREFIX = "UNEXPECTED: "
# Marks issues this script created so --force never rewrites manual tickets.
GENERATED_LABEL = "snapd-release-tickets"
SECURITY_TITLE_PREFIX = "Security: "
TARGET_STATUS = "Triaged"

DEFAULT_TEAM = "SnapD EMEA"
CROSS_DISTRO_TEAM = "SnapD Cross-distro"
AMER_TEAM = "SnapD AMER"
# --team accepts a short name or the full Jira team name. Cross-distro
# tasks ignore --team and always use CROSS_DISTRO_TEAM.
TEAM_NAMES = {
    "EMEA": DEFAULT_TEAM,
    "AMER": AMER_TEAM,
    "Cross-distro": CROSS_DISTRO_TEAM,
}
TEAM_FIELD_NAME = "Team"
STORY_POINTS_FIELD_NAME = "Story Points"

PROG_NAME = "create-release-tickets.py"
JIRA_API_TOKEN_URL = "https://id.atlassian.com/manage-profile/security/api-tokens"
DISTRO_SERIES_ERROR = (
    "cannot determine Ubuntu series, "
    "install distro-info or pass --dev-target and --lts-targets"
)


class UsageError(Exception):
    """Invalid CLI usage; main() prints this and exits 2."""


class TaskSpec(NamedTuple):
    """One planned Jira task: summary, Acceptance Criteria, points, and team kind."""

    summary: str
    checklist: tuple
    points: int
    cross_distro: bool = False


class PlannedRelease(NamedTuple):
    """Epic and tasks ready for dry-run printing or Jira apply."""

    variant: str
    version: str
    jira_version: str
    parent_epic: str
    epic_summary: str
    tasks: tuple
    team: str


class ApplyResult(NamedTuple):
    """Issue keys created or reused by apply_plan."""

    epic_key: str
    tasks: tuple
    created_keys: tuple
    unexpected: tuple
    epic_created: bool
    updated: bool = False


def new_local_id():
    return str(uuid.uuid4())


def parse_team(raw):
    """Map a --team short or full name to its Jira team, or DEFAULT_TEAM when empty."""
    if not raw:
        return DEFAULT_TEAM
    for short_name, team in TEAM_NAMES.items():
        if raw.lower() in (short_name.lower(), team.lower()):
            return team
    allowed = ", ".join(f'"{name}"' for name in TEAM_NAMES)
    raise UsageError(f'invalid --team "{raw}", expected one of {allowed}')


def task_team(plan, task):
    """Team name for a task: Cross-distro tasks ignore plan.team."""
    if task.cross_distro:
        return CROSS_DISTRO_TEAM
    return plan.team


def parse_targets(raw, devel):
    targets = [part.strip() for part in raw.split(",") if part.strip()]
    if not targets:
        raise RuntimeError(
            "cannot parse --lts-targets, expected a comma-separated list of series"
        )
    if devel in targets:
        raise RuntimeError(
            f'cannot list development series "{devel}" in --lts-targets, '
            f"it already gets its own {devel}-proposed task"
        )
    return targets


def ubuntu_distro_info(args):
    return subprocess.check_output(["ubuntu-distro-info"] + list(args), text=True)


def default_ubuntu_series():
    """Return (devel, supported-minus-devel) from ubuntu-distro-info."""
    try:
        devel = ubuntu_distro_info(["--devel", "-c"]).strip()
        supported = ubuntu_distro_info(["--supported", "-c"]).split()
    except (OSError, subprocess.CalledProcessError) as err:
        raise RuntimeError(DISTRO_SERIES_ERROR) from err
    if not devel:
        raise RuntimeError(DISTRO_SERIES_ERROR)
    targets = [name for name in supported if name and name != devel]
    if not targets:
        raise RuntimeError(DISTRO_SERIES_ERROR)
    return devel, targets


def resolve_ubuntu_series(devel, targets_raw):
    """Resolve devel and SRU series from flags, filling gaps via distro-info."""
    if devel and targets_raw:
        return devel, parse_targets(targets_raw, devel)
    auto_devel, auto_targets = default_ubuntu_series()
    if not devel:
        devel = auto_devel
    if targets_raw:
        return devel, parse_targets(targets_raw, devel)
    targets = [name for name in auto_targets if name != devel]
    if not targets:
        raise RuntimeError(DISTRO_SERIES_ERROR)
    return devel, targets


def targets_proposed_label(targets):
    return "{" + ",".join(targets) + "}-proposed"


def variant_from_version(version, security=False):
    """Map X.YY to major and X.YY.Z to bugfix, or security with --security."""
    if VERSION_MAJOR_RE.match(version):
        if security:
            raise RuntimeError(
                f'cannot use --security with version "{version}", expected X.YY.Z'
            )
        return "major"
    if VERSION_PATCH_RE.match(version):
        if security:
            return "security"
        return "bugfix"
    raise RuntimeError(f'cannot use version "{version}", expected X.YY or X.YY.Z')


def jira_version_for(variant, version):
    """Return the X.YY used in Fix Version "snapd X.YY"."""
    if variant == "major":
        if not VERSION_MAJOR_RE.match(version):
            raise RuntimeError(
                f'cannot use version "{version}" with variant "major", expected X.YY'
            )
        return version
    if variant not in VARIANT_LABELS:
        raise RuntimeError(f'cannot use unknown variant "{variant}"')
    match = VERSION_PATCH_RE.match(version)
    if not match:
        raise RuntimeError(
            f'cannot use version "{version}" with variant "{variant}", expected X.YY.Z'
        )
    return f"{match.group(1)}.{match.group(2)}"


def epic_summary_for(variant, version):
    return f"Snapd {VARIANT_LABELS[variant]} Release {version}"


def task_cut_release(*, version, variant):
    branch = jira_version_for(variant, version)
    if variant == "major":
        branch_step = f"Create the release/{branch} branch from master"
    else:
        branch_step = (
            f"Confirm the release/{branch} branch exists "
            "(patch releases do not get a new branch)"
        )
    return TaskSpec(
        summary=f"Cut release {version}",
        checklist=(
            branch_step,
            "Create the Launchpad SRU tracking bug "
            "(unless this version supersedes an unreleased version)",
            "Curate NEWS.md",
            "Fill the SRU test template on each milestone bug",
            "Generate changelogs with release-tools/changelog.py",
            "Open the release PR",
            "Analyse test results and merge release PR",
        ),
        points=3,
    )


def task_beta_snaps():
    return TaskSpec(
        summary="Build and upload BETA snapd snaps",
        checklist=(
            "Push the version git tag",
            "Open and merge the changelog PR against master",
            "Build snapd snaps on Launchpad (including FIPS)",
            "Verify the FIPS snap loads the FIPS provider on a FIPS-enabled system",
            "Promote revisions to latest/beta",
            "Promote FIPS revisions to fips-updates/beta",
            "Close the GitHub milestone for this release",
            "Notify snapd QA and the certification tester",
            "Update the snapd roadmap for beta",
        ),
        points=2,
    )


def task_debs():
    return TaskSpec(
        summary="Build and upload snapd debs",
        checklist=(
            "Build snapd debs for each target series",
            "Create source tarballs with release-tools/repack-debian-tarball.sh",
            "Create a draft GitHub release and upload tarballs",
            "Upload to a test PPA and run autopkgtests",
            "Upload to ppa:snappy-dev/image",
            "Set the SRU bug series status to in progress",
        ),
        points=2,
    )


def task_qa_beta():
    return TaskSpec(
        summary="Complete Snapd Team QA BETA validation",
        checklist=(
            "Coordinate snapd team QA beta validation",
            "Review known issues from beta testing",
            "Obtain QA sign-off to promote to candidate",
            "Confirm the certification team beta sign-off",
            "Promote snapd snap revisions to candidate",
            "Update the snapd roadmap for candidate",
            "Create the release forum post and publicize the move to candidate",
            "Publish the GitHub release created when uploading tarballs",
        ),
        points=3,
    )


def task_certification_beta():
    return TaskSpec(
        summary="Monitor Certification Team BETA validation",
        checklist=(
            "Contact the assigned certification tester",
            "Track certification beta results",
            "Obtain certification sign-off",
        ),
        points=1,
    )


def task_devel_upload(*, devel):
    return TaskSpec(
        summary=(
            f"Request upload to {devel}-proposed and facilitate LP bug verification"
        ),
        checklist=(
            f"Request sponsorship for upload to {devel}-proposed",
            f"Ensure autopkgtests pass on {devel}-proposed",
            (
                f"Request snapd QA tests on {devel}-proposed debs "
                "and record the result on the SRU bug"
            ),
            f"Verify Launchpad bugs for {devel}",
        ),
        points=2,
    )


def task_sru_upload(*, targets):
    targets_proposed = targets_proposed_label(targets)
    return TaskSpec(
        summary=(
            f"Request upload to {targets_proposed} and facilitate LP bug verification"
        ),
        checklist=(
            f"Request sponsorship for upload to {targets_proposed}",
            f"Ensure autopkgtests pass on {targets_proposed}",
            (
                f"Request snapd QA tests on {targets_proposed} debs "
                "and record the result on the SRU bug"
            ),
            "Perform distro-upgrade testing",
            "Verify Launchpad bugs for each target series",
            (
                "Confirm the candidate soak week, the WSL smoke tests, "
                "and that no stable-stopper bugs are open"
            ),
            "Request progressive stable promotion of the snapd snap",
            "Update the snapd roadmap once the progressive release completes",
            "Request the move to -updates",
            "Mark milestone bugs as Fix released",
        ),
        points=2,
    )


def task_cross_distro_arch_opensuse_amazon():
    return TaskSpec(
        summary="Cross-distro: releases Arch, openSUSE & Amazon",
        checklist=(
            "Confirm snapd is in candidate and the GitHub release is public",
            "Run WSL smoke tests on candidate",
            "Complete the Arch Linux release",
            "Complete the openSUSE release",
            "Complete the Amazon Linux release",
        ),
        points=3,
        cross_distro=True,
    )


def task_cross_distro_fedora_debian():
    return TaskSpec(
        summary="Cross-distro: Fedora and Debian",
        checklist=(
            "Complete the Fedora release",
            "Complete the Debian release",
        ),
        points=3,
        cross_distro=True,
    )


# Epic checklist order. Builders take any of the TASK_CONTEXT_KEYS.
TASKS = (
    task_cut_release,
    task_beta_snaps,
    task_debs,
    task_qa_beta,
    task_certification_beta,
    task_devel_upload,
    task_sru_upload,
    task_cross_distro_arch_opensuse_amazon,
    task_cross_distro_fedora_debian,
)

TASK_CONTEXT_KEYS = ("version", "variant", "devel", "targets")


def call_task(fn, ctx):
    """Call a task builder with only the ctx keys its signature accepts."""
    names = inspect.signature(fn).parameters
    unknown = [name for name in names if name not in TASK_CONTEXT_KEYS]
    if unknown:
        raise RuntimeError(f"cannot fill {fn.__name__}, unknown input {unknown[0]}")
    return fn(**{name: ctx[name] for name in names})


def task_summary_for(variant, summary):
    """Prefix security-release task titles; checklists stay unprefixed."""
    if variant == "security":
        return SECURITY_TITLE_PREFIX + summary
    return summary


def build_plan(
    variant,
    version,
    devel,
    targets,
    jira_version=None,
    parent_epic=None,
    team=None,
):
    """Build the epic summary, Fix Version, and nine TaskSpecs for a release."""
    derived_version = jira_version_for(variant, version)
    if jira_version is None:
        jira_version = f"snapd {derived_version}"
    if parent_epic is None:
        parent_epic = DEFAULT_PARENT_EPIC
    if team is None:
        team = DEFAULT_TEAM
    ctx = {
        "version": version,
        "variant": variant,
        "devel": devel,
        "targets": targets,
    }
    tasks = tuple(
        spec._replace(summary=task_summary_for(variant, spec.summary))
        for spec in (call_task(fn, ctx) for fn in TASKS)
    )
    return PlannedRelease(
        variant=variant,
        version=version,
        jira_version=jira_version,
        parent_epic=parent_epic,
        epic_summary=epic_summary_for(variant, version),
        tasks=tasks,
        team=team,
    )


def adf_text(text, href=None):
    node = {"type": "text", "text": text}
    if href is not None:
        node["marks"] = [{"type": "link", "attrs": {"href": href}}]
    return node


def adf_paragraph(*content):
    return {"type": "paragraph", "content": list(content)}


def adf_bullet_list(items):
    return {
        "type": "bulletList",
        "content": [
            {
                "type": "listItem",
                "content": [adf_paragraph(*item)],
            }
            for item in items
        ],
    }


def adf_task_list(items):
    content = []
    for item in items:
        if isinstance(item, tuple):
            text, state = item
        else:
            text, state = item, "TODO"
        content.append(
            {
                "type": "taskItem",
                "attrs": {"localId": new_local_id(), "state": state},
                "content": [{"type": "text", "text": text}],
            }
        )
    return {
        "type": "taskList",
        "attrs": {"localId": new_local_id()},
        "content": content,
    }


def adf_doc(*blocks):
    return {"type": "doc", "version": 1, "content": list(blocks)}


# Process links shared by every issue Description (ADF for Jira, text for dry-run).
INSTRUCTION_HEADING = "Follow the snapd release process:"
INSTRUCTION_BULLETS = (
    (("How to release snapd", RELEASE_MD_URL),),
    (
        ("Snapd SRU special case: ", None),
        ("package-specific notes", SRU_PACKAGE_NOTES_URL),
        (" and ", None),
        ("Snapd Updates", SRU_SNAPD_UPDATES_URL),
    ),
)


def instruction_preview_lines():
    lines = [INSTRUCTION_HEADING]
    for bullet in INSTRUCTION_BULLETS:
        if len(bullet) == 1 and bullet[0][1] is not None:
            text, href = bullet[0]
            lines.append(f"- {text}: {href}")
            continue
        parts = []
        for text, href in bullet:
            if href is None:
                parts.append(text)
            else:
                parts.append(f"{text} ({href})")
        lines.append("- " + "".join(parts))
    return tuple(lines)


def instruction_blocks():
    return (
        adf_paragraph(adf_text(INSTRUCTION_HEADING)),
        adf_bullet_list(
            [
                [adf_text(text, href=href) for text, href in bullet]
                for bullet in INSTRUCTION_BULLETS
            ]
        ),
    )


def issue_key_from_progress_text(text):
    body = text.removeprefix(UNEXPECTED_PREFIX)
    match = ISSUE_KEY_RE.match(body)
    if match is None:
        return None
    return match.group(1)


def task_items_with_state(adf):
    items = []

    def walk(node):
        if not isinstance(node, dict):
            return
        if node.get("type") == "taskItem":
            text = "".join(child.get("text", "") for child in node.get("content", []))
            state = node.get("attrs", {}).get("state", "TODO")
            items.append((text, state))
            return
        for child in node.get("content", []):
            walk(child)

    walk(adf)
    return items


def previous_task_states(adf):
    """Map existing checklist items by full text and by leading issue key."""
    by_text = {}
    by_key = {}
    for text, state in task_items_with_state(adf):
        by_text[text] = state
        key = issue_key_from_progress_text(text)
        if key is not None:
            by_key[key] = state
    return by_text, by_key


def item_state(text, by_text, by_key):
    if text in by_text:
        return by_text[text]
    key = issue_key_from_progress_text(text)
    if key is not None and key in by_key:
        return by_key[key]
    return "TODO"


def progress_item(key, summary):
    return f"{key} {summary}"


def unexpected_progress_item(key, summary):
    return f"{UNEXPECTED_PREFIX}{key} {summary}"


def issue_description():
    return adf_doc(*instruction_blocks())


def acceptance_criteria(items, unexpected_items=(), previous_adf=None):
    """ADF task list, copying DONE/TODO from previous_adf when item text matches."""
    by_text, by_key = {}, {}
    if previous_adf is not None:
        by_text, by_key = previous_task_states(previous_adf)

    def with_state(text):
        return (text, item_state(text, by_text, by_key))

    blocks = [adf_task_list([with_state(item) for item in items])]
    if unexpected_items:
        blocks.extend(
            [
                adf_paragraph(
                    adf_text(
                        "These issues are children of the epic but are not part of "
                        "the standard release tasks."
                    )
                ),
                adf_task_list([with_state(item) for item in unexpected_items]),
            ]
        )
    return adf_doc(*blocks)


def checklist_adf_from_issue(issue, acceptance_field):
    fields = issue.get("fields") or {}
    adf = fields.get(acceptance_field)
    if adf:
        return adf
    return fields.get("description")


def task_item_texts(adf):
    """ADF task-item texts; used by tests."""
    return [text for text, _state in task_items_with_state(adf)]


def linked_hrefs(adf):
    """Link hrefs in an ADF document; used by tests."""
    hrefs = []

    def walk(node):
        if not isinstance(node, dict):
            return
        for mark in node.get("marks", []):
            if mark.get("type") == "link":
                hrefs.append(mark["attrs"]["href"])
        for child in node.get("content", []):
            walk(child)

    walk(adf)
    return hrefs


def epic_fields(
    project, plan, acceptance_field, description=None, team_field=None, team_id=None
):
    """Create payload for the release epic, including team when resolved."""
    if description is None:
        description = issue_description()
    fields = {
        "project": {"key": project},
        "issuetype": {"name": "Epic"},
        "summary": plan.epic_summary,
        "description": description,
        "fixVersions": [{"name": plan.jira_version}],
        "parent": {"key": plan.parent_epic},
        "labels": [GENERATED_LABEL],
        acceptance_field: acceptance_criteria([task.summary for task in plan.tasks]),
    }
    epic_name_field = os.environ.get("JIRA_EPIC_NAME_FIELD", "")
    if epic_name_field:
        fields[epic_name_field] = plan.epic_summary
    if team_field and team_id:
        fields[team_field] = team_id
    return fields


def task_fields(
    project,
    plan,
    task,
    parent_key,
    acceptance_field,
    team_field=None,
    team_id=None,
    story_points_field=None,
):
    """Create payload for one release task, including team and points when resolved."""
    fields = {
        "project": {"key": project},
        "issuetype": {"name": "Task"},
        "summary": task.summary,
        "description": issue_description(),
        "fixVersions": [{"name": plan.jira_version}],
        "labels": [GENERATED_LABEL],
        acceptance_field: acceptance_criteria(task.checklist),
    }
    fields.update(parent_fields(parent_key))
    if team_field and team_id:
        fields[team_field] = team_id
    if story_points_field:
        fields[story_points_field] = task.points
    return fields


def parent_fields(parent_key):
    fields = {"parent": {"key": parent_key}}
    epic_link_field = os.environ.get("JIRA_EPIC_LINK_FIELD", "")
    if epic_link_field:
        fields[epic_link_field] = parent_key
    return fields


def issue_parent_key(issue):
    parent = issue.get("fields", {}).get("parent") or {}
    key = parent.get("key")
    if key:
        return key
    epic_link_field = os.environ.get("JIRA_EPIC_LINK_FIELD", "")
    if epic_link_field:
        link = issue.get("fields", {}).get(epic_link_field)
        if isinstance(link, dict):
            return link.get("key")
        if isinstance(link, str) and link:
            return link
    return None


def jql_string(value):
    """Quote a value as a JQL string literal."""
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def connect_jira(url, email, token):
    """Return a Jira Cloud v3 client (email + API token as Basic auth)."""
    try:
        from atlassian.jira import JiraCloud
    except ImportError as err:
        raise RuntimeError(
            "cannot import Jira Cloud client, please install atlassian-python-api"
        ) from err
    return JiraCloud(
        url,
        username=email,
        password=token,
        timeout=REQUEST_TIMEOUT_SECONDS,
        api_version=3,
    )


def jira_io_errors():
    """HTTP and network exceptions raised by JiraCloud (requests)."""
    errors = [TimeoutError]
    try:
        from requests.exceptions import RequestException
    except ImportError:
        return tuple(errors)
    return (TimeoutError, RequestException)


def jira_http_error():
    """requests.HTTPError, if requests is installed."""
    try:
        from requests import HTTPError

        return HTTPError
    except ImportError:
        return type("HTTPError", (Exception,), {})


def jira_error_message(err):
    """One-line CLI text for a Jira HTTP or network failure."""
    response = getattr(err, "response", None)
    status = getattr(response, "status_code", None)
    if status is not None:
        text = getattr(response, "text", "") or str(err)
        return f"cannot talk to Jira: HTTP {status}: {text}"
    timeout_types = [TimeoutError]
    try:
        from requests.exceptions import Timeout

        timeout_types.append(Timeout)
    except ImportError:
        pass
    if isinstance(err, tuple(timeout_types)):
        return f"cannot talk to Jira: timed out after {REQUEST_TIMEOUT_SECONDS}s"
    return f"cannot talk to Jira: {err}"


def create_issue(jira, fields):
    return jira.create_issue(data={"fields": fields})


def edit_issue(jira, key, fields):
    return jira.edit_issue(key, data={"fields": fields})


def search_jql(jira, jql, fields, limit=50):
    """Search, following nextPageToken until limit issues are collected."""
    issues = []
    token = None
    while len(issues) < limit:
        body = {
            "jql": jql,
            "maxResults": min(limit - len(issues), SEARCH_PAGE_SIZE),
            "fields": list(fields),
        }
        if token is not None:
            body["nextPageToken"] = token
        payload = jira.search_and_reconsile_issues_using_jql_post(data=body) or {}
        issues.extend(payload.get("issues") or [])
        token = payload.get("nextPageToken")
        if not token:
            break
    return issues[:limit]


def version_create_body(project, name):
    """Build the version create payload; the API wants a numeric project id."""
    try:
        project_id = int(project["id"])
    except (KeyError, TypeError, ValueError) as err:
        raise RuntimeError(
            f"cannot find Jira project id for {DEFAULT_PROJECT}"
        ) from err
    return {"name": name, "projectId": project_id}


def create_project_version(jira, name):
    project = jira.get_project(DEFAULT_PROJECT) or {}
    return jira.create_version(data=version_create_body(project, name))


def find_epic(jira, summary):
    # Epic summaries are built from fixed words and a validated version,
    # so they carry no characters the text search would treat specially.
    issues = search_jql(
        jira,
        f"project = {DEFAULT_PROJECT} AND issuetype = Epic "
        f"AND summary ~ {jql_string(summary)}",
        fields=["summary", "description"],
    )
    for issue in issues:
        if issue.get("fields", {}).get("summary") == summary:
            return issue
    return None


def list_children(jira, epic_key, fields=None):
    if fields is None:
        fields = ["summary", "labels", "parent"]
    return search_jql(
        jira,
        f"parent = {epic_key} ORDER BY created ASC",
        fields=fields,
        limit=MAX_SEARCH_ISSUES,
    )


def find_generated_issues(jira, fix_version, fields=None):
    """Return the tasks this script created for a Fix Version.

    Summaries are matched by the caller: label and Fix Version are
    exact-match fields, so the query needs no text search.
    """
    if fields is None:
        fields = ["summary", "labels", "parent"]
    return search_jql(
        jira,
        f"project = {DEFAULT_PROJECT} AND issuetype != Epic "
        f"AND labels = {jql_string(GENERATED_LABEL)} "
        f"AND fixVersion = {jql_string(fix_version)}",
        fields=fields,
        limit=MAX_SEARCH_ISSUES,
    )


def ensure_parent(jira, key, parent_key):
    issue = jira.get_issue(key, fields="parent")
    if issue_parent_key(issue) == parent_key:
        return
    edit_issue(jira, key, parent_fields(parent_key))


def ensure_fix_versions(jira, key, version_name):
    issue = jira.get_issue(key, fields="fixVersions")
    names = {
        item.get("name")
        for item in issue.get("fields", {}).get("fixVersions") or []
        if isinstance(item, dict)
    }
    if version_name in names and len(names) == 1:
        return
    edit_issue(jira, key, {"fixVersions": [{"name": version_name}]})


def ensure_label(jira, key, label, present):
    issue = jira.get_issue(key, fields="labels")
    labels = list(issue.get("fields", {}).get("labels") or [])
    has_label = label in labels
    if present and not has_label:
        labels.append(label)
        edit_issue(jira, key, {"labels": labels})
    elif not present and has_label:
        labels = [item for item in labels if item != label]
        edit_issue(jira, key, {"labels": labels})


def credentials():
    """Read JIRA_EMAIL and JIRA_API_TOKEN from the environment."""
    email = os.environ.get("JIRA_EMAIL", "")
    token = os.environ.get("JIRA_API_TOKEN", "")
    if not email or not token:
        raise RuntimeError(
            "cannot find Jira credentials, please set JIRA_EMAIL and JIRA_API_TOKEN"
        )
    return email, token


ACCEPTANCE_CRITERIA_FIELD_NAME = "Acceptance Criteria"


def jira_field_id(jira, field_name, env_var):
    """Resolve a Jira custom field id by name, or from env_var."""
    override = os.environ.get(env_var, "").strip()
    if override:
        return override
    matches = [
        field["id"]
        for field in jira.get_fields() or []
        if field.get("name") == field_name
    ]
    if not matches:
        raise RuntimeError(
            f'cannot find Jira field "{field_name}"; '
            f"set {env_var} to the customfield id"
        )
    return matches[0]


def acceptance_criteria_field_id(jira):
    """Resolve Acceptance Criteria field id, or JIRA_ACCEPTANCE_CRITERIA_FIELD."""
    return jira_field_id(
        jira, ACCEPTANCE_CRITERIA_FIELD_NAME, "JIRA_ACCEPTANCE_CRITERIA_FIELD"
    )


def team_field_id(jira):
    """Resolve the Team custom field id (or JIRA_TEAM_FIELD)."""
    return jira_field_id(jira, TEAM_FIELD_NAME, "JIRA_TEAM_FIELD")


def story_points_field_id(jira):
    """Resolve the Story Points field id (or JIRA_STORY_POINTS_FIELD)."""
    return jira_field_id(jira, STORY_POINTS_FIELD_NAME, "JIRA_STORY_POINTS_FIELD")


def _plain_suggestion_text(value):
    return re.sub(r"<[^>]+>", "", value or "").strip()


def team_id_from_suggestions(payload, team_name):
    for item in payload.get("results") or []:
        display = _plain_suggestion_text(item.get("displayName") or "")
        value = (item.get("value") or "").strip().strip('"')
        if value and display == team_name:
            return value
    return None


def issue_team_id(issue, team_field):
    value = (issue.get("fields") or {}).get(team_field)
    if isinstance(value, dict):
        return value.get("id") or value.get("value")
    return value


def find_team_id(jira, team_name):
    """Resolve an Atlassian team name to the UUID the Team field requires.

    Prefer JQL autocomplete (name -> id). Fall back to an issue search if
    suggestions are empty; that path only works when Jira accepts the name.
    """
    suggestions = jira.get_field_auto_complete_for_query_string(
        field_name="Team",
        field_value=team_name,
    )
    payload = suggestions or {}
    team_id = team_id_from_suggestions(payload, team_name)
    if team_id:
        return team_id
    field_id = team_field_id(jira)
    try:
        issues = search_jql(
            jira,
            f"Team = {jql_string(team_name)}",
            fields=[field_id],
            limit=1,
        )
    except jira_http_error():
        issues = []
    if issues:
        found = issue_team_id(issues[0], field_id)
        if found:
            return found
    raise RuntimeError(f'cannot find Jira team "{team_name}"')


def resolve_plan_teams(jira, plan):
    """Return (team_field_id, {team_name: team_uuid}) for the plan's teams."""
    field_id = team_field_id(jira)
    names = {plan.team, CROSS_DISTRO_TEAM}
    ids = {name: find_team_id(jira, name) for name in names}
    return field_id, ids


def ensure_team(jira, key, team_field, team_id):
    """Set the Team field when it does not already match team_id."""
    issue = jira.get_issue(key, fields=team_field)
    if issue_team_id(issue, team_field) == team_id:
        return
    edit_issue(jira, key, {team_field: team_id})


def issue_story_points(issue, story_points_field):
    value = (issue.get("fields") or {}).get(story_points_field)
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return value


def ensure_story_points(jira, key, story_points_field, points):
    """Set Story Points when they do not already match points."""
    issue = jira.get_issue(key, fields=story_points_field)
    if issue_story_points(issue, story_points_field) == points:
        return
    edit_issue(jira, key, {story_points_field: points})


def ensure_fix_version(jira, plan, create_version):
    """Require Fix Version to exist; create it only for a major --create-version."""
    if create_version and plan.variant != "major":
        raise RuntimeError(
            f'cannot use --create-version with variant "{plan.variant}", '
            "Jira versions exist only for major releases"
        )
    versions = jira.get_project_versions(DEFAULT_PROJECT) or []
    names = {item.get("name") for item in versions if isinstance(item, dict)}
    if plan.jira_version in names:
        return
    if not create_version:
        raise RuntimeError(
            f'cannot find Jira version "{plan.jira_version}" in {DEFAULT_PROJECT}; '
            "create it in Jira first or pass --create-version"
        )
    create_project_version(jira, plan.jira_version)


def classify_children(plan, children):
    """Split epic children into expected tasks (by summary) and extras."""
    expected = {task.summary: None for task in plan.tasks}
    unexpected = []
    for child in children:
        summary = child.get("fields", {}).get("summary", "")
        if summary in expected and expected[summary] is None:
            expected[summary] = child
        else:
            unexpected.append(child)
    return expected, unexpected


def issue_status_name(issue):
    status = (issue.get("fields") or {}).get("status") or {}
    return status.get("name") or ""


def is_unstarted_status(issue):
    return issue_status_name(issue).lower() in {"", "to do", "todo", "open"}


def ensure_status(jira, key, status_name=TARGET_STATUS):
    """Move the issue to status_name when it is not already there."""
    issue = jira.get_issue(key, fields="status")
    current = issue_status_name(issue)
    if current.lower() == status_name.lower():
        return
    payload = jira.get_transitions(key) or {}
    for trans in payload.get("transitions") or []:
        dest = ((trans.get("to") or {}).get("name")) or ""
        if dest.lower() == status_name.lower():
            jira.do_transition(
                key,
                data={"transition": {"id": str(trans["id"])}},
            )
            return
    extra = f' from "{current}"' if current else ""
    raise RuntimeError(f'cannot move {key} to "{status_name}"{extra}')


def triage_if_needed(jira, key, created):
    """Triage newly created issues, and recover generated ones still in To Do."""
    if not created:
        issue = jira.get_issue(key, fields="status")
        if not is_unstarted_status(issue):
            return
    ensure_status(jira, key)


def issue_labels(issue):
    return list((issue.get("fields") or {}).get("labels") or [])


def is_generated_issue(issue):
    """True when the issue was created by this script (has GENERATED_LABEL)."""
    return GENERATED_LABEL in issue_labels(issue)


def matching_unlinked_task(issues, summary, epic_key):
    """Find a generated task with this summary not owned by another epic."""
    for issue in issues:
        if issue.get("fields", {}).get("summary") != summary:
            continue
        if not is_generated_issue(issue):
            continue
        parent = issue_parent_key(issue)
        if parent in (None, epic_key):
            return issue
    return None


def list_existing_release(plan, jira, existing):
    """Report the matching epic and its children without creating or editing."""
    epic_key = existing["key"]
    children = list_children(jira, epic_key, fields=["summary", "labels", "parent"])
    by_summary, unexpected_children = classify_children(plan, children)
    tasks = tuple(
        (by_summary[task.summary]["key"], task.summary)
        for task in plan.tasks
        if by_summary.get(task.summary) is not None
    )
    unexpected = tuple(
        (child["key"], child.get("fields", {}).get("summary", ""))
        for child in unexpected_children
    )
    return ApplyResult(
        epic_key=epic_key,
        tasks=tasks,
        created_keys=(),
        unexpected=unexpected,
        epic_created=False,
        updated=False,
    )


def apply_plan(plan, jira, force=False, create_version=False):
    """Create the release epic and tasks, or update them with --force.

    Without --force, a matching epic is listed and left unchanged. --force
    creates missing tasks and rewrites the script-generated epic and labeled
    tasks (parent, Fix Version, team, Story Points, labels, Description,
    Acceptance Criteria, and Triaged when still in To Do). Unlabeled issues
    that match a planned summary are listed but not changed. Extra generated
    children are labeled not-in-release-plan. Story Points are set on
    generated tasks only; the epic is left unset so Jira can roll up the
    child total.
    """
    ensure_fix_version(jira, plan, create_version)
    existing = find_epic(jira, plan.epic_summary)
    if existing is not None and not force:
        return list_existing_release(plan, jira, existing)
    ac_field = acceptance_criteria_field_id(jira)
    points_field = story_points_field_id(jira)
    team_field, team_ids = resolve_plan_teams(jira, plan)
    child_fields = ["summary", "labels", "parent", "description", ac_field]
    epic_created = existing is None
    if existing is None:
        epic = create_issue(
            jira,
            epic_fields(
                DEFAULT_PROJECT,
                plan,
                ac_field,
                team_field=team_field,
                team_id=team_ids[plan.team],
            ),
        )
        epic_key = epic["key"]
        previous_adf = None
        children = []
    else:
        epic_key = existing["key"]
        issue = jira.get_issue(epic_key, fields=f"description,{ac_field}")
        previous_adf = checklist_adf_from_issue(issue, ac_field)
        children = list_children(jira, epic_key, fields=child_fields)

    by_summary, unexpected_children = classify_children(plan, children)
    created_keys = []
    tasks = []
    owned_keys = []
    generated_issues = None
    for task in plan.tasks:
        child = by_summary.get(task.summary)
        if child is None and force:
            if generated_issues is None:
                generated_issues = find_generated_issues(
                    jira,
                    plan.jira_version,
                    fields=child_fields,
                )
            child = matching_unlinked_task(generated_issues, task.summary, epic_key)
        assigned_team = team_ids[task_team(plan, task)]
        if child is None:
            issue = create_issue(
                jira,
                task_fields(
                    DEFAULT_PROJECT,
                    plan,
                    task,
                    epic_key,
                    ac_field,
                    team_field=team_field,
                    team_id=assigned_team,
                    story_points_field=points_field,
                ),
            )
            key = issue["key"]
            created_keys.append(key)
            owned_keys.append(key)
            tasks.append((key, task.summary))
            continue
        key = child["key"]
        tasks.append((key, task.summary))
        if not is_generated_issue(child):
            continue
        owned_keys.append(key)
        ensure_parent(jira, key, epic_key)
        ensure_fix_versions(jira, key, plan.jira_version)
        ensure_team(jira, key, team_field, assigned_team)
        ensure_story_points(jira, key, points_field, task.points)
        edit_issue(
            jira,
            key,
            {
                "description": issue_description(),
                ac_field: acceptance_criteria(task.checklist),
            },
        )
        ensure_label(jira, key, UNEXPECTED_LABEL, False)

    unexpected = []
    for child in unexpected_children:
        key = child["key"]
        summary = child.get("fields", {}).get("summary", "")
        if is_generated_issue(child):
            ensure_parent(jira, key, epic_key)
            ensure_fix_versions(jira, key, plan.jira_version)
            ensure_label(jira, key, UNEXPECTED_LABEL, True)
        unexpected.append((key, summary))

    progress_items = [progress_item(key, summary) for key, summary in tasks]
    unexpected_items = [
        unexpected_progress_item(key, summary) for key, summary in unexpected
    ]
    epic_update = {
        "parent": {"key": plan.parent_epic},
        "fixVersions": [{"name": plan.jira_version}],
        "description": issue_description(),
        ac_field: acceptance_criteria(
            progress_items,
            unexpected_items=unexpected_items,
            previous_adf=previous_adf,
        ),
        team_field: team_ids[plan.team],
    }
    epic_labels = issue_labels(jira.get_issue(epic_key, fields="labels"))
    if GENERATED_LABEL not in epic_labels:
        epic_update["labels"] = epic_labels + [GENERATED_LABEL]
    edit_issue(jira, epic_key, epic_update)
    created = set(created_keys)
    triage_if_needed(jira, epic_key, epic_created)
    for key in owned_keys:
        triage_if_needed(jira, key, key in created)
    return ApplyResult(
        epic_key=epic_key,
        tasks=tuple(tasks),
        created_keys=tuple(created_keys),
        unexpected=tuple(unexpected),
        epic_created=epic_created,
        updated=not epic_created,
    )


def browse_url(key, jira_url=DEFAULT_JIRA_URL):
    return f"{jira_url.rstrip('/')}/browse/{key}"


def issue_link(key, jira_url=DEFAULT_JIRA_URL, hyperlinks=True):
    """Issue key as visible text, with an OSC 8 hyperlink when hyperlinks is set."""
    if not hyperlinks:
        return key
    return f"\033]8;;{browse_url(key, jira_url)}\033\\{key}\033]8;;\033\\"


def print_apply_result(
    plan, result, jira_url=DEFAULT_JIRA_URL, out=None, hyperlinks=None
):
    """Print apply_plan output. TTY stdout uses clickable issue keys."""
    if out is None:
        out = sys.stdout
    if hyperlinks is None:
        hyperlinks = bool(getattr(out, "isatty", lambda: False)())
    created = set(result.created_keys)
    if result.epic_created:
        action = "Created"
    elif result.updated:
        action = "Updated"
    else:
        action = "Found"
    epic = issue_link(result.epic_key, jira_url, hyperlinks)
    print(f"{action} {epic}: {plan.epic_summary}", file=out)
    for key, summary in result.tasks:
        suffix = " [created]" if key in created else ""
        print(f"  {issue_link(key, jira_url, hyperlinks)}: {summary}{suffix}", file=out)
    if result.unexpected:
        print("Not in the expected release task list:", file=out)
        for key, summary in result.unexpected:
            print(f"  {issue_link(key, jira_url, hyperlinks)}: {summary}", file=out)
    print(f"Epic: {epic}", file=out)
    print(
        f"Parent: {issue_link(plan.parent_epic, jira_url, hyperlinks)}",
        file=out,
    )


def print_description_and_objectives(out, indent, objectives):
    prefix = " " * indent
    print(f"{prefix}Description:", file=out)
    for line in instruction_preview_lines():
        print(f"{prefix}  {line}", file=out)
    print(f"{prefix}Acceptance Criteria:", file=out)
    for item in objectives:
        print(f"{prefix}  [ ] {item}", file=out)


def print_plan(plan, out=None):
    """Print the dry-run plan (epic, tasks, teams, checklists)."""
    if out is None:
        out = sys.stdout
    print("Dry-run (pass --apply to create issues, --force to update)", file=out)
    print(file=out)
    print(f"Fix Version: {plan.jira_version}", file=out)
    print(f"Parent: {plan.parent_epic}", file=out)
    print(file=out)
    print(f"Epic: {plan.epic_summary}", file=out)
    print(f"  Team: {plan.team}", file=out)
    print_description_and_objectives(
        out, indent=2, objectives=[task.summary for task in plan.tasks]
    )
    for task in plan.tasks:
        print(file=out)
        print(f"Task: {task.summary}", file=out)
        print(f"  Team: {task_team(plan, task)}", file=out)
        print(f"  Points: {task.points}", file=out)
        print_description_and_objectives(out, indent=2, objectives=task.checklist)


def print_help(out=None):
    if out is None:
        out = sys.stdout
    prog = PROG_NAME
    flags = (
        ("      --apply", "Create issues in Jira when none exist (default is dry-run)"),
        ("      --create-version", "Create the Jira version if missing (major only)"),
        (
            "      --force",
            "Update preexisting script-generated epic and tasks for this version",
        ),
        ("      --security", "Treat a patch version as a security release"),
        (
            "      --team string",
            "Non-cross-distro team: EMEA (default), AMER, Cross-distro",
        ),
        (
            "      --lts-targets strings",
            "SRU series, comma-separated (default supported minus devel)",
        ),
        (
            "      --dev-target string",
            "Ubuntu devel series (default from ubuntu-distro-info)",
        ),
        ("  -h, --help", "Help for create-release-tickets"),
    )
    width = max(len(name) for name, _desc in flags)
    print("Create Jira epic and tasks for a snapd release.", file=out)
    print(file=out)
    print(
        "--apply requires JIRA_EMAIL (your Atlassian account email) and JIRA_API_TOKEN.",
        file=out,
    )
    print("Install atlassian-python-api (pip install atlassian-python-api).", file=out)
    print("Create a classic unscoped API token at:", file=out)
    print(f"  {JIRA_API_TOKEN_URL}", file=out)
    print(file=out)
    print("Usage:", file=out)
    print(f"  {prog} <version> [flags]", file=out)
    print(file=out)
    print("Examples:", file=out)
    print(f"  {prog} 2.78", file=out)
    print(f"  {prog} 2.77.1", file=out)
    print(f"  {prog} 2.77.1 --security", file=out)
    print(f"  {prog} 2.78 --apply", file=out)
    print(f"  {prog} 2.78 --team AMER", file=out)
    print(file=out)
    print("Flags:", file=out)
    for name, desc in flags:
        print(f"{name.ljust(width)}  {desc}", file=out)


def parse_arguments(argv=None):
    """Parse argv. Unknown flags and invalid --team raise UsageError."""
    if argv is None:
        argv = sys.argv[1:]
    parser = argparse.ArgumentParser(add_help=False, allow_abbrev=False)
    parser.add_argument("version", nargs="?")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--create-version", action="store_true", dest="create_version")
    parser.add_argument("--dev-target", dest="dev_target")
    parser.add_argument("--force", action="store_true")
    parser.add_argument("--security", action="store_true")
    parser.add_argument("--team", "--Team", dest="team")
    parser.add_argument("--lts-targets", dest="lts_targets")
    parser.add_argument("-h", "--help", action="store_true")
    args, unknown = parser.parse_known_args(argv)
    if unknown:
        token = unknown[0]
        if token.startswith("-"):
            raise UsageError(f"unknown flag: {token}")
        raise UsageError(f'unknown command "{token}" for "{PROG_NAME}"')
    args.team = parse_team(args.team)
    return args


def main(argv=None):
    """CLI entry: dry-run the plan, or apply it when --apply is passed."""
    if argv is None:
        argv = sys.argv[1:]
    try:
        args = parse_arguments(argv)
    except UsageError as err:
        print(f"Error: {err}", file=sys.stderr)
        print(f"Run '{PROG_NAME} --help' for usage.", file=sys.stderr)
        return 2

    if args.help:
        print_help()
        return 0
    if not args.version:
        print_help()
        return 2

    variant = variant_from_version(args.version, security=args.security)
    devel, targets = resolve_ubuntu_series(args.dev_target, args.lts_targets)
    plan = build_plan(
        variant=variant,
        version=args.version,
        devel=devel,
        targets=targets,
        team=args.team,
    )
    if not args.apply:
        print_plan(plan)
        return 0

    email, token = credentials()
    jira = connect_jira(DEFAULT_JIRA_URL, email, token)
    result = apply_plan(
        plan,
        jira,
        force=args.force,
        create_version=args.create_version,
    )
    print_apply_result(plan, result)
    return 0


def cli(argv=None):
    """Run main(), mapping SNAPDENG and Jira I/O failures to a one-line exit."""
    try:
        return main(argv)
    except RuntimeError as err:
        print(err, file=sys.stderr)
        return 1
    except jira_io_errors() as err:
        print(jira_error_message(err), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(cli())
