#!/usr/bin/env python3

"""Track snapd GitHub security advisories as SNAPDENG Jira tickets.

Commands:
  resolve-mode  Print summary, dry-run, apply, or skip from the workflow event.
  summary       Read-only overview. Markdown by default, or JSON with --format json.
  sync          Create missing triage and draft tickets, generate a petname when
                the advisory has none, and transition status. --dry-run does not write.

Environment:
  SNAPD_ADVISORY_GITHUB_TOKEN  preferred token for repository security advisories
  GITHUB_TOKEN                 fallback advisory token
  GITHUB_REPOSITORY            owner/name, default canonical/snapd
  JIRA_API_TOKEN               Jira Cloud API token
  JIRA_EMAIL                   account email for that token
  JIRA_BASE_URL                default https://warthogs.atlassian.net
  MATTERMOST_NEW_ADVISORY_WEBHOOK_URL
                               incoming webhook for a newly created ticket
  MATTERMOST_MAINTENANCE_WEBHOOK_URL
                               incoming webhook for the run-summary alert
  GITHUB_SERVER_URL            server used in the maintenance run link
  GITHUB_RUN_ID                Actions run id used in that link
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import re
import subprocess
import sys
import unicodedata
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable, Mapping, Sequence
from dataclasses import asdict, dataclass, replace
from datetime import UTC, datetime, timedelta
from typing import Any

GITHUB_API = "https://api.github.com"
PROPERTY_KEY = "advisory-tracker"
AUTOMATION_MARK = "advisory-tracker:"
CHANNELS = ("mattermost", "jira_comment")
NEW_ADVISORY_WEBHOOK = "MATTERMOST_NEW_ADVISORY_WEBHOOK_URL"
MAINTENANCE_WEBHOOK = "MATTERMOST_MAINTENANCE_WEBHOOK_URL"
NOTIFY_EVENTS = ("created", "not_ready", "stall")
ISSUE_FIELDS = (
    "summary",
    "status",
    "labels",
    "description",
    "assignee",
    "reporter",
    "created",
    "issuetype",
    "parent",
)
GHSA_RE = re.compile(r"^GHSA-[0-9a-z]{4}-[0-9a-z]{4}-[0-9a-z]{4}$")
ISSUE_KEY_RE = re.compile(r"^[A-Z][A-Z0-9]+-\d+$")
PROJECT_KEY_RE = re.compile(r"^[A-Z][A-Z0-9]+$")
REPO_RE = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
CODENAME_RE = re.compile(
    r"\((?:aka\s+)?(?P<name>[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+)+(?:\s+\d+)?)\)\s*$"
)
SUMMARY_SHAPE_RE = re.compile(r"^[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+)+(?:\s+\d+)?$")
petname_word_dir = "/usr/share/petname/small"
GENERATED_NAME_RE = re.compile(r"^[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+)+$")
NAME_DRAW_LIMIT = 20
KEY_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")
MAP_KEY_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_']*(?: [A-Za-z_][A-Za-z0-9_']*)*")
SEVERITY_KEYS = ("critical", "high", "medium", "low")
THRESHOLD_KEYS = (*SEVERITY_KEYS, "default")


class TrackerError(Exception):
    def __init__(self, message: str, status: int | None = None) -> None:
        super().__init__(message)
        self.status = status


@dataclass(frozen=True)
class HttpResult:
    status: int
    headers: dict[str, str]
    body: bytes


class HttpClient:
    def request(
        self,
        method: str,
        url: str,
        headers: Mapping[str, str],
        body: bytes | None,
    ) -> HttpResult:
        raise NotImplementedError


class UrllibClient(HttpClient):
    def request(
        self,
        method: str,
        url: str,
        headers: Mapping[str, str],
        body: bytes | None,
    ) -> HttpResult:
        request = urllib.request.Request(
            url, data=body, headers=dict(headers), method=method
        )
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                header_map = {
                    key.lower(): value for key, value in response.headers.items()
                }
                return HttpResult(response.status, header_map, response.read())
        except urllib.error.HTTPError as exc:
            header_map = {key.lower(): value for key, value in exc.headers.items()}
            return HttpResult(exc.code, header_map, exc.read())
        except urllib.error.URLError as exc:
            host = urllib.parse.urlparse(url).netloc or "endpoint"
            raise TrackerError(f"cannot reach {host}: {exc.reason}") from exc


@dataclass(frozen=True)
class Stage:
    name: str
    within_days: int
    within_by_severity: dict[str, int]
    kind: str
    comments_min: int
    statuses: tuple[str, ...]


@dataclass(frozen=True)
class Config:
    parent_key: str
    project_key: str
    issue_type: str
    create_for_states: tuple[str, ...]
    state_map: dict[str, str]
    status_progress: dict[str, str]
    priority_map: dict[str, str]
    min_description_chars: int
    stall_cooldown_days: int
    stages: tuple[Stage, ...]
    notify: dict[str, tuple[str, ...]]


@dataclass(frozen=True)
class Issue:
    key: str
    summary: str
    status: str
    labels: tuple[str, ...]
    description_text: str
    assignee: bool
    created: datetime
    issue_type: str
    parent_key: str
    assignee_id: str | None = None
    assignee_name: str | None = None
    reporter_id: str | None = None
    reporter_name: str | None = None


@dataclass
class Stall:
    issue_key: str
    reasons: list[str]


@dataclass
class PendingStall:
    row: AdvisoryRow
    issue_key: str
    stage_name: str


@dataclass
class PendingNotReady:
    row: AdvisoryRow
    epic_key: str
    ghsa_id: str


@dataclass
class AdvisoryRow:
    ghsa_id: str
    url: str
    cve_id: str | None
    summary: str
    codename: str | None
    state: str
    severity: str
    expected_status: str | None
    ready: bool
    reasons: list[str]
    action: str
    issue_key: str | None
    issue_keys: list[str]
    issue_status: str | None
    status_aligned: bool
    stalls: list[Stall]
    last_activity: str | None
    notifications: list[str]
    error: str | None


@dataclass(frozen=True)
class Permissions:
    create: bool
    notify: bool
    sync: bool


@dataclass
class Report:
    parent: str
    mode: str
    confirmation_error: str | None
    errors: list[str]
    advisories: list[AdvisoryRow]
    permissions: Permissions


def default_config_path() -> str:
    return os.path.normpath(
        os.path.join(
            os.path.dirname(os.path.abspath(__file__)), "..", "advisory-tracker.yaml"
        )
    )


def parse_scalar(raw: str) -> Any:
    if len(raw) >= 2 and raw[0] == raw[-1] and raw[0] in {'"', "'"}:
        inner = raw[1:-1]
        if raw[0] == '"':
            inner = inner.replace('\\"', '"').replace("\\n", "\n")
        return inner
    if raw in {"true", "True", "TRUE"}:
        return True
    if raw in {"false", "False", "FALSE"}:
        return False
    if raw in {"null", "Null", "NULL", "~"}:
        return None
    if re.fullmatch(r"-?\d+", raw):
        return int(raw)
    return raw


def _prepared_lines(text: str) -> list[tuple[int, str]]:
    lines: list[tuple[int, str]] = []
    for number, raw in enumerate(text.splitlines(), start=1):
        if raw.strip() == "" or raw.lstrip().startswith("#"):
            continue
        if "\t" in raw:
            raise TrackerError(
                f"cannot parse config: tabs are not allowed on line {number}"
            )
        indent = len(raw) - len(raw.lstrip(" "))
        lines.append((indent, raw.strip()))
    return lines


def _parse_at(lines: list[tuple[int, str]], index: int) -> tuple[Any, int]:
    if index >= len(lines):
        raise TrackerError("cannot parse config: unexpected end of file")
    indent, text = lines[index]
    if text.startswith("- "):
        return _parse_list(lines, index, indent)
    return _parse_map(lines, index, indent)


def _mapping_entry(text: str) -> tuple[str, str] | None:
    if text.startswith(("'", '"')):
        quote = text[0]
        end = text.find(quote, 1)
        if end < 1:
            return None
        tail = text[end + 1 :].lstrip()
        if not tail.startswith(":"):
            return None
        key = parse_scalar(text[: end + 1])
        if not isinstance(key, str) or key.strip() == "":
            return None
        return key, tail[1:].strip()
    key, sep, rest = text.partition(":")
    key = key.strip()
    if sep == "" or not MAP_KEY_RE.fullmatch(key):
        return None
    return key, rest.strip()


def _parse_map(
    lines: list[tuple[int, str]], index: int, indent: int
) -> tuple[dict[str, Any], int]:
    result: dict[str, Any] = {}
    while index < len(lines):
        line_indent, text = lines[index]
        if line_indent < indent:
            break
        if line_indent > indent or text.startswith("- "):
            raise TrackerError(
                f"cannot parse config: expected a mapping entry near {text}"
            )
        entry = _mapping_entry(text)
        if entry is None:
            raise TrackerError(f"cannot parse config: expected key: value near {text}")
        key, rest = entry
        if key in result:
            raise TrackerError(f"cannot parse config: duplicate key {key}")
        rest = rest.strip()
        index += 1
        if rest == "":
            if index < len(lines) and lines[index][0] > indent:
                value, index = _parse_at(lines, index)
            else:
                value = None
        elif rest in {"|", ">"}:
            raise TrackerError(
                "cannot parse config: multiline scalars are not supported"
            )
        else:
            value = parse_scalar(rest)
        result[key] = value
    return result, index


def _parse_list(
    lines: list[tuple[int, str]], index: int, indent: int
) -> tuple[list[Any], int]:
    items: list[Any] = []
    while index < len(lines):
        line_indent, text = lines[index]
        if line_indent < indent:
            break
        if line_indent > indent or not text.startswith("- "):
            raise TrackerError(
                f"cannot parse config: expected a list entry near {text}"
            )
        rest = text[2:].strip()
        index += 1
        key, sep, after = rest.partition(":")
        if sep and KEY_RE.fullmatch(key.strip()) and not rest.startswith(("'", '"')):
            item: dict[str, Any] = {}
            after = after.strip()
            item_key = key.strip()
            if after == "":
                if index < len(lines) and lines[index][0] > indent:
                    item[item_key], index = _parse_at(lines, index)
                else:
                    item[item_key] = None
            else:
                item[item_key] = parse_scalar(after)
            if index < len(lines) and lines[index][0] > indent:
                nested, index = _parse_map(lines, index, lines[index][0])
                overlap = set(item).intersection(nested)
                if overlap:
                    raise TrackerError(
                        f"cannot parse config: duplicate key {min(overlap)}"
                    )
                item.update(nested)
            items.append(item)
            continue
        if rest == "":
            if index >= len(lines):
                raise TrackerError("cannot parse config: empty list item")
            value, index = _parse_at(lines, index)
            items.append(value)
            continue
        items.append(parse_scalar(rest))
    return items, index


def parse_yaml(text: str) -> dict[str, Any]:
    lines = _prepared_lines(text)
    if not lines:
        raise TrackerError("cannot parse config: document is empty")
    value, index = _parse_at(lines, 0)
    if index != len(lines):
        raise TrackerError("cannot parse config: leftover content")
    if not isinstance(value, dict):
        raise TrackerError("cannot parse config: document must be a mapping")
    return value


def _require_mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise TrackerError(f"cannot load config: {label} must be a mapping")
    return value


def _require_string(value: Any, label: str) -> str:
    if not isinstance(value, str) or value.strip() == "":
        raise TrackerError(f"cannot load config: {label} must be a non-empty string")
    return value


def _require_string_list(value: Any, label: str) -> tuple[str, ...]:
    if (
        not isinstance(value, list)
        or not value
        or not all(isinstance(item, str) and item.strip() for item in value)
    ):
        raise TrackerError(f"cannot load config: {label} must be a list of strings")
    return tuple(value)


def _string_map(value: Any, label: str) -> dict[str, str]:
    mapping = _require_mapping(value, label)
    result: dict[str, str] = {}
    for key, item in mapping.items():
        result[key] = _require_string(item, f"{label}.{key}")
    if not result:
        raise TrackerError(f"cannot load config: {label} must not be empty")
    return result


def load_config(path: str) -> Config:
    try:
        with open(path, encoding="utf-8") as handle:
            text = handle.read()
    except OSError as exc:
        raise TrackerError(f"cannot read config {path}: {exc.strerror}") from exc
    return config_from_mapping(parse_yaml(text))


def config_from_mapping(raw: Mapping[str, Any]) -> Config:
    parent_key = _require_string(raw.get("parent_key"), "parent_key")
    project_key = _require_string(raw.get("project_key"), "project_key")
    issue_type = _require_string(raw.get("issue_type"), "issue_type")
    if not ISSUE_KEY_RE.fullmatch(parent_key):
        raise TrackerError(
            f"cannot load config: parent_key {parent_key} is not an issue key"
        )
    if not PROJECT_KEY_RE.fullmatch(project_key):
        raise TrackerError(
            f"cannot load config: project_key {project_key} is not a project key"
        )
    if not parent_key.startswith(project_key + "-"):
        raise TrackerError("cannot load config: parent_key must belong to project_key")
    create_for_states = _require_string_list(
        raw.get("create_for_states"), "create_for_states"
    )
    state_map = _string_map(raw.get("state_map"), "state_map")
    status_progress = _load_status_progress(raw.get("status_progress"), state_map)
    priority_map = _string_map(raw.get("priority_map"), "priority_map")
    for state in create_for_states:
        if state not in state_map:
            raise TrackerError(
                f"cannot load config: create state {state} is missing from state_map"
            )
    readiness = _require_mapping(raw.get("readiness"), "readiness")
    minimum = readiness.get("min_description_chars")
    if not isinstance(minimum, int) or minimum < 1:
        raise TrackerError(
            "cannot load config: readiness.min_description_chars "
            "must be a positive integer"
        )
    progress = _require_mapping(raw.get("progress"), "progress")
    cooldown = progress.get("stall_cooldown_days")
    if not isinstance(cooldown, int) or cooldown < 0:
        raise TrackerError(
            "cannot load config: progress.stall_cooldown_days "
            "must be a non-negative integer"
        )
    stage_values = progress.get("stages")
    if not isinstance(stage_values, list) or not stage_values:
        raise TrackerError("cannot load config: progress.stages must be a list")
    stages: list[Stage] = []
    seen: set[str] = set()
    for stage in stage_values:
        stages.append(_load_stage(stage, seen))
    notify_raw = _require_mapping(raw.get("notify"), "notify")
    notify: dict[str, tuple[str, ...]] = {}
    for event in NOTIFY_EVENTS:
        if event not in notify_raw:
            raise TrackerError(f"cannot load config: notify.{event} is required")
        channels = notify_raw[event]
        if channels is None:
            channels = []
        if not isinstance(channels, list) or not all(
            isinstance(item, str) for item in channels
        ):
            raise TrackerError(
                f"cannot load config: notify.{event} must be a list of channels"
            )
        for channel in channels:
            if channel not in CHANNELS:
                raise TrackerError(f"cannot load config: unknown channel {channel}")
        notify[event] = tuple(channels)
    return Config(
        parent_key=parent_key,
        project_key=project_key,
        issue_type=issue_type,
        create_for_states=create_for_states,
        state_map=state_map,
        status_progress=status_progress,
        priority_map=priority_map,
        min_description_chars=minimum,
        stall_cooldown_days=cooldown,
        stages=tuple(stages),
        notify=notify,
    )


def _load_status_progress(value: Any, state_map: Mapping[str, str]) -> dict[str, str]:
    mapping = _require_mapping(value, "status_progress")
    if not mapping:
        raise TrackerError("cannot load config: status_progress must not be empty")
    result: dict[str, str] = {}
    for key, item in mapping.items():
        if not isinstance(key, str) or key.strip() == "":
            raise TrackerError(
                "cannot load config: status_progress status name must be a string"
            )
        if not isinstance(item, str) or item.strip() == "":
            raise TrackerError(
                f"cannot load config: status_progress.{key} must be a non-empty string"
            )
        if item not in state_map:
            raise TrackerError(
                f"cannot load config: status_progress.{key} is not an advisory state"
            )
        result[key] = item
    return result


def _load_stage(raw: Any, seen: set[str]) -> Stage:
    stage = _require_mapping(raw, "stage")
    name = _require_string(stage.get("name"), "stage.name")
    if name in seen:
        raise TrackerError(f"cannot load config: duplicate stage {name}")
    seen.add(name)
    within_days, within_by_severity = _load_within_days(stage.get("within_days"), name)
    requires = _require_mapping(stage.get("requires"), f"stage {name} requires")
    kinds = [
        key
        for key in ("assignee", "human_comments_min", "status_any")
        if key in requires
    ]
    if len(kinds) != 1:
        raise TrackerError(
            f"cannot load config: stage {name} must set exactly one requirement"
        )
    kind = kinds[0]
    if kind == "assignee":
        if requires[kind] is not True:
            raise TrackerError(
                f"cannot load config: stage {name} assignee requirement must be true"
            )
        return Stage(name, within_days, within_by_severity, "assignee", 0, ())
    if kind == "human_comments_min":
        count = requires[kind]
        if not isinstance(count, int) or count < 1:
            raise TrackerError(
                f"cannot load config: stage {name} human_comments_min "
                "must be a positive integer"
            )
        return Stage(name, within_days, within_by_severity, "comments", count, ())
    statuses = _require_string_list(requires[kind], f"stage {name} status_any")
    return Stage(name, within_days, within_by_severity, "status", 0, statuses)


def _load_within_days(within: Any, name: str) -> tuple[int, dict[str, int]]:
    if isinstance(within, int):
        if within < 1:
            raise TrackerError(
                f"cannot load config: stage {name} within_days "
                "must be a positive integer"
            )
        return within, {}
    if not isinstance(within, dict):
        raise TrackerError(
            f"cannot load config: stage {name} within_days "
            "must be a positive integer or a severity map"
        )
    unknown = [key for key in within if key not in THRESHOLD_KEYS]
    if unknown:
        raise TrackerError(
            f"cannot load config: stage {name} within_days "
            f"unknown severity {unknown[0]}"
        )
    missing = [key for key in THRESHOLD_KEYS if key not in within]
    if missing:
        raise TrackerError(
            f"cannot load config: stage {name} within_days missing {missing[0]}"
        )
    limits: dict[str, int] = {}
    for key in THRESHOLD_KEYS:
        value = within[key]
        if not isinstance(value, int) or value < 1:
            raise TrackerError(
                f"cannot load config: stage {name} within_days.{key} "
                "must be a positive integer"
            )
        limits[key] = value
    return limits["default"], limits


def extract_codename(summary: str) -> str | None:
    match = CODENAME_RE.search(summary.strip())
    if match is None:
        return None
    return match.group("name")


def normalize_title(text: str) -> str:
    text = unicodedata.normalize("NFKC", text)
    text = re.sub(r"[\u200b-\u200d\ufeff\u2060]", "", text)
    return " ".join(text.split())


def petname_from_summary(summary: str) -> str | None:
    text = normalize_title(summary)
    if SUMMARY_SHAPE_RE.fullmatch(text):
        return text
    found = extract_codename(text)
    if found:
        return found
    lead = re.match(
        r"^(?P<name>[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+)+"
        r"(?:\s+\d+)?)\s*[:\u2013\u2014-]\s+\S",
        text,
    )
    if lead and SUMMARY_SHAPE_RE.fullmatch(lead.group("name")):
        return lead.group("name")
    return None


def petname_from_issues(issues: list[Issue]) -> str | None:
    fallback = ""
    for issue in issues:
        name = petname_from_summary(issue.summary)
        if name is None:
            continue
        if normalize_title(issue.summary) == name:
            return name
        if fallback == "":
            fallback = name
    return fallback or None


def codename_key(name: str) -> str:
    compact = " ".join(name.split())
    base = re.sub(r"\s+\d+$", "", compact)
    return base.casefold()


def _word_file(path: str) -> set[str] | None:
    try:
        with open(path, encoding="utf-8") as handle:
            return {line.strip().casefold() for line in handle if line.strip()}
    except OSError:
        return None


def petname_vocabulary() -> tuple[set[str], set[str]] | None:
    adjectives = _word_file(os.path.join(petname_word_dir, "adjectives.txt"))
    names = _word_file(os.path.join(petname_word_dir, "names.txt"))
    if adjectives is None or names is None:
        return None
    return adjectives, names


def is_public_codename(name: str) -> bool:
    vocabulary = petname_vocabulary()
    if vocabulary is None:
        return False
    adjectives, names = vocabulary
    base = re.sub(r"\s+\d+$", "", " ".join(name.split()))
    parts = base.split(" ")
    if len(parts) != 2:
        return False
    return parts[0].casefold() in adjectives and parts[1].casefold() in names


def public_label(row: AdvisoryRow) -> str:
    if row.codename and is_public_codename(row.codename):
        return row.codename
    return row.ghsa_id


def format_petname(raw: str) -> str:
    words = [word for word in re.split(r"[\s_-]+", raw.strip()) if word]
    return " ".join(word[:1].upper() + word[1:].lower() for word in words)


def petname_words() -> str:
    try:
        completed = subprocess.run(
            ["petname", "--words", "2", "--separator", " "],
            check=False,
            capture_output=True,
            text=True,
            timeout=10,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise TrackerError(
            "cannot generate codename: petname is not available"
        ) from exc
    if completed.returncode != 0 or completed.stdout.strip() == "":
        raise TrackerError("cannot generate codename: petname failed")
    return completed.stdout.strip()


def choose_codename(source: Callable[[], str], used: set[str]) -> str:
    for _ in range(NAME_DRAW_LIMIT):
        formatted = format_petname(source())
        if GENERATED_NAME_RE.fullmatch(formatted) is None:
            continue
        key = codename_key(formatted)
        if key in used:
            continue
        used.add(key)
        return formatted
    raise TrackerError(
        f"cannot generate codename: no unused petname after {NAME_DRAW_LIMIT} draws"
    )


def collect_used_codenames(
    advisories: Sequence[Mapping[str, Any]], children: Sequence[Issue]
) -> set[str]:
    used: set[str] = set()
    for advisory in advisories:
        summary = advisory.get("summary")
        if isinstance(summary, str):
            name = extract_codename(summary)
            if name:
                used.add(codename_key(name))
    for issue in children:
        name = petname_from_summary(issue.summary)
        if name:
            used.add(codename_key(name))
    return used


def summary_with_codename(summary: str, codename: str) -> str:
    return f"{summary.rstrip()} ({codename})"


def readiness_reasons(advisory: Mapping[str, Any], config: Config) -> list[str]:
    reasons: list[str] = []
    summary = advisory.get("summary")
    if not isinstance(summary, str) or summary.strip() == "":
        reasons.append("missing summary")
    description = advisory.get("description")
    if (
        not isinstance(description, str)
        or len(description.strip()) < config.min_description_chars
    ):
        reasons.append(
            f"description is shorter than {config.min_description_chars} characters"
        )
    return reasons


def transition_payload(transition_id: str) -> dict[str, dict[str, str]]:
    return {"transition": {"id": transition_id}}


def transition_needed(current_status: str, expected_status: str) -> bool:
    return current_status != expected_status


def sync_mismatch(row: AdvisoryRow, issue: Issue) -> str:
    return (
        f"{row.ghsa_id} {issue.key}: advisory state {row.state} "
        f"!= Jira status {issue.status}, expected {row.expected_status or ''}"
    )


def should_store_state(
    stored_state: str | None,
    advisory_state: str,
    transition_required: bool,
    transitioned: bool,
) -> bool:
    if stored_state == advisory_state:
        return False
    return not (transition_required and not transitioned)


def requirement_met(stage: Stage, issue: Issue, human_comments: int) -> bool:
    if stage.kind == "assignee":
        return issue.assignee
    if stage.kind == "comments":
        return human_comments >= stage.comments_min
    if stage.kind == "status":
        return issue.status in stage.statuses
    raise TrackerError(f"cannot evaluate stage {stage.name}: unknown requirement")


def selected_threshold(stage: Stage, severity: str) -> tuple[int, str]:
    if not stage.within_by_severity:
        return stage.within_days, ""
    if severity in SEVERITY_KEYS:
        return stage.within_by_severity[severity], severity
    return stage.within_by_severity["default"], "unset"


def stall_reason(stage: Stage, severity: str) -> str:
    days, label = selected_threshold(stage, severity)
    if stage.kind == "assignee":
        detail = "no assignee"
    elif stage.kind == "comments":
        noun = "human comment" if stage.comments_min == 1 else "human comments"
        detail = f"fewer than {stage.comments_min} {noun}"
    elif stage.kind == "status":
        names = stage.statuses
        if len(names) <= 1:
            reached = ", ".join(names)
        elif len(names) == 2:
            reached = f"{names[0]} or {names[1]}"
        else:
            reached = ", ".join(names[:-1]) + f", or {names[-1]}"
        detail = f"has not reached {reached}"
    else:
        raise TrackerError(f"cannot evaluate stage {stage.name}: unknown requirement")
    message = (
        f"not {stage.name.replace('_', ' ')}: {detail} "
        f"within {days} days of ticket creation"
    )
    if label == "unset":
        return f"{message} for unset severity"
    if label:
        return f"{message} for {label}"
    return message


def behind_stages(
    issue: Issue, human_comments: int, now: datetime, config: Config, severity: str = ""
) -> list[Stage]:
    if now.tzinfo is None or issue.created.tzinfo is None:
        raise TrackerError(
            "cannot evaluate progress: timestamps must include a timezone"
        )
    if config.stages and requirement_met(config.stages[-1], issue, human_comments):
        return []
    age = now - issue.created
    late: list[Stage] = []
    for stage in config.stages:
        days, _label = selected_threshold(stage, severity)
        if age < timedelta(days=days):
            continue
        if requirement_met(stage, issue, human_comments):
            continue
        late.append(stage)
    return late


def _clean_not_ready(value: Any) -> str | dict[str, str]:
    if isinstance(value, str) and value != "":
        return value
    if isinstance(value, dict):
        return {
            key: stamp
            for key, stamp in value.items()
            if isinstance(key, str) and isinstance(stamp, str) and stamp != ""
        }
    return {}


def _clean_stalls(value: Any) -> dict[str, str | dict[str, str]]:
    if not isinstance(value, dict):
        return {}
    clean: dict[str, str | dict[str, str]] = {}
    for name, stamp in value.items():
        if not isinstance(name, str):
            continue
        if isinstance(stamp, str) and stamp != "":
            clean[name] = stamp
            continue
        if not isinstance(stamp, dict):
            continue
        channels = {
            channel: sent
            for channel, sent in stamp.items()
            if isinstance(channel, str) and isinstance(sent, str) and sent != ""
        }
        if channels:
            clean[name] = channels
    return clean


def _property_body(
    prop: Mapping[str, Any] | None,
    advisory_state: str | None,
    stalls: Mapping[str, Any],
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "advisory_state": advisory_state,
        "stalls": dict(stalls),
    }
    if prop is not None and prop.get("not_ready"):
        body["not_ready"] = prop["not_ready"]
    return body


def _stamp_is_current(stamp: str | None, now: datetime, days: int) -> bool:
    if not isinstance(stamp, str) or days <= 0:
        return False
    try:
        previous = parse_time(stamp)
    except TrackerError:
        return False
    return now - previous < timedelta(days=days)


def cooldown_active(
    prop: Mapping[str, Any] | None, stage_name: str, now: datetime, days: int
) -> bool:
    if prop is None or days <= 0:
        return False
    stalls = prop.get("stalls")
    if not isinstance(stalls, dict):
        return False
    raw = stalls.get(stage_name)
    if not isinstance(raw, str):
        return False
    # The visible title says "not assigned". The stored key stays the stage
    # name, such as "assigned", so stamps from older runs still match.
    return _stamp_is_current(raw, now, days)


def _channel_cooled(stamp: Any, channel: str, now: datetime, days: int) -> bool:
    # A string stamp was stored only after every channel succeeded, so it
    # still cools each channel. A map records the channels that succeeded.
    if isinstance(stamp, str):
        return _stamp_is_current(stamp, now, days)
    if isinstance(stamp, dict):
        raw = stamp.get(channel)
        return _stamp_is_current(raw if isinstance(raw, str) else None, now, days)
    return False


def _merge_channel_stamps(
    current: Any, sent: tuple[str, ...], stamp: str
) -> dict[str, str]:
    merged = dict(current) if isinstance(current, dict) else {}
    for channel in sent:
        merged[channel] = stamp
    return merged


def confirm_children(children: list[Issue], config: Config) -> str | None:
    if not children:
        return "cannot confirm issue type: parent has no child tickets"
    if any(issue.issue_type == config.issue_type for issue in children):
        return None
    found = ", ".join(sorted({issue.issue_type for issue in children}))
    return (
        f"cannot confirm issue type: configured {config.issue_type} "
        f"does not match child issue type {found}"
    )


def issue_references(issue: Issue, ghsa_id: str) -> bool:
    if ghsa_id == "":
        return False
    if any(ghsa_id in label for label in issue.labels):
        return True
    return ghsa_id in issue.summary or ghsa_id in issue.description_text


def _issue_rank(
    issue: Issue, parent_key: str, codename: str | None
) -> tuple[bool, bool, datetime, str]:
    named = codename is not None and issue.summary == codename
    child = issue.parent_key == parent_key
    return (not child, not named, issue.created, issue.key)


def select_issues(
    matches: list[Issue], parent_key: str, codename: str | None = None
) -> tuple[Issue | None, list[Issue]]:
    unique: list[Issue] = []
    seen: set[str] = set()
    for issue in matches:
        if issue.key in seen:
            continue
        seen.add(issue.key)
        unique.append(issue)
    if not unique:
        return None, []
    primary = min(unique, key=lambda issue: _issue_rank(issue, parent_key, codename))
    others = sorted(
        (issue for issue in unique if issue.key != primary.key),
        key=lambda issue: issue.key,
    )
    return primary, [primary, *others]


def parse_time(value: str) -> datetime:
    text = value.strip()
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    if re.search(r"[+-]\d{4}$", text):
        text = text[:-2] + ":" + text[-2:]
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError as exc:
        raise TrackerError(f"cannot parse timestamp {value}") from exc
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=UTC)
    return parsed.astimezone(UTC)


def is_true(value: str | None) -> bool:
    return (value or "").strip().lower() == "true"


def permissions_from_env(env: Mapping[str, str]) -> Permissions:
    return Permissions(
        is_true(env.get("ADVISORY_TRACKER_CREATE")),
        is_true(env.get("ADVISORY_TRACKER_NOTIFY")),
        is_true(env.get("ADVISORY_TRACKER_SYNC")),
    )


def permission_line(permissions: Permissions) -> str:
    def word(enabled: bool) -> str:
        return "on" if enabled else "off"

    return (
        f"Permissions: create {word(permissions.create)}, "
        f"notify {word(permissions.notify)}, sync {word(permissions.sync)}"
    )


def resolve_mode(env: Mapping[str, str]) -> str:
    event = env.get("GITHUB_EVENT_NAME") or ""
    permissions = permissions_from_env(env)
    if event == "schedule":
        action = "sync"
        apply = permissions.create or permissions.notify or permissions.sync
        dry_run = False
    elif event == "workflow_dispatch":
        action = env.get("INPUT_ACTION") or "summary"
        apply = is_true(env.get("INPUT_APPLY"))
        dry_run = is_true(env.get("INPUT_DRY_RUN"))
    elif event == "repository_dispatch":
        action = env.get("PAYLOAD_ACTION") or "summary"
        apply = is_true(env.get("PAYLOAD_APPLY"))
        dry_run = is_true(env.get("PAYLOAD_DRY_RUN"))
    else:
        raise TrackerError(f"cannot resolve mode: unsupported event {event or 'unset'}")
    if action not in {"summary", "sync"}:
        raise TrackerError(f"cannot resolve mode: unsupported action {action}")
    if action == "summary":
        return "summary"
    if dry_run:
        return "dry-run"
    if apply:
        return "apply"
    return "skip"


def _adf_inline(content: list[Any]) -> str:
    parts: list[str] = []
    for child in content:
        if isinstance(child, dict) and child.get("type") == "hardBreak":
            parts.append("\n")
            continue
        parts.append(adf_to_text(child))
    return "".join(parts)


def adf_to_text(node: Any) -> str:
    if node is None:
        return ""
    if isinstance(node, str):
        return node
    if isinstance(node, list):
        return "\n".join(
            part for part in (adf_to_text(child) for child in node) if part
        )
    if isinstance(node, dict):
        # A formatted GHSA id is several text nodes in one paragraph. Joining
        # those nodes with a newline hides the id from ticket matching.
        if node.get("type") in {"paragraph", "heading", "codeBlock"}:
            content = node.get("content")
            return _adf_inline(content if isinstance(content, list) else [])
        if node.get("type") == "hardBreak":
            return "\n"
        parts: list[str] = []
        if isinstance(node.get("text"), str):
            parts.append(node["text"])
        if node.get("type") == "mention":
            attrs = node.get("attrs")
            mention = attrs.get("text") if isinstance(attrs, dict) else None
            if isinstance(mention, str):
                parts.append(mention)
        content = node.get("content")
        if isinstance(content, list):
            nested = adf_to_text(content)
            if nested:
                parts.append(nested)
        return "\n".join(parts)
    return ""


def adf_doc(paragraphs: list[str]) -> dict[str, Any]:
    content = []
    for paragraph in paragraphs:
        text = paragraph.strip()
        if text == "":
            continue
        content.append(
            {"type": "paragraph", "content": [{"type": "text", "text": text}]}
        )
    if not content:
        content.append(
            {
                "type": "paragraph",
                "content": [{"type": "text", "text": "GitHub advisory"}],
            }
        )
    return {"type": "doc", "version": 1, "content": content}


def account_identity(value: Any) -> tuple[str | None, str | None]:
    if not isinstance(value, dict):
        return None, None
    account = value.get("accountId")
    name = value.get("displayName")
    account_id = account if isinstance(account, str) and account != "" else None
    display = name if isinstance(name, str) and name != "" else None
    return account_id, display


def issue_from_json(payload: Mapping[str, Any]) -> Issue:
    key = payload.get("key")
    fields = payload.get("fields")
    if not isinstance(key, str) or not isinstance(fields, dict):
        raise TrackerError("cannot read Jira issue: response is missing key or fields")
    if not ISSUE_KEY_RE.fullmatch(key):
        raise TrackerError(f"cannot read Jira issue: unexpected key {key}")
    summary = fields.get("summary")
    status_raw = fields.get("status")
    status = status_raw if isinstance(status_raw, dict) else {}
    type_raw = fields.get("issuetype")
    issue_type = type_raw if isinstance(type_raw, dict) else {}
    status_name = status.get("name")
    type_name = issue_type.get("name")
    created = fields.get("created")
    if (
        not isinstance(summary, str)
        or summary == ""
        or not isinstance(status_name, str)
        or status_name == ""
        or not isinstance(type_name, str)
        or type_name == ""
        or not isinstance(created, str)
        or created == ""
    ):
        raise TrackerError(
            f"cannot read Jira issue {key}: "
            "missing summary, status, issuetype, or created"
        )
    labels = fields.get("labels") or []
    if not isinstance(labels, list) or not all(
        isinstance(label, str) for label in labels
    ):
        raise TrackerError(
            f"cannot read Jira issue {key}: labels are not a list of strings"
        )
    parent_raw = fields.get("parent")
    parent = parent_raw if isinstance(parent_raw, dict) else {}
    parent_key_raw = parent.get("key")
    parent_key = parent_key_raw if isinstance(parent_key_raw, str) else ""
    assignee_id, assignee_name = account_identity(fields.get("assignee"))
    reporter_id, reporter_name = account_identity(fields.get("reporter"))
    return Issue(
        key=key,
        summary=summary,
        status=status_name,
        labels=tuple(labels),
        description_text=adf_to_text(fields.get("description")),
        assignee=fields.get("assignee") is not None,
        created=parse_time(created),
        issue_type=type_name,
        parent_key=parent_key,
        assignee_id=assignee_id,
        assignee_name=assignee_name,
        reporter_id=reporter_id,
        reporter_name=reporter_name,
    )


def with_status(issue: Issue, status: str) -> Issue:
    return replace(issue, status=status)


def comment_text(comment: Mapping[str, Any]) -> str:
    body = comment.get("body")
    if isinstance(body, str):
        return body
    return adf_to_text(body)


def is_human_comment(comment: Mapping[str, Any]) -> bool:
    if AUTOMATION_MARK in comment_text(comment):
        return False
    author = comment.get("author")
    if not isinstance(author, dict):
        return False
    return author.get("accountType") != "app"


def human_comment_count(comments: Sequence[Mapping[str, Any]]) -> int:
    return sum(1 for comment in comments if is_human_comment(comment))


def latest_activity(
    issue: Issue,
    comments: Sequence[Mapping[str, Any]],
    histories: Sequence[Mapping[str, Any]],
) -> datetime:
    times = [issue.created]
    for comment in comments:
        created = comment.get("created")
        if isinstance(created, str):
            times.append(parse_time(created))
    for history in histories:
        created = history.get("created")
        if isinstance(created, str):
            times.append(parse_time(created))
    return max(times)


def cvss_text(advisory: Mapping[str, Any]) -> str:
    severities = advisory.get("cvss_severities")
    if isinstance(severities, dict):
        for key in ("cvss_v4", "cvss_v3"):
            item = severities.get(key)
            if isinstance(item, dict) and item.get("vector_string"):
                score = item.get("score")
                vector = str(item["vector_string"])
                if score is None:
                    return vector
                return f"{vector} ({score})"
    cvss = advisory.get("cvss")
    if isinstance(cvss, dict) and cvss.get("vector_string"):
        return str(cvss["vector_string"])
    return "not set"


def affected_text(advisory: Mapping[str, Any]) -> str:
    vulns = advisory.get("vulnerabilities")
    if not isinstance(vulns, list):
        return "not set"
    parts: list[str] = []
    for vuln in vulns:
        if not isinstance(vuln, dict):
            continue
        package_raw = vuln.get("package")
        package = package_raw if isinstance(package_raw, dict) else {}
        package_name = package.get("name")
        name = (
            package_name
            if isinstance(package_name, str) and package_name
            else "unspecified package"
        )
        version = vuln.get("vulnerable_version_range")
        if not isinstance(version, str) or version == "":
            version = "unspecified versions"
        parts.append(f"{name} {version}")
    return "; ".join(parts) if parts else "not set"


def reporter_text(advisory: Mapping[str, Any]) -> str:
    credits = advisory.get("credits")
    if not isinstance(credits, list):
        return "not set"
    names = [
        str(credit["login"])
        for credit in credits
        if isinstance(credit, dict) and credit.get("login")
    ]
    return ", ".join(names) if names else "not set"


def description_doc(row: AdvisoryRow, advisory: Mapping[str, Any]) -> dict[str, Any]:
    cve = row.cve_id or "not set"
    paragraphs = [
        f"GitHub-Advisory: {row.ghsa_id}",
        f"Link: {row.url}",
        f"Summary: {row.summary}",
        f"CVE: {cve}",
        f"Severity: {row.severity or 'not set'}",
        f"CVSS: {cvss_text(advisory)}",
        f"Reporter: {reporter_text(advisory)}",
        f"Affected: {affected_text(advisory)}",
        "Report:",
    ]
    description = advisory.get("description")
    if isinstance(description, str):
        paragraphs.extend(description.splitlines())
    return adf_doc(paragraphs)


def create_fields(
    config: Config, row: AdvisoryRow, advisory: Mapping[str, Any]
) -> dict[str, Any]:
    if row.codename is None:
        raise TrackerError(
            f"cannot create a ticket for {row.ghsa_id}: missing codename"
        )
    fields = {
        "project": {"key": config.project_key},
        "parent": {"key": config.parent_key},
        "issuetype": {"name": config.issue_type},
        "summary": row.codename,
        "labels": [row.ghsa_id],
        "description": description_doc(row, advisory),
    }
    priority = config.priority_map.get(row.severity)
    if priority:
        fields["priority"] = {"name": priority}
    return fields


def next_link(header: str) -> str | None:
    for part in header.split(","):
        if 'rel="next"' not in part:
            continue
        start = part.find("<")
        end = part.find(">")
        if start != -1 and end > start:
            return part[start + 1 : end]
    return None


def repository_name(env: Mapping[str, str]) -> str:
    repo = env.get("GITHUB_REPOSITORY") or "canonical/snapd"
    if not REPO_RE.fullmatch(repo):
        raise TrackerError(f"cannot list advisories: unexpected repository {repo}")
    return repo


def github_token(env: Mapping[str, str]) -> str:
    token = env.get("SNAPD_ADVISORY_GITHUB_TOKEN") or env.get("GITHUB_TOKEN") or ""
    if token == "":
        raise TrackerError(
            "cannot list advisories: set SNAPD_ADVISORY_GITHUB_TOKEN or GITHUB_TOKEN"
        )
    return token


def _github_headers(token: str) -> dict[str, str]:
    return {
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github+json",
        "User-Agent": "snapd-advisory-tracker",
        "X-GitHub-Api-Version": "2022-11-28",
    }


def jira_settings(env: Mapping[str, str]) -> tuple[str, str]:
    email = env.get("JIRA_EMAIL") or ""
    token = env.get("JIRA_API_TOKEN") or ""
    if email == "" or token == "":
        raise TrackerError("cannot reach Jira: set JIRA_EMAIL and JIRA_API_TOKEN")
    base = (env.get("JIRA_BASE_URL") or "https://warthogs.atlassian.net").rstrip("/")
    raw = f"{email}:{token}".encode()
    return base, "Basic " + base64.b64encode(raw).decode("ascii")


def _json_bytes(payload: Mapping[str, Any] | None) -> bytes | None:
    if payload is None:
        return None
    return json.dumps(payload).encode("utf-8")


def _decode_json(result: HttpResult) -> Any:
    if result.body.strip() == b"":
        return {}
    try:
        return json.loads(result.body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise TrackerError(f"cannot read response: HTTP {result.status}") from exc


class Tracker:
    def __init__(
        self,
        config: Config,
        env: Mapping[str, str],
        client: HttpClient,
        now: datetime,
        name_source: Callable[[], str] | None = None,
    ) -> None:
        self.config = config
        self.env = env
        self.client = client
        self.now = now
        self.name_source = name_source or petname_words
        self.repo = repository_name(env)
        self.base, self.auth = jira_settings(env)
        self.token = github_token(env)
        self.permissions = permissions_from_env(env)
        self._parent_epic: Issue | None = None
        self._parent_epic_error: str | None = None
        self._parent_epic_loaded = False
        self.pending_stalls: list[PendingStall] = []
        self.pending_not_ready: list[PendingNotReady] = []
        self.maintenance_rows: list[AdvisoryRow] = []

    def github(self, url: str) -> HttpResult:
        return self.client.request("GET", url, _github_headers(self.token), None)

    def update_advisory_summary(self, ghsa_id: str, summary: str) -> None:
        headers = _github_headers(self.token)
        headers["Content-Type"] = "application/json"
        result = self.client.request(
            "PATCH",
            f"{GITHUB_API}/repos/{self.repo}/security-advisories/{ghsa_id}",
            headers,
            _json_bytes({"summary": summary}),
        )
        if result.status in {401, 403}:
            raise TrackerError(
                f"cannot update advisory summary: "
                f"GitHub returned HTTP {result.status}. "
                "Set SNAPD_ADVISORY_GITHUB_TOKEN with write access "
                "to repository security advisories",
                result.status,
            )
        if result.status != 200:
            raise TrackerError(
                f"cannot update advisory summary: HTTP {result.status}", result.status
            )

    def jira(
        self,
        method: str,
        path: str,
        payload: Mapping[str, Any] | None = None,
        ok: tuple[int, ...] = (200, 201, 204),
    ) -> Any:
        result = self.client.request(
            method,
            self.base + path,
            {
                "Authorization": self.auth,
                "Accept": "application/json",
                "Content-Type": "application/json",
                "User-Agent": "snapd-advisory-tracker",
            },
            _json_bytes(payload),
        )
        if result.status not in ok:
            raise TrackerError(
                f"cannot call Jira {method} {path}: HTTP {result.status}",
                result.status,
            )
        return _decode_json(result)

    def fetch_advisories(self) -> list[dict[str, Any]]:
        url = f"{GITHUB_API}/repos/{self.repo}/security-advisories?per_page=100"
        advisories: list[dict[str, Any]] = []
        for _ in range(20):
            result = self.github(url)
            if result.status == 403:
                raise TrackerError(
                    "cannot list advisories: GitHub returned HTTP 403. "
                    "Set SNAPD_ADVISORY_GITHUB_TOKEN with access "
                    "to repository security advisories",
                    403,
                )
            if result.status != 200:
                raise TrackerError(
                    f"cannot list advisories: HTTP {result.status}", result.status
                )
            payload = _decode_json(result)
            if not isinstance(payload, list):
                raise TrackerError("cannot list advisories: response was not a list")
            for item in payload:
                if isinstance(item, dict):
                    advisories.append(item)
            url = next_link(result.headers.get("link", "")) or ""
            if url == "":
                return advisories
        raise TrackerError("cannot list advisories: pagination did not end")

    def search(self, jql: str) -> list[Issue]:
        issues: list[Issue] = []
        page_token = ""
        for _ in range(20):
            payload: dict[str, Any] = {
                "jql": jql,
                "maxResults": 50,
                "fields": list(ISSUE_FIELDS),
            }
            if page_token:
                payload["nextPageToken"] = page_token
            result = self.jira("POST", "/rest/api/3/search/jql", payload)
            if not isinstance(result, dict):
                raise TrackerError("cannot search Jira: response was not an object")
            batch = result.get("issues") or []
            if not isinstance(batch, list):
                raise TrackerError("cannot search Jira: issues was not a list")
            issues.extend(
                issue_from_json(item) for item in batch if isinstance(item, dict)
            )
            token = result.get("nextPageToken")
            if not isinstance(token, str) or token == "":
                return issues
            page_token = token
        raise TrackerError("cannot search Jira: pagination did not end")

    def get_issue(self, key: str) -> Issue:
        fields = ",".join(ISSUE_FIELDS)
        payload = self.jira("GET", f"/rest/api/3/issue/{key}?fields={fields}")
        if not isinstance(payload, dict):
            raise TrackerError(f"cannot read Jira issue {key}")
        return issue_from_json(payload)

    def get_property(self, key: str) -> dict[str, Any] | None:
        result = self.client.request(
            "GET",
            f"{self.base}/rest/api/3/issue/{key}/properties/{PROPERTY_KEY}",
            {
                "Authorization": self.auth,
                "Accept": "application/json",
                "User-Agent": "snapd-advisory-tracker",
            },
            None,
        )
        if result.status == 404:
            return None
        if result.status != 200:
            raise TrackerError(
                f"cannot read advisory property for {key}: HTTP {result.status}",
                result.status,
            )
        payload = _decode_json(result)
        value = payload.get("value") if isinstance(payload, dict) else None
        if not isinstance(value, dict):
            return {"advisory_state": None, "stalls": {}, "not_ready": {}}
        state = (
            value.get("advisory_state")
            if isinstance(value.get("advisory_state"), str)
            else None
        )
        return {
            "advisory_state": state,
            "stalls": _clean_stalls(value.get("stalls")),
            "not_ready": _clean_not_ready(value.get("not_ready")),
        }

    def put_property(self, key: str, value: Mapping[str, Any]) -> None:
        self.jira("PUT", f"/rest/api/3/issue/{key}/properties/{PROPERTY_KEY}", value)

    def comments(self, key: str) -> list[dict[str, Any]]:
        return self._pages(key, "comment", "comments")

    def changelog(self, key: str) -> list[dict[str, Any]]:
        return self._pages(key, "changelog", "values", alternate="histories")

    def _pages(
        self, key: str, leaf: str, field: str, alternate: str = ""
    ) -> list[dict[str, Any]]:
        items: list[dict[str, Any]] = []
        start = 0
        while start < 2000:
            payload = self.jira(
                "GET", f"/rest/api/3/issue/{key}/{leaf}?startAt={start}&maxResults=100"
            )
            if not isinstance(payload, dict):
                raise TrackerError(f"cannot read {leaf} for {key}")
            batch = payload.get(field)
            if batch is None and alternate:
                batch = payload.get(alternate)
            if not isinstance(batch, list):
                raise TrackerError(f"cannot read {leaf} for {key}")
            page = [item for item in batch if isinstance(item, dict)]
            items.extend(page)
            total = payload.get("total")
            start += len(page)
            if not page:
                return items
            # A short page is the end only when Jira did not report a total.
            # Otherwise keep reading until that total is reached.
            if isinstance(total, int):
                if start >= total:
                    return items
                continue
            if len(page) < 100:
                return items
        raise TrackerError(f"cannot read {leaf} for {key}: result was truncated")

    def transitions(self, key: str) -> list[tuple[str, str]]:
        payload = self.jira("GET", f"/rest/api/3/issue/{key}/transitions")
        if not isinstance(payload, dict):
            raise TrackerError(f"cannot read transitions for {key}")
        found: list[tuple[str, str]] = []
        for item in payload.get("transitions") or []:
            if not isinstance(item, dict):
                continue
            ident = item.get("id")
            if isinstance(ident, int):
                ident = str(ident)
            target_raw = item.get("to")
            target = target_raw if isinstance(target_raw, dict) else {}
            target_name = target.get("name")
            name = target_name if isinstance(target_name, str) else item.get("name")
            if isinstance(ident, str) and isinstance(name, str):
                found.append((ident, name))
        return found

    def transition(self, key: str, ident: str) -> None:
        self.jira(
            "POST", f"/rest/api/3/issue/{key}/transitions", transition_payload(ident)
        )

    def add_comment(self, key: str, body: Mapping[str, Any]) -> None:
        self.jira("POST", f"/rest/api/3/issue/{key}/comment", {"body": dict(body)})

    def parent_epic(self) -> Issue | None:
        if self._parent_epic_loaded:
            return self._parent_epic
        try:
            self._parent_epic = self.get_issue(self.config.parent_key)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            self._parent_epic_error = str(exc)
            self._parent_epic = None
        self._parent_epic_loaded = True
        return self._parent_epic

    def create(self, fields: Mapping[str, Any]) -> Issue:
        payload = self.jira("POST", "/rest/api/3/issue", {"fields": dict(fields)})
        if not isinstance(payload, dict) or not isinstance(payload.get("key"), str):
            raise TrackerError(
                "cannot create Jira issue: response did not include a key"
            )
        return self.get_issue(payload["key"])


def _row_from_advisory(
    advisory: Mapping[str, Any], config: Config, repo: str
) -> AdvisoryRow:
    ghsa = advisory.get("ghsa_id")
    ghsa_id = ghsa if isinstance(ghsa, str) else ""
    summary_raw = advisory.get("summary")
    summary = summary_raw if isinstance(summary_raw, str) else ""
    state_raw = advisory.get("state")
    state = state_raw if isinstance(state_raw, str) else ""
    severity = advisory.get("severity")
    severity_text = severity.strip().lower() if isinstance(severity, str) else ""
    cve_raw = advisory.get("cve_id")
    cve = cve_raw if isinstance(cve_raw, str) else None
    url_raw = advisory.get("html_url")
    url = url_raw if isinstance(url_raw, str) else ""
    if url == "" and GHSA_RE.fullmatch(ghsa_id):
        url = f"https://github.com/{repo}/security/advisories/{ghsa_id}"
    reasons = readiness_reasons(advisory, config)
    return AdvisoryRow(
        ghsa_id=ghsa_id,
        url=url,
        cve_id=cve,
        summary=summary,
        codename=extract_codename(summary) if summary else None,
        state=state,
        severity=severity_text,
        expected_status=config.state_map.get(state),
        ready=not reasons,
        reasons=reasons,
        action="",
        issue_key=None,
        issue_keys=[],
        issue_status=None,
        status_aligned=False,
        stalls=[],
        last_activity=None,
        notifications=[],
        error=None,
    )


def _find_matches(
    tracker: Tracker, children: list[Issue], ghsa_id: str, codename: str | None
) -> list[Issue]:
    jql = (
        f'project = "{tracker.config.project_key}" AND '
        f'(labels = "{ghsa_id}" OR text ~ "{ghsa_id}")'
    )
    parent_key = tracker.config.parent_key
    found = list(children)
    found.extend(tracker.search(jql))
    matches = [
        issue
        for issue in found
        if issue.key != parent_key and issue_references(issue, ghsa_id)
    ]
    if codename:
        matches.extend(
            issue
            for issue in children
            if issue.key != parent_key and issue.summary == codename
        )
    return matches


def epic_children(issues: list[Issue], parent_key: str) -> list[Issue]:
    return [issue for issue in issues if issue.parent_key == parent_key]


def outside_parent_error(ghsa_id: str, outside: list[Issue], parent_key: str) -> str:
    keys = ", ".join(issue.key for issue in outside)
    if len(outside) == 1:
        relation = "associated Jira ticket is not a child"
    else:
        relation = "associated Jira tickets are not children"
    return f"{ghsa_id} {keys}: {relation} of {parent_key}"


def _split_listing(children: list[Issue]) -> str:
    return ", ".join(f"{issue.key} {issue.status}" for issue in children)


def _slowest(children: list[Issue], rank: Mapping[str, int]) -> Issue:
    return min(children, key=lambda issue: (rank[issue.status], issue.key))


def _split_gap(
    row: AdvisoryRow, children: list[Issue], implied: str, slowest: Issue
) -> str:
    return (
        f"{row.ghsa_id} {_split_listing(children)}: advisory state {row.state} "
        f"!= least progressed {implied} ({slowest.key}), "
        f"expected {row.expected_status or ''}"
    )


def _report_split(
    report: Report,
    row: AdvisoryRow,
    children: list[Issue],
    progress: Mapping[str, str],
    mode: str,
) -> None:
    unknown = next((issue for issue in children if issue.status not in progress), None)
    if unknown is not None:
        row.action = "error"
        row.issue_status = unknown.status
        row.status_aligned = False
        _remember_error(
            report,
            row,
            f"{row.ghsa_id} {_split_listing(children)}: "
            f"Jira status {unknown.status} is not in status_progress",
        )
        return
    rank = {name: index for index, name in enumerate(progress)}
    slowest = _slowest(children, rank)
    implied = progress[slowest.status]
    row.issue_status = slowest.status
    row.status_aligned = row.state == implied
    if row.status_aligned:
        row.action = "tracked" if mode == "summary" else "unchanged"
        return
    row.action = "error"
    _remember_error(report, row, _split_gap(row, children, implied, slowest))


def _store_transition(tracker: Tracker, issue: Issue, advisory_state: str) -> None:
    prop = tracker.get_property(issue.key)
    stored = prop.get("advisory_state") if prop is not None else None
    if not isinstance(stored, str):
        stored = None
    if not should_store_state(stored, advisory_state, True, True):
        return
    stalls = (
        {}
        if prop is None or not isinstance(prop.get("stalls"), dict)
        else dict(prop["stalls"])
    )
    tracker.put_property(issue.key, _property_body(prop, advisory_state, stalls))


def _transition_forward(
    tracker: Tracker, issue: Issue, expected_status: str, advisory_state: str
) -> tuple[Issue, bool]:
    choices = tracker.transitions(issue.key)
    ident = next((item_id for item_id, name in choices if name == expected_status), "")
    if ident == "":
        return issue, False
    tracker.transition(issue.key, ident)
    moved = with_status(issue, expected_status)
    _store_transition(tracker, moved, advisory_state)
    return moved, True


def _sync_split(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    children: list[Issue],
    writes: Permissions,
    mode: str,
) -> list[Issue]:
    progress = tracker.config.status_progress
    unknown = next((issue for issue in children if issue.status not in progress), None)
    if unknown is not None or mode == "summary":
        _report_split(report, row, children, progress, mode)
        return children
    target = row.expected_status
    if not isinstance(target, str) or target not in progress:
        row.action = "error"
        row.status_aligned = False
        detail = (
            f"advisory state {row.state} is not in the state map"
            if not isinstance(target, str)
            else f"Jira status {target} is not in status_progress"
        )
        _remember_error(report, row, detail)
        return children
    rank = {name: index for index, name in enumerate(progress)}
    slowest_rank = min(rank[issue.status] for issue in children)
    # Move only tickets on the least-progressed status, and only forward.
    # A ticket that is already further along stays where it is.
    if _sync_is_disabled(tracker, report):
        slowest = _slowest(children, rank)
        implied = progress[slowest.status]
        row.issue_status = slowest.status
        row.status_aligned = row.state == implied
        if row.action == "created":
            return children
        if slowest_rank >= rank[target] and row.status_aligned:
            row.action = "unchanged"
        else:
            row.action = "sync_disabled"
        return children
    if slowest_rank >= rank[target]:
        _report_split(report, row, children, progress, mode)
        return children
    if not writes.sync:
        _report_split(report, row, children, progress, mode)
        row.action = "would_transition"
        return children
    updated: list[Issue] = []
    failed = False
    for issue in children:
        if rank[issue.status] != slowest_rank:
            updated.append(issue)
            continue
        moved, ok = _transition_forward(tracker, issue, target, row.state)
        updated.append(moved)
        failed = failed or not ok
    slowest = _slowest(updated, rank)
    implied = progress[slowest.status]
    row.issue_status = slowest.status
    row.status_aligned = row.state == implied and not failed
    message = _split_gap(row, updated, implied, slowest)
    if failed:
        row.action = "error"
        _remember_error(report, row, f"{message}: no transition")
        return updated
    if row.state != implied:
        row.action = "error"
        _remember_error(report, row, message)
        return updated
    row.action = "transitioned"
    return updated


def _sync_is_disabled(tracker: Tracker, report: Report) -> bool:
    return not tracker.permissions.sync and report.confirmation_error is None


def _mark_notify_disabled(row: AdvisoryRow) -> None:
    if "notify disabled" not in row.notifications:
        row.notifications.append("notify disabled")


def _remember_error(report: Report, row: AdvisoryRow, message: str) -> None:
    row.error = message
    if message not in report.errors:
        report.errors.append(message)


def _sync_status(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    issue: Issue,
    writes: Permissions,
    mode: str,
) -> Issue:
    prior = row.action
    target = row.expected_status
    if not isinstance(target, str):
        row.action = "error"
        _remember_error(
            report, row, f"advisory state {row.state} is not in the state map"
        )
        return issue
    # One ticket must match the mapped status exactly. In Review on a draft
    # advisory is moved to In Progress.
    row.issue_status = issue.status
    row.status_aligned = issue.status == target
    if mode == "summary":
        if not row.status_aligned:
            row.action = "error"
            _remember_error(report, row, sync_mismatch(row, issue))
        elif prior != "created":
            row.action = "tracked"
        return issue
    prop = tracker.get_property(issue.key)
    stored = prop.get("advisory_state") if prop is not None else None
    if not isinstance(stored, str):
        stored = None
    required = transition_needed(issue.status, target)
    transitioned = False
    if required and not writes.sync:
        if _sync_is_disabled(tracker, report):
            row.action = "sync_disabled"
        else:
            row.action = "would_transition"
    elif required:
        issue, transitioned = _transition_forward(tracker, issue, target, row.state)
        row.action = "transitioned" if transitioned else "error"
    else:
        row.action = "unchanged"
    if prior == "created" and row.action in {
        "unchanged",
        "transitioned",
        "error",
        "sync_disabled",
    }:
        row.action = "created"
    if row.action == "sync_disabled" or (
        prior == "created" and required and _sync_is_disabled(tracker, report)
    ):
        return issue
    if (
        writes.sync
        and not transitioned
        and should_store_state(stored, row.state, required, transitioned)
    ):
        stalls = (
            {}
            if prop is None or not isinstance(prop.get("stalls"), dict)
            else dict(prop["stalls"])
        )
        tracker.put_property(issue.key, _property_body(prop, row.state, stalls))
    row.issue_status = issue.status
    row.status_aligned = issue.status == target
    if not row.status_aligned:
        message = sync_mismatch(row, issue)
        if required and writes.sync and not transitioned:
            message = f"{message}: no transition"
        _remember_error(report, row, message)
        if row.action not in {"created", "would_transition"}:
            row.action = "error"
    return issue


def unmet_label(stage: Stage) -> str:
    return f"not {stage.name.replace('_', ' ')}"


def stall_title(stage: Stage) -> str:
    return f"{AUTOMATION_MARK} {unmet_label(stage)}"


def stall_detail_lines(issue: Issue, row: AdvisoryRow, stage: Stage) -> list[str]:
    name = public_label(row)
    return [
        f"Warning: {issue.key} ({name}): {stall_reason(stage, row.severity)}",
        f"GitHub-Advisory: {row.ghsa_id}",
        f"Link: {row.url}",
    ]


def stall_comment_doc(
    title: str, lines: list[str], mention: tuple[str, str] | None
) -> dict[str, Any]:
    content: list[dict[str, Any]] = [
        {
            "type": "panel",
            "attrs": {"panelType": "warning"},
            "content": [
                {
                    "type": "heading",
                    "attrs": {"level": 4},
                    "content": [{"type": "text", "text": title}],
                }
            ],
        }
    ]
    if mention is not None:
        account_id, display = mention
        content.append(
            {
                "type": "paragraph",
                "content": [
                    {
                        "type": "mention",
                        "attrs": {"id": account_id, "text": f"@{display}"},
                    }
                ],
            }
        )
    for line in lines:
        content.append(
            {"type": "paragraph", "content": [{"type": "text", "text": line}]}
        )
    return {"type": "doc", "version": 1, "content": content}


def named_account(account_id: str | None, name: str | None) -> tuple[str, str] | None:
    if account_id is None or account_id == "":
        return None
    return account_id, name or account_id


def _webhook_url(tracker: Tracker, name: str) -> str:
    url = tracker.env.get(name) or ""
    if url == "":
        raise TrackerError(f"cannot send mattermost message: missing {name}")
    return url


def run_summary_url(env: Mapping[str, str]) -> str:
    run_id = env.get("GITHUB_RUN_ID") or ""
    if run_id == "":
        raise TrackerError("cannot send mattermost message: missing GITHUB_RUN_ID")
    server = (env.get("GITHUB_SERVER_URL") or "https://github.com").rstrip("/")
    repo = env.get("GITHUB_REPOSITORY") or ""
    if repo == "":
        raise TrackerError("cannot send mattermost message: missing GITHUB_REPOSITORY")
    return f"{server}/{repo}/actions/runs/{run_id}"


def maintenance_message(env: Mapping[str, str]) -> str:
    return "\n".join(
        [f"{AUTOMATION_MARK} maintenance", f"Link: {run_summary_url(env)}"]
    )


def _send(
    tracker: Tracker,
    channel: str,
    message: str,
    issue_key: str | None,
    mention: tuple[str, str] | None = None,
    title: str | None = None,
    lines: list[str] | None = None,
    webhook: str = MAINTENANCE_WEBHOOK,
) -> None:
    if channel == "mattermost":
        url = _webhook_url(tracker, webhook)
        result = tracker.client.request(
            "POST",
            url,
            {
                "Content-Type": "application/json",
                "User-Agent": "snapd-advisory-tracker",
            },
            _json_bytes({"text": message}),
        )
        if result.status >= 300:
            raise TrackerError(
                f"cannot send mattermost message: HTTP {result.status}", result.status
            )
        return
    if channel == "jira_comment":
        if issue_key is None:
            raise TrackerError(
                "cannot comment on a jira ticket: advisory has no ticket"
            )
        if title is None:
            body = adf_doc(message.splitlines())
        else:
            body = stall_comment_doc(title, lines or [], mention)
        tracker.add_comment(issue_key, body)
        return
    raise TrackerError(f"cannot notify: unknown channel {channel}")


def _notify(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    channels: tuple[str, ...],
    message: str,
    issue_key: str | None,
    label: str,
    writes: Permissions,
    mention: tuple[str, str] | None = None,
    title: str | None = None,
    lines: list[str] | None = None,
    webhook: str = MAINTENANCE_WEBHOOK,
) -> tuple[str, ...]:
    sent: list[str] = []
    for channel in channels:
        if not writes.notify:
            row.notifications.append(f"would {channel} {label}")
            continue
        try:
            _send(
                tracker,
                channel,
                message,
                issue_key,
                mention,
                title,
                lines,
                webhook,
            )
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            row.error = str(exc)
            report.errors.append(f"{row.ghsa_id}: {exc}")
            continue
        row.notifications.append(f"sent {channel} {label}")
        sent.append(channel)
    return tuple(sent)


def stall_mention(
    tracker: Tracker, report: Report, row: AdvisoryRow, issue: Issue
) -> tuple[str, str] | None:
    chosen = named_account(issue.assignee_id, issue.assignee_name)
    if chosen is not None:
        return chosen
    if issue.assignee:
        return None
    epic = tracker.parent_epic()
    if tracker._parent_epic_error:
        _remember_error(report, row, tracker._parent_epic_error)
        return None
    if epic is None:
        return None
    return named_account(epic.assignee_id, epic.assignee_name) or named_account(
        epic.reporter_id, epic.reporter_name
    )


def _add_stall(row: AdvisoryRow, issue_key: str, reason: str) -> None:
    for stall in row.stalls:
        if stall.issue_key == issue_key:
            if reason not in stall.reasons:
                stall.reasons.append(reason)
            return
    row.stalls.append(Stall(issue_key, [reason]))


def stall_progress(stalls: list[Stall]) -> str:
    return ", ".join(
        f"{stall.issue_key}: {', '.join(stall.reasons)}" for stall in stalls
    )


def _record_stalls(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    issue: Issue,
    writes: Permissions,
    mode: str,
) -> None:
    stages = tracker.config.stages
    if stages and requirement_met(stages[-1], issue, 0):
        if row.last_activity is None:
            row.last_activity = issue.created.isoformat()
        return
    comments = tracker.comments(issue.key)
    histories = tracker.changelog(issue.key)
    row.last_activity = latest_activity(issue, comments, histories).isoformat()
    late = behind_stages(
        issue, human_comment_count(comments), tracker.now, tracker.config, row.severity
    )
    for reason in (stall_reason(stage, row.severity) for stage in late):
        _add_stall(row, issue.key, reason)
    if not late or mode == "summary":
        return
    if not tracker.permissions.notify:
        _mark_notify_disabled(row)
        return
    prop = tracker.get_property(issue.key)
    stamps = (
        {}
        if prop is None or not isinstance(prop.get("stalls"), dict)
        else dict(prop["stalls"])
    )
    state = (
        prop.get("advisory_state")
        if prop is not None and isinstance(prop.get("advisory_state"), str)
        else None
    )
    days = tracker.config.stall_cooldown_days
    channels = tracker.config.notify["stall"]
    dirty = False
    for stage in late:
        current = stamps.get(stage.name)
        cooled = [
            channel
            for channel in channels
            if _channel_cooled(current, channel, tracker.now, days)
        ]
        pending = tuple(channel for channel in channels if channel not in cooled)
        if channels and not pending:
            row.notifications.append(f"cooldown {stage.name}")
            continue
        for channel in cooled:
            row.notifications.append(f"cooldown {channel} {stage.name}")
        if not pending:
            continue
        immediate = tuple(channel for channel in pending if channel != "mattermost")
        if "mattermost" in pending:
            _remember_maintenance(tracker, row)
            if writes.notify:
                tracker.pending_stalls.append(PendingStall(row, issue.key, stage.name))
        if not immediate:
            continue
        mention = stall_mention(tracker, report, row, issue)
        lines = stall_detail_lines(issue, row, stage)
        title = stall_title(stage)
        sent = _notify(
            tracker,
            report,
            row,
            immediate,
            "",
            issue.key,
            stage.name,
            writes,
            mention,
            title,
            lines,
        )
        if writes.notify and sent:
            stamps[stage.name] = _merge_channel_stamps(
                current, sent, tracker.now.isoformat()
            )
            dirty = True
    if dirty:
        tracker.put_property(issue.key, _property_body(prop, state, stamps))


def execute(
    config: Config,
    env: Mapping[str, str],
    client: HttpClient,
    now: datetime,
    command: str,
    dry_run: bool,
    name_source: Callable[[], str] | None = None,
) -> Report:
    if command not in {"summary", "sync"}:
        raise TrackerError(f"cannot run advisory tracker: unknown command {command}")
    tracker = Tracker(config, env, client, now, name_source)
    mode = "summary" if command == "summary" else "dry-run" if dry_run else "apply"
    report = Report(config.parent_key, mode, None, [], [], tracker.permissions)
    advisories = tracker.fetch_advisories()
    children = tracker.search(f"parent = {config.parent_key}")
    report.confirmation_error = confirm_children(children, config)
    active = command == "sync" and not dry_run and report.confirmation_error is None
    writes = Permissions(
        active and tracker.permissions.create,
        active and tracker.permissions.notify,
        active and tracker.permissions.sync,
    )
    used = collect_used_codenames(advisories, children)
    for advisory in advisories:
        row = _row_from_advisory(advisory, config, tracker.repo)
        try:
            _process(tracker, report, row, advisory, children, writes, mode, used)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            row.action = row.action or "error"
            row.error = str(exc)
            report.errors.append(f"{row.ghsa_id or 'advisory'}: {exc}")
        report.advisories.append(row)
    if report.confirmation_error and command == "sync":
        report.errors.append(report.confirmation_error)
    _finish_maintenance(tracker, report, writes, command)
    return report


def _not_ready_stamp(prop: Mapping[str, Any] | None, ghsa_id: str | None) -> str | None:
    if prop is None:
        return None
    raw = prop.get("not_ready")
    if ghsa_id is None:
        return raw if isinstance(raw, str) else None
    if isinstance(raw, dict):
        stamp = raw.get(ghsa_id)
        return stamp if isinstance(stamp, str) else None
    return None


def _save_not_ready(
    tracker: Tracker,
    key: str,
    prop: Mapping[str, Any] | None,
    stamp: str,
    ghsa_id: str | None,
) -> None:
    state = None
    stalls: dict[str, str | dict[str, str]] = {}
    current: Any = None
    if prop is not None:
        stored = prop.get("advisory_state")
        state = stored if isinstance(stored, str) else None
        stalls = _clean_stalls(prop.get("stalls"))
        current = prop.get("not_ready")
    if ghsa_id is None:
        not_ready: str | dict[str, str] = stamp
    else:
        mapping = dict(current) if isinstance(current, dict) else {}
        mapping[ghsa_id] = stamp
        not_ready = mapping
    body = _property_body(None, state, stalls)
    body["not_ready"] = not_ready
    tracker.put_property(key, body)


def created_message(row: AdvisoryRow, issue_key: str) -> str:
    name = public_label(row)
    lines = [
        f"{issue_key} ({name}): Jira ticket created",
        f"GitHub-Advisory: {row.ghsa_id}",
        f"Link: {row.url}",
    ]
    return "\n".join([f"{AUTOMATION_MARK} new advisory detected", *lines])


def _announce_created(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    issue_key: str,
    writes: Permissions,
) -> None:
    if not tracker.permissions.notify:
        _mark_notify_disabled(row)
        return
    _notify(
        tracker,
        report,
        row,
        tracker.config.notify["created"],
        created_message(row, issue_key),
        issue_key,
        "created",
        writes,
        webhook=NEW_ADVISORY_WEBHOOK,
    )


def _remember_maintenance(tracker: Tracker, row: AdvisoryRow) -> None:
    if all(item is not row for item in tracker.maintenance_rows):
        tracker.maintenance_rows.append(row)


def _maintenance_targets(tracker: Tracker, report: Report) -> list[AdvisoryRow]:
    rows = list(tracker.maintenance_rows)
    for row in report.advisories:
        if row.error and all(item is not row for item in rows):
            rows.append(row)
    return rows


def _fail_maintenance(tracker: Tracker, report: Report, exc: TrackerError) -> None:
    message = str(exc)
    rows: list[AdvisoryRow] = []
    pending_rows = [item.row for item in tracker.pending_stalls]
    pending_rows.extend(item.row for item in tracker.pending_not_ready)
    for row in pending_rows:
        if all(item is not row for item in rows):
            rows.append(row)
    if not rows:
        if message not in report.errors:
            report.errors.append(message)
        return
    for row in rows:
        row.error = message
        prefixed = f"{row.ghsa_id}: {message}"
        if prefixed not in report.errors:
            report.errors.append(prefixed)


def _stamp_pending_stalls(tracker: Tracker, report: Report) -> None:
    grouped: dict[str, list[PendingStall]] = {}
    for pending in tracker.pending_stalls:
        grouped.setdefault(pending.issue_key, []).append(pending)
    stamp = tracker.now.isoformat()
    for issue_key, items in grouped.items():
        try:
            prop = tracker.get_property(issue_key)
            stalls = _clean_stalls(None if prop is None else prop.get("stalls"))
            state = None
            if prop is not None and isinstance(prop.get("advisory_state"), str):
                state = prop["advisory_state"]
            for pending in items:
                stalls[pending.stage_name] = _merge_channel_stamps(
                    stalls.get(pending.stage_name), ("mattermost",), stamp
                )
            tracker.put_property(issue_key, _property_body(prop, state, stalls))
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            for pending in items:
                _remember_error(report, pending.row, str(exc))
            continue
        for pending in items:
            pending.row.notifications.append(f"sent mattermost {pending.stage_name}")


def _stamp_pending_not_ready(tracker: Tracker, report: Report) -> None:
    stamp = tracker.now.isoformat()
    for pending in tracker.pending_not_ready:
        try:
            prop = tracker.get_property(pending.epic_key)
            _save_not_ready(tracker, pending.epic_key, prop, stamp, pending.ghsa_id)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            _remember_error(report, pending.row, str(exc))
            continue
        pending.row.notifications.append("sent mattermost not-ready")


def _finish_maintenance(
    tracker: Tracker,
    report: Report,
    writes: Permissions,
    command: str,
) -> None:
    # One maintenance post covers every due Mattermost stall and not-ready
    # alert, and any other error. The confirmation failure refuses every write.
    if command != "sync" or report.confirmation_error:
        return
    targets = _maintenance_targets(tracker, report)
    if not targets:
        return
    if not tracker.permissions.notify:
        for row in targets:
            _mark_notify_disabled(row)
        return
    if not writes.notify:
        if "would mattermost maintenance" not in targets[0].notifications:
            targets[0].notifications.append("would mattermost maintenance")
        return
    try:
        _send(
            tracker,
            "mattermost",
            maintenance_message(tracker.env),
            None,
            webhook=MAINTENANCE_WEBHOOK,
        )
    except TrackerError as exc:
        _fail_maintenance(tracker, report, exc)
        return
    _stamp_pending_stalls(tracker, report)
    _stamp_pending_not_ready(tracker, report)


def _page_not_ready(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    issue: Issue | None,
    writes: Permissions,
) -> None:
    # Not-ready is paged only for triage and draft, and at most once per
    # stall_cooldown_days. The stamp is always on the epic, keyed by GHSA, so
    # linking a ticket cannot reset it. A legacy string stamp on the ticket is
    # still honored. A failed read is reported and not sent.
    if row.state not in tracker.config.create_for_states:
        return
    if not tracker.permissions.notify:
        _mark_notify_disabled(row)
        return
    days = tracker.config.stall_cooldown_days
    epic_key = tracker.config.parent_key
    try:
        epic_prop = tracker.get_property(epic_key)
    except TrackerError as exc:
        if exc.status in {401, 403}:
            raise
        _remember_error(report, row, str(exc))
        return
    stamp = _not_ready_stamp(epic_prop, row.ghsa_id)
    if stamp is None and issue is not None:
        try:
            ticket_prop = tracker.get_property(issue.key)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            _remember_error(report, row, str(exc))
            return
        stamp = _not_ready_stamp(ticket_prop, None)
    if _stamp_is_current(stamp, tracker.now, days):
        row.notifications.append("cooldown not-ready")
        return
    channels = tracker.config.notify["not_ready"]
    immediate = tuple(channel for channel in channels if channel != "mattermost")
    if immediate:
        _notify(
            tracker,
            report,
            row,
            immediate,
            "",
            None,
            "not-ready",
            writes,
        )
    if "mattermost" not in channels:
        return
    _remember_maintenance(tracker, row)
    if writes.notify:
        tracker.pending_not_ready.append(PendingNotReady(row, epic_key, row.ghsa_id))


def _publish_codename(
    tracker: Tracker,
    row: AdvisoryRow,
    issues: list[Issue],
    writes: Permissions,
    mode: str,
    used: set[str],
) -> None:
    name = row.codename or petname_from_issues(issues)
    if name is None or not is_public_codename(name):
        return
    used.add(codename_key(name))
    if mode == "summary" or not writes.create:
        row.codename = name
        if mode != "summary" and tracker.permissions.create:
            row.notifications.append(f"would rename advisory {name}")
        elif mode != "summary":
            row.notifications.append("create disabled")
        return
    updated = summary_with_codename(row.summary, name)
    tracker.update_advisory_summary(row.ghsa_id, updated)
    row.summary = updated
    row.codename = name


def _process(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    advisory: Mapping[str, Any],
    children: list[Issue],
    writes: Permissions,
    mode: str,
    used: set[str],
) -> None:
    if not GHSA_RE.fullmatch(row.ghsa_id):
        row.action = "error"
        row.error = "advisory is missing a GHSA id"
        report.errors.append(row.error)
        return
    named_on_advisory = row.codename is not None
    related = select_issues(
        _find_matches(tracker, children, row.ghsa_id, row.codename),
        tracker.config.parent_key,
        row.codename,
    )[1]
    epic = epic_children(related, tracker.config.parent_key)
    outside = [item for item in related if item.parent_key != tracker.config.parent_key]
    issue = epic[0] if epic else None
    row.issue_keys = [item.key for item in related]
    if outside and not epic:
        row.action = "error"
        _remember_error(
            report,
            row,
            outside_parent_error(row.ghsa_id, outside, tracker.config.parent_key),
        )
        return
    if issue is None and row.state not in tracker.config.create_for_states:
        row.action = "skipped"
        return
    if issue is None and not row.ready:
        row.action = "not_ready"
        if mode != "summary":
            _page_not_ready(tracker, report, row, None, writes)
        return
    if issue is None:
        if mode == "summary":
            row.action = "untracked"
            return
        if not writes.create:
            if report.confirmation_error:
                row.action = "refused"
            elif tracker.permissions.create:
                row.action = "would_create"
                _announce_created(tracker, report, row, "new", writes)
            else:
                row.action = "create_disabled"
            return
        if row.codename is None:
            row.codename = choose_codename(tracker.name_source, used)
        issue = tracker.create(create_fields(tracker.config, row, advisory))
        children.append(issue)
        epic = [issue]
        related = [issue, *[item for item in related if item.key != issue.key]]
        row.issue_keys = [item.key for item in related]
        row.action = "created"
        row.issue_key = issue.key
        _announce_created(tracker, report, row, issue.key, writes)
    if not named_on_advisory:
        try:
            _publish_codename(tracker, row, related, writes, mode, used)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            _remember_error(report, row, str(exc))
    if (
        row.reasons
        and row.state in tracker.config.create_for_states
        and mode != "summary"
    ):
        _page_not_ready(tracker, report, row, issue, writes)
    row.issue_key = epic[0].key
    if len(epic) > 1:
        epic = _sync_split(tracker, report, row, epic, writes, mode)
    else:
        synced = _sync_status(tracker, report, row, epic[0], writes, mode)
        epic = [synced]
        row.issue_key = synced.key
        row.issue_status = synced.status
    for stalled in epic:
        if stalled.issue_type != tracker.config.issue_type:
            continue
        try:
            _record_stalls(tracker, report, row, stalled, writes, mode)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            _remember_error(report, row, str(exc))
    if outside:
        _remember_error(
            report,
            row,
            outside_parent_error(row.ghsa_id, outside, tracker.config.parent_key),
        )
        row.action = "error"


def _cell(value: Any) -> str:
    text = "" if value is None else str(value)
    return text.replace("|", "/").replace("\n", " ")


def settled_without_error(state: str, error: str | None) -> bool:
    return state in {"published", "closed"} and not error


def displayed_advisories(
    report: Report, omit_settled: bool = False
) -> list[AdvisoryRow]:
    if not omit_settled:
        return list(report.advisories)
    return [
        row
        for row in report.advisories
        if not settled_without_error(row.state, row.error)
    ]


def render_markdown(report: Report, omit_settled: bool = False) -> str:
    lines = [
        "# Advisory tracker",
        "",
        f"Parent: `{report.parent}`",
        f"Mode: {report.mode}",
        permission_line(report.permissions),
    ]
    visible = displayed_advisories(report, omit_settled)
    if omit_settled:
        hidden = len(report.advisories) - len(visible)
        lines.append(f"Omitting {hidden} published or closed advisories with no error.")
    lines.append("")
    if report.confirmation_error:
        lines.extend([f"Confirmation: {report.confirmation_error}", ""])
    else:
        lines.extend(["Confirmation: child issue type matches.", ""])
    lines.extend(
        [
            "| Advisory | State | Ticket | Status | Progress |",
            "| --- | --- | --- | --- | --- |",
        ]
    )
    for row in visible:
        name = public_label(row)
        progress = stall_progress(row.stalls) if row.stalls else row.action
        if row.error:
            progress = row.error
        lines.append(
            "| "
            + " | ".join(
                [
                    _cell(f"{name} {row.ghsa_id}"),
                    _cell(row.state),
                    _cell(
                        ", ".join(row.issue_keys)
                        if row.issue_keys
                        else (row.issue_key or "")
                    ),
                    _cell(row.issue_status or ""),
                    _cell(progress),
                ]
            )
            + " |"
        )
    not_ready = [row for row in visible if row.reasons]
    lines.extend(["", "## Not ready for triage", ""])
    if not not_ready:
        lines.append("None.")
    for row in not_ready:
        lines.append(f"- `{row.ghsa_id}`: {', '.join(row.reasons)}")
    lines.extend(["", "## Stalls", ""])
    stalled = [row for row in visible if row.stalls]
    if not stalled:
        lines.append("None.")
    for row in stalled:
        name = public_label(row)
        for stall in row.stalls:
            lines.append(f"- `{stall.issue_key}` {name}: {', '.join(stall.reasons)}")
    if report.errors:
        lines.extend(["", "## Errors", ""])
        for error in report.errors:
            lines.append(f"- {error}")
    lines.append("")
    return "\n".join(lines)


def render_json(report: Report) -> str:
    payload = asdict(report)
    advisories = payload["advisories"]
    if report.mode == "summary":
        advisories = [
            row
            for row in advisories
            if not settled_without_error(row.get("state") or "", row.get("error"))
        ]
        payload["omitted_advisories"] = len(report.advisories) - len(advisories)
    for row in advisories:
        row.pop("summary", None)
        row.pop("cve_id", None)
        codename = row.get("codename")
        if not isinstance(codename, str) or not is_public_codename(codename):
            row["codename"] = None
    payload["advisories"] = advisories
    return json.dumps(payload, indent=2) + "\n"


def write_step_summary(markdown: str, env: Mapping[str, str]) -> None:
    path = env.get("GITHUB_STEP_SUMMARY") or ""
    if path == "":
        return
    with open(path, "a", encoding="utf-8") as handle:
        handle.write(markdown)
        if not markdown.endswith("\n"):
            handle.write("\n")


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Track snapd GitHub advisories in Jira"
    )
    parser.add_argument("command", choices=("resolve-mode", "summary", "sync"))
    parser.add_argument("--config", default=default_config_path())
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--format", choices=("json", "markdown"), default="markdown")
    parser.add_argument("--output")
    return parser


def main(
    argv: list[str] | None = None,
    *,
    env: Mapping[str, str] | None = None,
    client: HttpClient | None = None,
    now: datetime | None = None,
) -> int:
    args = _parser().parse_args(sys.argv[1:] if argv is None else argv)
    environ = os.environ if env is None else env
    if args.command == "resolve-mode":
        try:
            print(resolve_mode(environ))
        except TrackerError as exc:
            print(f"::error::{exc}", file=sys.stderr)
            return 1
        return 0
    output_format = args.format if args.command == "summary" else "markdown"
    try:
        report = execute(
            load_config(args.config),
            environ,
            client or UrllibClient(),
            now or datetime.now(UTC),
            args.command,
            args.dry_run,
        )
    except TrackerError as exc:
        print(f"::error::{exc}", file=sys.stderr)
        return 1
    omit_stdout = args.command == "summary"
    markdown = render_markdown(report, omit_settled=omit_stdout)
    text = render_json(report) if output_format == "json" else markdown
    if output_format == "json":
        sys.stdout.write(text)
    else:
        sys.stdout.write(markdown)
    write_step_summary(render_markdown(report, omit_settled=True), environ)
    if args.output:
        with open(args.output, "w", encoding="utf-8") as handle:
            handle.write(text)
    for row in report.advisories:
        if row.reasons:
            print(
                f"::warning::{row.ghsa_id} is not ready: " f"{', '.join(row.reasons)}",
                file=sys.stderr,
            )
    for error in report.errors:
        print(f"::error::{error}", file=sys.stderr)
    if args.command == "summary":
        return 1 if any(row.error for row in report.advisories) else 0
    if report.confirmation_error:
        return 2
    if report.errors or any(row.error for row in report.advisories):
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
