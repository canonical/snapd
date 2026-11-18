#!/usr/bin/env python3

"""Create snapd GitHub security advisories as SNAPDENG Jira tickets.

Commands:
  resolve-mode  Print summary, dry-run, apply, or skip from the workflow event.
  summary       Read-only overview. Markdown by default, or JSON with --format json.
  sync          Create missing triage and draft tickets and announce them.
                --dry-run does not write.

A follow-up can continue after the petname is published on the advisory.
This command stops once the ticket exists and that announcement is done.

Environment:
  SNAPD_ADVISORY_GITHUB_TOKEN  preferred token for repository security advisories
  GITHUB_TOKEN                 fallback advisory token
  GITHUB_REPOSITORY            owner/name, default canonical/snapd
  JIRA_API_TOKEN               Jira Cloud API token
  JIRA_EMAIL                   account email for that token
  JIRA_BASE_URL                default https://warthogs.atlassian.net
                               Jira calls use atlassian-python-api
  MATTERMOST_NEW_ADVISORY_WEBHOOK_URL
                               incoming webhook for created tickets and for the
                               one notice when a ticket was not created
  GITHUB_SERVER_URL            server used in the not-created run link
  GITHUB_RUN_ID                Actions run id used in that link
"""

from __future__ import annotations

import argparse
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
from dataclasses import asdict, dataclass
from datetime import UTC, datetime
from typing import Any

GITHUB_API = "https://api.github.com"
JIRA_TIMEOUT_SECONDS = 60
AUTOMATION_MARK = "advisory-tracker:"
CHANNELS = ("mattermost",)
NEW_ADVISORY_WEBHOOK = "MATTERMOST_NEW_ADVISORY_WEBHOOK_URL"
NOTIFY_EVENTS = ("created", "not_created")
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
class Config:
    parent_key: str
    project_key: str
    issue_type: str
    create_for_states: tuple[str, ...]
    priority_map: dict[str, str]
    min_description_chars: int
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
class AdvisoryRow:
    ghsa_id: str
    url: str
    cve_id: str | None
    summary: str
    codename: str | None
    state: str
    severity: str
    ready: bool
    reasons: list[str]
    action: str
    issue_key: str | None
    issue_keys: list[str]
    issue_status: str | None
    notifications: list[str]
    error: str | None


@dataclass(frozen=True)
class Permissions:
    create: bool
    notify: bool


@dataclass
class Report:
    parent: str
    mode: str
    confirmation_error: str | None
    errors: list[str]
    advisories: list[AdvisoryRow]
    permissions: Permissions
    create_for_states: tuple[str, ...] = ("triage", "draft")


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
    priority_map = _string_map(raw.get("priority_map"), "priority_map")
    readiness = _require_mapping(raw.get("readiness"), "readiness")
    minimum = readiness.get("min_description_chars")
    if not isinstance(minimum, int) or minimum < 1:
        raise TrackerError(
            "cannot load config: readiness.min_description_chars "
            "must be a positive integer"
        )
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
        priority_map=priority_map,
        min_description_chars=minimum,
        notify=notify,
    )


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


def confirm_children(children: Sequence[Issue], config: Config) -> str | None:
    if not children:
        return "cannot confirm issue type: parent has no child tickets"
    if any(issue.issue_type == config.issue_type for issue in children):
        return None
    found = ", ".join(sorted({issue.issue_type for issue in children}))
    return (
        f"cannot confirm issue type: configured {config.issue_type} "
        f"does not match child issue type {found}"
    )


def _issue_rank(
    issue: Issue, parent_key: str, codename: str | None
) -> tuple[bool, bool, datetime, str]:
    named = codename is not None and issue.summary == codename
    child = issue.parent_key == parent_key
    return (not child, not named, issue.created, issue.key)


def select_issues(
    matches: Sequence[Issue], parent_key: str, codename: str | None
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
    )


def permission_line(permissions: Permissions) -> str:
    def word(enabled: bool) -> str:
        return "on" if enabled else "off"

    return (
        f"Permissions: create {word(permissions.create)}, "
        f"notify {word(permissions.notify)}"
    )


def resolve_mode(env: Mapping[str, str]) -> str:
    event = env.get("GITHUB_EVENT_NAME") or ""
    permissions = permissions_from_env(env)
    if event == "schedule":
        action = "sync"
        apply = permissions.create or permissions.notify
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
    fields: dict[str, Any] = {
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


def jira_settings(env: Mapping[str, str]) -> tuple[str, str, str]:
    email = env.get("JIRA_EMAIL") or ""
    token = env.get("JIRA_API_TOKEN") or ""
    if email == "" or token == "":
        raise TrackerError("cannot reach Jira: set JIRA_EMAIL and JIRA_API_TOKEN")
    base = (env.get("JIRA_BASE_URL") or "https://warthogs.atlassian.net").rstrip("/")
    return base, email, token


def connect_jira(url: str, email: str, token: str) -> Any:
    """Return a Jira Cloud v3 client (email + API token as Basic auth)."""
    try:
        from atlassian.jira import JiraCloud
    except ImportError as exc:
        raise TrackerError(
            "cannot import Jira Cloud client, please install atlassian-python-api"
        ) from exc
    return JiraCloud(
        url,
        username=email,
        password=token,
        timeout=JIRA_TIMEOUT_SECONDS,
        api_version=3,
    )


def _status_code(exc: BaseException) -> int | None:
    response = getattr(exc, "response", None)
    status = getattr(response, "status_code", None)
    if isinstance(status, int):
        return status
    return None


def _is_requests_error(exc: BaseException) -> bool:
    try:
        from requests.exceptions import RequestException
    except ImportError:
        return False
    return isinstance(exc, RequestException)


def _jira_failure(exc: BaseException) -> TrackerError:
    # The response body can echo the request. Keep it out of the summary.
    status = _status_code(exc)
    if status is not None:
        return TrackerError(f"cannot call Jira: HTTP {status}", status)
    return TrackerError(f"cannot reach Jira: {exc.__class__.__name__}")


def _call_jira(method: Callable[..., Any], *args: Any, **kwargs: Any) -> Any:
    try:
        return method(*args, **kwargs)
    except TrackerError:
        raise
    except Exception as exc:
        if _status_code(exc) is None and not _is_requests_error(exc):
            raise
        raise _jira_failure(exc) from exc


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
        jira: Any,
        name_source: Callable[[], str] | None = None,
    ) -> None:
        self.config = config
        self.env = env
        self.client = client
        self.jira = jira
        self.now = now
        self.name_source = name_source or petname_words
        self.repo = repository_name(env)
        self.token = github_token(env)
        self.permissions = permissions_from_env(env)

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
                "cannot update advisory summary: "
                f"GitHub returned HTTP {result.status}. "
                "Set SNAPD_ADVISORY_GITHUB_TOKEN with write access "
                "to repository security advisories",
                result.status,
            )
        if result.status != 200:
            raise TrackerError(
                f"cannot update advisory summary: HTTP {result.status}", result.status
            )

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
            result = _call_jira(
                self.jira.search_and_reconsile_issues_using_jql_post, data=payload
            )
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
        payload = _call_jira(self.jira.get_issue, key, fields=",".join(ISSUE_FIELDS))
        if not isinstance(payload, dict):
            raise TrackerError(f"cannot read Jira issue {key}")
        return issue_from_json(payload)

    def create(self, fields: Mapping[str, Any]) -> Issue:
        payload = _call_jira(self.jira.create_issue, data={"fields": dict(fields)})
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
        ready=not reasons,
        reasons=reasons,
        action="",
        issue_key=None,
        issue_keys=[],
        issue_status=None,
        notifications=[],
        error=None,
    )


def issue_references(issue: Issue, ghsa_id: str) -> bool:
    if ghsa_id == "":
        return False
    if any(ghsa_id in label for label in issue.labels):
        return True
    return ghsa_id in issue.summary or ghsa_id in issue.description_text


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


def outside_parent_error(ghsa_id: str, outside: list[Issue], parent_key: str) -> str:
    keys = ", ".join(issue.key for issue in outside)
    if len(outside) == 1:
        relation = "associated Jira ticket is not a child"
    else:
        relation = "associated Jira tickets are not children"
    return f"{ghsa_id} {keys}: {relation} of {parent_key}"


def _remember_error(report: Report, row: AdvisoryRow, message: str) -> None:
    row.error = message
    if message not in report.errors:
        report.errors.append(message)


def _attach(row: AdvisoryRow, issue: Issue, related: list[Issue]) -> None:
    row.issue_key = issue.key
    row.issue_keys = [item.key for item in related] or [issue.key]
    row.issue_status = issue.status


def created_message(row: AdvisoryRow, issue_key: str) -> str:
    name = public_label(row)
    lines = [
        f"{issue_key} ({name}): Jira ticket created",
        f"GitHub-Advisory: {row.ghsa_id}",
        f"Link: {row.url}",
    ]
    return "\n".join([f"{AUTOMATION_MARK} new advisory detected", *lines])


def run_summary_url(env: Mapping[str, str]) -> str:
    run_id = env.get("GITHUB_RUN_ID") or ""
    if run_id == "":
        raise TrackerError("cannot send mattermost message: missing GITHUB_RUN_ID")
    server = (env.get("GITHUB_SERVER_URL") or "https://github.com").rstrip("/")
    repo = env.get("GITHUB_REPOSITORY") or ""
    if repo == "":
        raise TrackerError("cannot send mattermost message: missing GITHUB_REPOSITORY")
    return f"{server}/{repo}/actions/runs/{run_id}"


def not_created_message(env: Mapping[str, str]) -> str:
    return "\n".join(
        [
            f"{AUTOMATION_MARK} a Jira ticket was not created",
            f"Link: {run_summary_url(env)}",
        ]
    )


def _post_mattermost(tracker: Tracker, message: str) -> None:
    url = tracker.env.get(NEW_ADVISORY_WEBHOOK) or ""
    if url == "":
        raise TrackerError(
            f"cannot send mattermost message: set {NEW_ADVISORY_WEBHOOK}"
        )
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


def _announce_created(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    issue_key: str,
    writes: Permissions,
) -> None:
    if not tracker.permissions.notify:
        if "notify disabled" not in row.notifications:
            row.notifications.append("notify disabled")
        return
    message = created_message(row, issue_key)
    for channel in tracker.config.notify["created"]:
        if not writes.notify:
            row.notifications.append(f"would {channel} created")
            continue
        try:
            _post_mattermost(tracker, message)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            message_error = f"{row.ghsa_id}: {exc}"
            row.error = str(exc)
            if message_error not in report.errors:
                report.errors.append(message_error)
            continue
        row.notifications.append(f"sent {channel} created")


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


def _blocked_creation(row: AdvisoryRow, states: Sequence[str]) -> bool:
    if row.state not in states or row.issue_key:
        return False
    return row.action in {"not_ready", "error", "refused"}


def _finish_not_created(
    tracker: Tracker, report: Report, command: str, dry_run: bool
) -> None:
    if command != "sync":
        return
    rows = [
        row
        for row in report.advisories
        if _blocked_creation(row, report.create_for_states)
    ]
    if not rows or not tracker.permissions.create:
        return
    channels = tracker.config.notify.get("not_created", ())
    if "mattermost" not in channels:
        return
    row = rows[0]
    if not tracker.permissions.notify:
        row.notifications.append("notify disabled not_created")
        return
    if dry_run:
        row.notifications.append("would mattermost not_created")
        return
    try:
        _post_mattermost(tracker, not_created_message(tracker.env))
    except TrackerError as exc:
        message = f"cannot send not-created notice: {exc}"
        report.errors.append(message)
        if row.error:
            row.notifications.append(message)
        else:
            row.error = message
        return
    row.notifications.append("sent mattermost not_created")


def _create_missing(
    tracker: Tracker,
    report: Report,
    row: AdvisoryRow,
    advisory: Mapping[str, Any],
    writes: Permissions,
    used: set[str],
    mode: str,
) -> Issue | None:
    if row.state not in tracker.config.create_for_states:
        row.action = "skipped"
        return None
    if not row.ready:
        row.action = "not_ready"
        return None
    if mode == "summary":
        row.action = "untracked"
        return None
    if report.confirmation_error:
        row.action = "refused"
        row.error = report.confirmation_error
        return None
    if not writes.create:
        row.action = "would_create" if tracker.permissions.create else "create_disabled"
        if row.action == "would_create":
            _announce_created(tracker, report, row, "new", writes)
        return None
    if row.codename is None:
        row.codename = choose_codename(tracker.name_source, used)
    try:
        issue = tracker.create(create_fields(tracker.config, row, advisory))
    except TrackerError as exc:
        if exc.status in {401, 403}:
            raise
        row.action = "error"
        _remember_error(report, row, f"{row.ghsa_id}: {exc}")
        return None
    row.action = "created"
    _attach(row, issue, [issue])
    _announce_created(tracker, report, row, issue.key, writes)
    return issue


def _process(
    tracker: Tracker,
    report: Report,
    advisory: Mapping[str, Any],
    children: list[Issue],
    writes: Permissions,
    used: set[str],
    mode: str,
) -> None:
    row = _row_from_advisory(advisory, tracker.config, tracker.repo)
    report.advisories.append(row)
    if not GHSA_RE.fullmatch(row.ghsa_id):
        row.action = "error"
        _remember_error(report, row, "advisory is missing a GHSA id")
        return
    named_on_advisory = row.codename is not None
    _primary, related = select_issues(
        _find_matches(tracker, children, row.ghsa_id, row.codename),
        tracker.config.parent_key,
        row.codename,
    )
    epic = [item for item in related if item.parent_key == tracker.config.parent_key]
    outside = [
        item for item in related if item.parent_key != tracker.config.parent_key
    ]
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
    if issue is None:
        issue = _create_missing(tracker, report, row, advisory, writes, used, mode)
        if issue is None:
            return
        related = [issue]
        row.issue_keys = [issue.key]
    else:
        _attach(row, issue, related)
        if row.action == "":
            row.action = "tracked"
    if named_on_advisory:
        return
    try:
        _publish_codename(tracker, row, related, writes, mode, used)
    except TrackerError as exc:
        if exc.status in {401, 403}:
            raise
        _remember_error(report, row, str(exc))


def execute(
    config: Config,
    env: Mapping[str, str],
    client: HttpClient,
    now: datetime,
    command: str,
    dry_run: bool = False,
    name_source: Callable[[], str] | None = None,
    jira: Any = None,
) -> Report:
    if command not in {"summary", "sync"}:
        raise TrackerError(f"cannot run advisory tracker: unknown command {command}")
    if jira is None:
        jira = connect_jira(*jira_settings(env))
    tracker = Tracker(config, env, client, now, jira, name_source)
    mode = "summary" if command == "summary" else "dry-run" if dry_run else "apply"
    advisories = tracker.fetch_advisories()
    children = tracker.search(f"parent = {config.parent_key}")
    report = Report(
        parent=config.parent_key,
        mode=mode,
        confirmation_error=confirm_children(children, config),
        errors=[],
        advisories=[],
        permissions=tracker.permissions,
        create_for_states=config.create_for_states,
    )
    active = command == "sync" and not dry_run and report.confirmation_error is None
    writes = Permissions(
        active and tracker.permissions.create,
        active and tracker.permissions.notify,
    )
    used = collect_used_codenames(advisories, children)
    for advisory in advisories:
        try:
            _process(tracker, report, advisory, children, writes, used, mode)
        except TrackerError as exc:
            if exc.status in {401, 403}:
                raise
            if report.advisories:
                _remember_error(
                    report,
                    report.advisories[-1],
                    f"{report.advisories[-1].ghsa_id or 'advisory'}: {exc}",
                )
            else:
                report.errors.append(str(exc))
    if report.confirmation_error and command == "sync":
        if report.confirmation_error not in report.errors:
            report.errors.append(report.confirmation_error)
    _finish_not_created(tracker, report, command, dry_run)
    return report


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


def _not_created_reason(report: Report, row: AdvisoryRow) -> str:
    if row.action == "not_ready":
        return ", ".join(row.reasons) or "not ready"
    if row.action == "refused":
        return row.error or report.confirmation_error or "issue type does not match"
    if row.error:
        return row.error
    return row.action


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
            "| Advisory | State | Ticket | Status | Result |",
            "| --- | --- | --- | --- | --- |",
        ]
    )
    for row in visible:
        result = row.error or row.action
        if row.notifications:
            result = f"{result}; {', '.join(row.notifications)}"
        lines.append(
            "| "
            + " | ".join(
                [
                    _cell(f"{public_label(row)} {row.ghsa_id}"),
                    _cell(row.state),
                    _cell(
                        ", ".join(row.issue_keys)
                        if row.issue_keys
                        else (row.issue_key or "")
                    ),
                    _cell(row.issue_status or ""),
                    _cell(result),
                ]
            )
            + " |"
        )
    blocked = [
        row for row in visible if _blocked_creation(row, report.create_for_states)
    ]
    lines.extend(["", "## Not created", ""])
    if not blocked:
        lines.append("None.")
    for row in blocked:
        lines.append(f"- `{row.ghsa_id}`: {_not_created_reason(report, row)}")
    not_ready = [row for row in visible if row.reasons]
    lines.extend(["", "## Not ready for triage", ""])
    if not not_ready:
        lines.append("None.")
    for row in not_ready:
        lines.append(f"- `{row.ghsa_id}`: {', '.join(row.reasons)}")
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
    markdown = render_markdown(report, omit_settled=args.command == "summary")
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
                f"::warning::{row.ghsa_id} is not ready: {', '.join(row.reasons)}",
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
