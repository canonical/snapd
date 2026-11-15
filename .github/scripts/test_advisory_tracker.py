#!/usr/bin/env python3

import io
import json
import os
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from dataclasses import replace
from datetime import UTC, datetime, timedelta

import advisory_tracker as tracker

CONFIG_PATH = os.path.normpath(
    os.path.join(os.path.dirname(__file__), "..", "advisory-tracker.yaml")
)
NOW = datetime(2026, 11, 12, 15, 0, tzinfo=UTC)
NOW_STAMP = "2026-11-12T15:00:00.000+0000"
OLD_STAMP = "2026-11-09T15:00:00.000+0000"
PARENT = "SNAPDENG-37541"
EMBER = "GHSA-95j9-cvc3-mmgg"
WILLOW = "GHSA-p962-3gwv-5ppc"
QUIET = "GHSA-jhwq-ffw7-65j7"
KEEN = "GHSA-k33n-yak0-0001"
CALM = "GHSA-ca1m-mink-0002"
_PETNAME_DIR = None


def setUpModule():
    global _PETNAME_DIR
    _PETNAME_DIR = tempfile.TemporaryDirectory()
    adjectives = (
        "ember",
        "quiet",
        "lively",
        "salty",
        "keen",
        "calm",
        "relishing",
    )
    names = ("falcon", "lantern", "willow", "compass", "yak", "mink", "lionfish")
    with open(
        os.path.join(_PETNAME_DIR.name, "adjectives.txt"), "w", encoding="utf-8"
    ) as handle:
        handle.write("\n".join(adjectives) + "\n")
    with open(
        os.path.join(_PETNAME_DIR.name, "names.txt"), "w", encoding="utf-8"
    ) as handle:
        handle.write("\n".join(names) + "\n")
    tracker.petname_word_dir = _PETNAME_DIR.name


def tearDownModule():
    if _PETNAME_DIR is not None:
        _PETNAME_DIR.cleanup()


NEW_ADVISORY_HOOK = "https://hooks.mattermost.test/new-advisory"
MAINTENANCE_HOOK = "https://hooks.mattermost.test/maintenance"
RUN_LINK = "https://github.com/canonical/snapd/actions/runs/4242"
MAINTENANCE_TEXT = f"advisory-tracker: maintenance\nLink: {RUN_LINK}"


def environment():
    return {
        "SNAPD_ADVISORY_GITHUB_TOKEN": "ghs_test",
        "JIRA_EMAIL": "bot@example.com",
        "JIRA_API_TOKEN": "jira-token",
        "JIRA_BASE_URL": "https://warthogs.atlassian.net",
        "GITHUB_REPOSITORY": "canonical/snapd",
        "GITHUB_SERVER_URL": "https://github.com",
        "GITHUB_RUN_ID": "4242",
        "MATTERMOST_NEW_ADVISORY_WEBHOOK_URL": NEW_ADVISORY_HOOK,
        "MATTERMOST_MAINTENANCE_WEBHOOK_URL": MAINTENANCE_HOOK,
        "ADVISORY_TRACKER_CREATE": "true",
        "ADVISORY_TRACKER_NOTIFY": "true",
        "ADVISORY_TRACKER_SYNC": "true",
    }


def sample_advisory(
    ghsa, summary, state="triage", severity="medium", description=None, cve=None
):
    return {
        "ghsa_id": ghsa,
        "summary": summary,
        "state": state,
        "severity": severity,
        "description": "A" * 80 if description is None else description,
        "cve_id": cve,
        "html_url": f"https://github.com/canonical/snapd/security/advisories/{ghsa}",
        "credits": [{"login": "reporter", "type": "reporter"}],
        "cvss_severities": {
            "cvss_v4": {"vector_string": "CVSS:4.0/AV:L", "score": 5.7}
        },
        "vulnerabilities": [
            {"package": {"name": "snapd"}, "vulnerable_version_range": "< 2.70"}
        ],
    }


def jira_user(account_id, name):
    return {"accountId": account_id, "displayName": name}


ADA = jira_user("ada-id", "Ada")
RAE = jira_user("rae-id", "Rae")


def jira_issue(
    key,
    summary,
    status,
    ghsa=None,
    description="",
    created=NOW_STAMP,
    assignee=False,
    issue_type="Task",
    parent=PARENT,
    reporter=None,
):
    labels = [ghsa] if ghsa else []
    text = description
    if text == "" and ghsa:
        text = f"GitHub-Advisory: {ghsa}"
    body = None
    if text:
        body = {
            "type": "doc",
            "version": 1,
            "content": [
                {"type": "paragraph", "content": [{"type": "text", "text": text}]}
            ],
        }
    if assignee is True:
        assignee_field = ADA
    elif assignee:
        assignee_field = assignee
    else:
        assignee_field = None
    return {
        "key": key,
        "fields": {
            "summary": summary,
            "status": {"name": status},
            "labels": labels,
            "description": body,
            "assignee": assignee_field,
            "reporter": reporter,
            "created": created,
            "issuetype": {"name": issue_type},
            "parent": {"key": parent} if parent else None,
        },
    }


def parent_epic(assignee=None, reporter=None):
    return jira_issue(
        PARENT,
        "snapd advisories",
        "Triage",
        assignee=assignee,
        reporter=reporter,
        parent=None,
    )


def issue_key(path):
    parts = [part for part in path.split("/") if part]
    return parts[4]


def issue_view(
    key="SNAPDENG-10",
    status="Triage",
    assignee=False,
    created=NOW,
    summary="Ember Falcon",
):
    return tracker.Issue(
        key=key,
        summary=summary,
        status=status,
        labels=(EMBER,),
        description_text=f"GitHub-Advisory: {EMBER}",
        assignee=assignee,
        created=created,
        issue_type="Task",
        parent_key=PARENT,
    )


class FakeClient(tracker.HttpClient):
    def __init__(self, advisories, catalog):
        self.advisories = advisories
        self.extra_advisories = None
        self.catalog = catalog
        self.properties = {}
        self.comments = {}
        self.calls = []
        self.page = 0
        self.github_status = 200
        self.patch_status = 200
        self.github_body = None
        self.github_always_next = False
        self.mattermost_status = 200
        self.property_status = None
        self.omit_create_key = False
        self.search_page_size = None
        self.search_forever = False
        self.comment_page_size = None
        self.comment_status = {}
        self.create_error_body = None
        self.updates = []
        self.next_number = 500
        self.default_status = "Backlog"
        self.transition_names = {
            "31": "Triage",
            "32": "In Progress",
            "33": "Done",
            "34": "Won't Do",
        }
        self.transition_catalog = None

    def request(self, method, url, headers, body):
        self.calls.append((method, url, body))
        if "api.github.com" in url:
            return self._github(method, url, body)
        if url.startswith("https://hooks.mattermost.test/"):
            body = b"ok" if self.mattermost_status < 300 else b"no"
            return tracker.HttpResult(self.mattermost_status, {}, body)
        return self._jira(method, url, body)

    def _github(self, method, url, body):
        if method == "PATCH":
            if self.patch_status != 200:
                return tracker.HttpResult(
                    self.patch_status, {}, b'{"message":"rejected"}'
                )
            payload = json.loads(body.decode())
            self.updates.append((url, payload))
            ghsa = url.rstrip("/").split("/")[-1]
            for advisory in self.advisories:
                if advisory.get("ghsa_id") == ghsa:
                    advisory["summary"] = payload["summary"]
            return self._json(payload)
        if self.github_status != 200:
            return tracker.HttpResult(
                self.github_status, {}, b'{"message":"forbidden"}'
            )
        if self.github_body is not None:
            return tracker.HttpResult(200, {}, self.github_body)
        if self.github_always_next:
            header = f'<{url}&page=again>; rel="next"'
            return tracker.HttpResult(
                200, {"link": header}, json.dumps(self.advisories).encode()
            )
        if self.extra_advisories is None:
            return tracker.HttpResult(200, {}, json.dumps(self.advisories).encode())
        if self.page == 0:
            self.page = 1
            header = f'<{url}&page=2>; rel="next"'
            return tracker.HttpResult(
                200, {"link": header}, json.dumps(self.advisories).encode()
            )
        return tracker.HttpResult(200, {}, json.dumps(self.extra_advisories).encode())

    def _jira(self, method, url, body):
        path = url.split("warthogs.atlassian.net", 1)[-1].split("?", 1)[0]
        payload = json.loads(body.decode()) if body else {}
        if method == "POST" and path == "/rest/api/3/search/jql":
            token = payload.get("nextPageToken") or ""
            return self._json(self._search(payload["jql"], token))
        if method == "POST" and path == "/rest/api/3/issue":
            if self.create_error_body is not None:
                return tracker.HttpResult(400, {}, self.create_error_body)
            return self._json(self._create(payload["fields"]), 201)
        if path.endswith("/transitions") and method == "GET":
            key = issue_key(path)
            status = self._issue(key)["fields"]["status"]["name"]
            if self.transition_catalog is not None:
                targets = self.transition_catalog
            elif status == "Backlog":
                targets = [("31", "Triage")]
            else:
                targets = [
                    ("32", "In Progress"),
                    ("33", "Done"),
                    ("34", "Won't Do"),
                ]
            return self._json(
                {
                    "transitions": [
                        {"id": ident, "name": name, "to": {"name": name}}
                        for ident, name in targets
                    ]
                }
            )
        if path.endswith("/transitions") and method == "POST":
            key = issue_key(path)
            ident = payload["transition"]["id"]
            self._issue(key)["fields"]["status"]["name"] = self.transition_names[ident]
            return tracker.HttpResult(204, {}, b"")
        if path.endswith("/properties/advisory-tracker") and method == "GET":
            if self.property_status is not None:
                return tracker.HttpResult(
                    self.property_status, {}, b'{"errorMessages":["unavailable"]}'
                )
            key = issue_key(path)
            if key not in self.properties:
                return tracker.HttpResult(404, {}, b'{"errorMessages":["missing"]}')
            return self._json(
                {"key": "advisory-tracker", "value": self.properties[key]}
            )
        if path.endswith("/properties/advisory-tracker") and method == "PUT":
            key = issue_key(path)
            self.properties[key] = payload
            return tracker.HttpResult(200, {}, b"{}")
        if path.endswith("/comment") and method == "GET":
            key = issue_key(path)
            status = self.comment_status.get(key)
            if status is not None:
                return tracker.HttpResult(
                    status, {}, b'{"errorMessages":["unavailable"]}'
                )
            comments = self.comments.get(key, [])
            if self.comment_page_size:
                start = 0
                query = url.split("?", 1)[1] if "?" in url else ""
                for part in query.split("&"):
                    if part.startswith("startAt="):
                        start = int(part.split("=", 1)[1] or "0")
                page = comments[start : start + self.comment_page_size]
                return self._json({"comments": page, "total": len(comments)})
            return self._json({"comments": comments, "total": len(comments)})
        if path.endswith("/comment") and method == "POST":
            key = issue_key(path)
            self.comments.setdefault(key, []).append(
                {"body": payload["body"], "created": NOW_STAMP}
            )
            return self._json({"id": "1"}, 201)
        if path.endswith("/changelog") and method == "GET":
            return self._json({"values": [], "total": 0})
        if method == "GET" and path.startswith("/rest/api/3/issue/"):
            key = issue_key(path)
            try:
                return self._json(self._issue(key))
            except AssertionError:
                return tracker.HttpResult(404, {}, b'{"errorMessages":["missing"]}')
        return tracker.HttpResult(404, {}, b'{"errorMessages":["not found"]}')

    def _search(self, jql, page_token=""):
        if "parent =" in jql:
            issues = self.catalog
        else:
            issues = []
            for issue in self.catalog:
                labels = issue["fields"]["labels"]
                text = (
                    tracker.adf_to_text(issue["fields"]["description"])
                    + "\n"
                    + issue["fields"]["summary"]
                )
                if any(label in jql for label in labels) or any(
                    token in jql for token in text.split()
                ):
                    issues.append(issue)
        if self.search_forever:
            return {"issues": issues[:1], "nextPageToken": "again"}
        if not self.search_page_size:
            return {"issues": issues}
        page = int(page_token) if page_token else 0
        start = page * self.search_page_size
        chunk = issues[start : start + self.search_page_size]
        result = {"issues": chunk}
        if start + self.search_page_size < len(issues):
            result["nextPageToken"] = str(page + 1)
        return result

    def _create(self, fields):
        if self.omit_create_key:
            return {"id": "100"}
        key = f"SNAPDENG-{self.next_number}"
        self.next_number += 1
        issue = jira_issue(
            key,
            fields["summary"],
            self.default_status,
            description=tracker.adf_to_text(fields["description"]),
            created=NOW_STAMP,
            issue_type=fields["issuetype"]["name"],
        )
        issue["fields"]["labels"] = list(fields["labels"])
        issue["fields"]["description"] = fields["description"]
        self.catalog.append(issue)
        return {"id": "100", "key": key}

    def _issue(self, key):
        for issue in self.catalog:
            if issue["key"] == key:
                return issue
        raise AssertionError(key)

    def _json(self, payload, status=200):
        return tracker.HttpResult(status, {}, json.dumps(payload).encode())


def run(
    client,
    advisories=None,
    command="sync",
    dry_run=False,
    config=None,
    name_source=None,
    env=None,
):
    if advisories is not None:
        client.advisories = advisories
    return tracker.execute(
        config or tracker.load_config(CONFIG_PATH),
        environment() if env is None else env,
        client,
        NOW,
        command,
        dry_run,
        name_source,
    )


def quiet_catalog(issue):
    return [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue]


def aging_published_client():
    issue = jira_issue(
        "SNAPDENG-40", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
    )
    return FakeClient(
        [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
        quiet_catalog(issue) + [parent_epic(assignee=ADA)],
    )


def config_with_open_done_stage():
    config = tracker.load_config(CONFIG_PATH)
    stages = list(config.stages)
    stages[-1] = replace(stages[-1], statuses=("Blocked",))
    return replace(config, stages=tuple(stages))


def mattermost_texts(client, hook=None):
    return [
        json.loads(body.decode())["text"]
        for method, url, body in client.calls
        if method == "POST"
        and url.startswith("https://hooks.mattermost.test/")
        and (hook is None or url == hook)
    ]


def mention_nodes(node):
    found = []
    if isinstance(node, dict):
        if node.get("type") == "mention":
            found.append(node)
        for child in node.get("content") or []:
            found.extend(mention_nodes(child))
    elif isinstance(node, list):
        for child in node:
            found.extend(mention_nodes(child))
    return found


def severity_config(in_progress, done):
    with open(CONFIG_PATH, encoding="utf-8") as handle:
        raw = tracker.parse_yaml(handle.read())
    for stage in raw["progress"]["stages"]:
        if stage["name"] == "in_progress":
            stage["within_days"] = in_progress
        elif stage["name"] == "done":
            stage["within_days"] = done
    return tracker.config_from_mapping(raw)


def posts(client, suffix):
    found = []
    for method, url, body in client.calls:
        path = url.split("?", 1)[0]
        if method == "POST" and path.endswith(suffix):
            found.append(json.loads(body.decode()))
    return found


class AdvisoryTrackerTests(unittest.TestCase):
    def test_shipped_config(self):
        config = tracker.load_config(CONFIG_PATH)
        self.assertEqual(config.parent_key, PARENT)
        self.assertEqual(config.project_key, "SNAPDENG")
        self.assertEqual(config.issue_type, "Task")
        self.assertEqual(config.create_for_states, ("triage", "draft"))
        self.assertEqual(config.state_map["triage"], "Triage")
        self.assertEqual(config.state_map["draft"], "In Progress")
        self.assertEqual(config.state_map["published"], "Done")
        self.assertEqual(config.state_map["closed"], "Won't Do")
        self.assertEqual(
            list(config.status_progress.items()),
            [
                ("Triage", "triage"),
                ("In Progress", "draft"),
                ("In Review", "draft"),
                ("Done", "published"),
                ("Won't Do", "closed"),
            ],
        )
        self.assertEqual(config.priority_map["critical"], "Highest")
        self.assertEqual(config.min_description_chars, 80)
        self.assertEqual(config.stall_cooldown_days, 7)
        self.assertEqual(
            tuple(stage.name for stage in config.stages),
            ("assigned", "investigated", "in_progress", "done"),
        )
        self.assertEqual(config.stages[0].within_days, 2)
        self.assertEqual(config.stages[0].within_by_severity, {})
        self.assertEqual(config.stages[1].within_days, 7)
        self.assertEqual(config.stages[1].within_by_severity, {})
        self.assertEqual(
            config.stages[2].within_by_severity,
            {"critical": 14, "high": 14, "medium": 14, "low": 14, "default": 14},
        )
        self.assertEqual(
            config.stages[3].within_by_severity,
            {"critical": 60, "high": 60, "medium": 60, "low": 60, "default": 60},
        )
        self.assertEqual(config.stages[-1].statuses, ("Done", "Won't Do"))
        self.assertEqual(config.notify["created"], ("mattermost",))
        self.assertEqual(config.notify["not_ready"], ("mattermost",))
        self.assertEqual(config.notify["stall"], ("mattermost", "jira_comment"))

    def test_unknown_channel_is_rejected(self):
        with open(CONFIG_PATH, encoding="utf-8") as handle:
            raw = tracker.parse_yaml(handle.read())
        raw["notify"]["stall"] = ["sms"]
        with self.assertRaises(tracker.TrackerError) as caught:
            tracker.config_from_mapping(raw)
        self.assertIn("unknown channel sms", str(caught.exception))

    def test_codename_shapes(self):
        self.assertEqual(
            tracker.extract_codename("pins a map (Ember Falcon)"), "Ember Falcon"
        )
        self.assertEqual(
            tracker.extract_codename("priv esc (aka Lively Willow)"), "Lively Willow"
        )
        self.assertEqual(
            tracker.extract_codename("socket (Quiet Lantern 2)"), "Quiet Lantern 2"
        )
        self.assertIsNone(tracker.extract_codename("Incorrect D-Bus type for uid"))
        self.assertIsNone(tracker.extract_codename("(Ember Falcon) and more"))

    def test_readiness_flags_missing_information(self):
        config = tracker.load_config(CONFIG_PATH)
        ready = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        self.assertEqual(tracker.readiness_reasons(ready, config), [])
        self.assertIn(
            "missing summary",
            tracker.readiness_reasons({**ready, "summary": "  "}, config),
        )
        self.assertEqual(
            tracker.readiness_reasons({**ready, "summary": "no name"}, config), []
        )
        self.assertEqual(
            tracker.readiness_reasons({**ready, "severity": None}, config), []
        )
        self.assertIn(
            "description is shorter than 80 characters",
            tracker.readiness_reasons({**ready, "description": "short"}, config),
        )

    def test_transition_contract(self):
        self.assertEqual(tracker.transition_payload("31"), {"transition": {"id": "31"}})
        self.assertTrue(tracker.transition_needed("Backlog", "Triage"))
        self.assertFalse(tracker.transition_needed("Triage", "Triage"))
        self.assertTrue(tracker.transition_needed("Triage", "In Progress"))
        self.assertFalse(tracker.transition_needed("In Progress", "In Progress"))
        self.assertFalse(tracker.should_store_state("draft", "draft", True, True))
        self.assertFalse(tracker.should_store_state(None, "triage", True, False))
        self.assertTrue(tracker.should_store_state(None, "triage", True, True))
        self.assertTrue(tracker.should_store_state(None, "triage", False, False))

    def test_progress_and_cooldown(self):
        config = tracker.load_config(CONFIG_PATH)
        recent = issue_view(created=NOW)
        self.assertEqual(tracker.behind_stages(recent, 0, NOW, config), [])
        assigned = tracker.behind_stages(
            issue_view(created=NOW - timedelta(days=3)), 0, NOW, config
        )
        self.assertEqual([stage.name for stage in assigned], ["assigned"])
        investigated = tracker.behind_stages(
            issue_view(created=NOW - timedelta(days=8), assignee=True),
            0,
            NOW,
            config,
        )
        self.assertEqual([stage.name for stage in investigated], ["investigated"])
        caught_up = tracker.behind_stages(
            issue_view(created=NOW - timedelta(days=8), assignee=True),
            1,
            NOW,
            config,
        )
        self.assertEqual(caught_up, [])
        finished = tracker.behind_stages(
            issue_view(status="Done", created=NOW - timedelta(days=90)),
            0,
            NOW,
            config,
        )
        self.assertEqual(finished, [])
        self.assertEqual(
            tracker.human_comment_count(
                [{"body": "advisory-tracker:stall assigned\nnote"}]
            ),
            0,
        )
        human = {
            "body": {
                "type": "doc",
                "content": [{"type": "text", "text": "triaged the report"}],
            },
            "author": {"accountType": "atlassian"},
        }
        self.assertEqual(tracker.human_comment_count([human]), 1)
        self.assertEqual(
            tracker.human_comment_count([{"body": "triaged the report"}]), 0
        )
        app = {
            "body": "triaged the report",
            "author": {"accountType": "app"},
        }
        self.assertEqual(tracker.human_comment_count([app]), 0)
        self.assertEqual(
            [
                stage.name
                for stage in tracker.behind_stages(
                    issue_view(created=NOW - timedelta(days=8), assignee=True),
                    tracker.human_comment_count([app]),
                    NOW,
                    config,
                )
            ],
            ["investigated"],
        )
        active = {"stalls": {"assigned": (NOW - timedelta(days=1)).isoformat()}}
        self.assertTrue(tracker.cooldown_active(active, "assigned", NOW, 7))
        expired = {"stalls": {"assigned": (NOW - timedelta(days=8)).isoformat()}}
        self.assertFalse(tracker.cooldown_active(expired, "assigned", NOW, 7))

    def test_severity_thresholds_select_the_configured_limit(self):
        config = severity_config(
            in_progress={
                "critical": 3,
                "high": 5,
                "medium": 14,
                "low": 21,
                "default": 30,
            },
            done={"critical": 10, "high": 20, "medium": 40, "low": 90, "default": 120},
        )
        issue = issue_view(created=NOW - timedelta(days=4), assignee=True)
        self.assertEqual(
            [
                stage.name
                for stage in tracker.behind_stages(issue, 1, NOW, config, "critical")
            ],
            ["in_progress"],
        )
        self.assertEqual(tracker.behind_stages(issue, 1, NOW, config, "low"), [])
        self.assertEqual(tracker.behind_stages(issue, 1, NOW, config, ""), [])
        older = issue_view(created=NOW - timedelta(days=25), assignee=True)
        self.assertEqual(
            [
                stage.name
                for stage in tracker.behind_stages(older, 1, NOW, config, "critical")
            ],
            ["in_progress", "done"],
        )
        self.assertEqual(
            [
                stage.name
                for stage in tracker.behind_stages(older, 1, NOW, config, "low")
            ],
            ["in_progress"],
        )
        self.assertEqual(tracker.behind_stages(older, 1, NOW, config, "severe"), [])
        aged = issue_view(created=NOW - timedelta(days=31), assignee=True)
        self.assertEqual(
            [stage.name for stage in tracker.behind_stages(aged, 1, NOW, config, "")],
            ["in_progress"],
        )
        self.assertEqual(
            [
                stage.name
                for stage in tracker.behind_stages(aged, 1, NOW, config, "critical")
            ],
            ["in_progress", "done"],
        )
        in_progress = next(
            stage for stage in config.stages if stage.name == "in_progress"
        )
        done = next(stage for stage in config.stages if stage.name == "done")
        self.assertEqual(
            tracker.stall_reason(in_progress, "critical"),
            (
                "not in progress: has not reached In Progress, In Review, or Done "
                "within 3 days of ticket creation for critical"
            ),
        )
        self.assertEqual(
            tracker.stall_reason(done, "low"),
            (
                "not done: has not reached Done or Won't Do "
                "within 90 days of ticket creation for low"
            ),
        )
        self.assertEqual(
            tracker.stall_reason(done, ""),
            "not done: has not reached Done or Won't Do "
            "within 120 days of ticket creation for unset severity",
        )
        self.assertEqual(
            tracker.stall_reason(config.stages[0], "critical"),
            "not assigned: no assignee within 2 days of ticket creation",
        )

    def test_summary_json_reports_the_selected_stall_reason(self):
        config = severity_config(
            in_progress={
                "critical": 3,
                "high": 5,
                "medium": 14,
                "low": 21,
                "default": 30,
            },
            done={"critical": 10, "high": 20, "medium": 40, "low": 90, "default": 120},
        )
        issue = jira_issue(
            "SNAPDENG-30",
            "Ember Falcon",
            "Triage",
            EMBER,
            created=OLD_STAMP,
            assignee=True,
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", severity="critical")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client, command="summary", config=config)
        payload = json.loads(tracker.render_json(report))
        row = next(item for item in payload["advisories"] if item["ghsa_id"] == EMBER)
        self.assertEqual(
            row["stalls"],
            [
                {
                    "issue_key": "SNAPDENG-30",
                    "reasons": [
                        (
                            "not in progress: has not reached In Progress, "
                            "In Review, or Done "
                            "within 3 days of ticket creation for critical"
                        )
                    ],
                }
            ],
        )

    def test_severity_map_must_name_every_severity(self):
        with open(CONFIG_PATH, encoding="utf-8") as handle:
            raw = tracker.parse_yaml(handle.read())
        stage = next(
            item for item in raw["progress"]["stages"] if item["name"] == "done"
        )
        stage["within_days"] = {"critical": 10, "high": 20, "medium": 40, "low": 90}
        with self.assertRaises(tracker.TrackerError) as missing:
            tracker.config_from_mapping(raw)
        self.assertIn("within_days missing default", str(missing.exception))
        stage["within_days"] = {
            "critical": 10,
            "high": 20,
            "medium": 40,
            "low": 90,
            "default": 120,
            "urgent": 1,
        }
        with self.assertRaises(tracker.TrackerError) as unknown:
            tracker.config_from_mapping(raw)
        self.assertIn("unknown severity urgent", str(unknown.exception))
        stage["within_days"] = {
            "critical": 0,
            "high": 20,
            "medium": 40,
            "low": 90,
            "default": 120,
        }
        with self.assertRaises(tracker.TrackerError) as invalid:
            tracker.config_from_mapping(raw)
        self.assertIn(
            "within_days.critical must be a positive integer", str(invalid.exception)
        )

    def test_confirmation_and_duplicate_choice(self):
        config = tracker.load_config(CONFIG_PATH)
        child = issue_view(summary="Quiet Lantern")
        self.assertIsNone(tracker.confirm_children([child], config))
        self.assertIn("no child", tracker.confirm_children([], config))
        bug = issue_view(summary="Quiet Lantern")
        bug = tracker.Issue(**{**bug.__dict__, "issue_type": "Bug"})
        self.assertIn(
            "does not match child issue type Bug",
            tracker.confirm_children([bug], config),
        )
        plain = issue_view(summary="not a codename")
        self.assertIsNone(tracker.confirm_children([plain, bug], config))
        first = issue_view(key="SNAPDENG-1")
        second = issue_view(key="SNAPDENG-2", summary="fix the pin")
        chosen, related = tracker.select_issues([first, second], PARENT, "Ember Falcon")
        self.assertEqual(chosen.key, "SNAPDENG-1")
        self.assertEqual([issue.key for issue in related], ["SNAPDENG-1", "SNAPDENG-2"])
        outside = tracker.Issue(**{**first.__dict__, "parent_key": "SNAPDENG-9"})
        chosen, related = tracker.select_issues(
            [outside, second], PARENT, "Ember Falcon"
        )
        self.assertEqual(chosen.key, "SNAPDENG-2")
        self.assertEqual([issue.key for issue in related], ["SNAPDENG-2", "SNAPDENG-1"])
        self.assertTrue(tracker.issue_references(first, EMBER))
        described = tracker.Issue(
            **{
                **first.__dict__,
                "labels": (),
                "description_text": f"GitHub-Advisory: {WILLOW}",
            }
        )
        self.assertTrue(tracker.issue_references(described, WILLOW))
        self.assertFalse(tracker.issue_references(described, EMBER))

    def test_compound_label_is_associated(self):
        labeled = tracker.Issue(
            **{
                **issue_view().__dict__,
                "labels": (f"security-{EMBER}",),
                "summary": "pins a map",
                "description_text": "tracked privately",
            }
        )
        self.assertTrue(tracker.issue_references(labeled, EMBER))
        issue = jira_issue(
            "SNAPDENG-40", "pins a map", "Triage", description="tracked privately"
        )
        issue["fields"]["labels"] = [f"security-{EMBER}"]
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client)
        self.assertEqual(report.advisories[0].issue_keys, ["SNAPDENG-40"])
        self.assertNotEqual(report.advisories[0].action, "created")
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

    def test_split_ghsa_in_one_paragraph_is_recognized(self):
        description = {
            "type": "doc",
            "version": 1,
            "content": [
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "GHSA-95j9-"},
                        {
                            "type": "text",
                            "text": "cvc3-mmgg",
                            "marks": [{"type": "strong"}],
                        },
                    ],
                }
            ],
        }
        self.assertIn(EMBER, tracker.adf_to_text(description))
        broken = {
            "type": "paragraph",
            "content": [
                {"type": "text", "text": "GHSA-95j9-"},
                {"type": "hardBreak"},
                {"type": "text", "text": "cvc3-mmgg"},
            ],
        }
        self.assertNotIn(EMBER, tracker.adf_to_text(broken))
        self.assertIn("\n", tracker.adf_to_text(broken))
        issue = jira_issue("SNAPDENG-40", "pins a map", "Triage", description="note")
        issue["fields"]["labels"] = []
        issue["fields"]["description"] = description
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client)
        self.assertEqual(report.advisories[0].issue_keys, ["SNAPDENG-40"])
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

    def test_created_jira_comment_uses_the_new_ticket(self):
        config = replace(
            tracker.load_config(CONFIG_PATH),
            notify={
                "created": ("jira_comment",),
                "not_ready": ("mattermost",),
                "stall": ("mattermost", "jira_comment"),
            },
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, config=config)
        self.assertEqual(report.advisories[0].action, "created")
        self.assertIsNone(report.advisories[0].error)
        comments = [
            url
            for method, url, _body in client.calls
            if method == "POST" and url.endswith("/comment")
        ]
        self.assertEqual(
            comments,
            ["https://warthogs.atlassian.net/rest/api/3/issue/SNAPDENG-500/comment"],
        )
        self.assertIn(
            "SNAPDENG-500", tracker.adf_to_text(posts(client, "/comment")[0]["body"])
        )
        dry = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        dry_report = run(dry, config=config, dry_run=True)
        self.assertIn(
            "would jira_comment created", dry_report.advisories[0].notifications
        )
        self.assertEqual(posts(dry, "/comment"), [])

    def test_stall_read_failure_does_not_skip_the_next_child(self):
        first = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        second = jira_issue(
            "SNAPDENG-31", "second part", "Triage", EMBER, created=OLD_STAMP
        )
        catalog = [
            jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
            first,
            second,
            parent_epic(assignee=ADA),
        ]
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")], catalog
        )
        client.comment_status["SNAPDENG-30"] = 500
        report = run(client)
        row = report.advisories[0]
        self.assertIn("SNAPDENG-30", row.error or "")
        self.assertIn("HTTP 500", row.error or "")
        comments = [
            url
            for method, url, _body in client.calls
            if method == "POST" and url.endswith("/comment")
        ]
        self.assertEqual(
            comments,
            ["https://warthogs.atlassian.net/rest/api/3/issue/SNAPDENG-31/comment"],
        )
        forbidden = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")], catalog
        )
        forbidden.comment_status["SNAPDENG-30"] = 401
        with self.assertRaises(tracker.TrackerError) as caught:
            run(forbidden)
        self.assertEqual(caught.exception.status, 401)
        self.assertEqual(posts(forbidden, "/comment"), [])

    def test_resolve_mode(self):
        for name in (
            "ADVISORY_TRACKER_CREATE",
            "ADVISORY_TRACKER_NOTIFY",
            "ADVISORY_TRACKER_SYNC",
        ):
            self.assertEqual(
                tracker.resolve_mode({"GITHUB_EVENT_NAME": "schedule", name: "true"}),
                "apply",
            )
        self.assertEqual(
            tracker.resolve_mode({"GITHUB_EVENT_NAME": "schedule"}), "skip"
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "workflow_dispatch",
                    "INPUT_ACTION": "summary",
                    "INPUT_APPLY": "true",
                }
            ),
            "summary",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "workflow_dispatch",
                    "INPUT_ACTION": "sync",
                    "INPUT_APPLY": "true",
                    "INPUT_DRY_RUN": "true",
                }
            ),
            "dry-run",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "workflow_dispatch",
                    "INPUT_ACTION": "sync",
                    "INPUT_APPLY": "true",
                }
            ),
            "apply",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {"GITHUB_EVENT_NAME": "workflow_dispatch", "INPUT_ACTION": "sync"}
            ),
            "skip",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "workflow_dispatch",
                    "INPUT_ACTION": "sync",
                    "ADVISORY_TRACKER_CREATE": "true",
                    "ADVISORY_TRACKER_NOTIFY": "true",
                    "ADVISORY_TRACKER_SYNC": "true",
                }
            ),
            "skip",
        )
        self.assertEqual(
            tracker.resolve_mode({"GITHUB_EVENT_NAME": "repository_dispatch"}),
            "summary",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "repository_dispatch",
                    "PAYLOAD_ACTION": "sync",
                    "PAYLOAD_APPLY": "true",
                }
            ),
            "apply",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "repository_dispatch",
                    "PAYLOAD_ACTION": "sync",
                    "PAYLOAD_DRY_RUN": "true",
                    "ADVISORY_TRACKER_SYNC": "true",
                }
            ),
            "dry-run",
        )
        with self.assertRaises(tracker.TrackerError):
            tracker.resolve_mode({"GITHUB_EVENT_NAME": "push"})
        stdout = io.StringIO()
        with redirect_stdout(stdout):
            code = tracker.main(
                ["resolve-mode"],
                env={
                    "GITHUB_EVENT_NAME": "schedule",
                    "ADVISORY_TRACKER_NOTIFY": "true",
                },
            )
        self.assertEqual(code, 0)
        self.assertEqual(stdout.getvalue().strip(), "apply")

    def test_create_uses_codename_and_transition_id_only(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", cve="CVE-2024-1728")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        self.assertIsNone(report.confirmation_error)
        self.assertEqual(report.advisories[0].action, "created")
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-500")
        created = posts(client, "/rest/api/3/issue")
        self.assertEqual(len(created), 1)
        fields = created[0]["fields"]
        self.assertEqual(set(created[0]), {"fields"})
        self.assertNotIn("assignee", fields)
        self.assertEqual(fields["summary"], "Ember Falcon")
        self.assertEqual(fields["labels"], [EMBER])
        self.assertEqual(fields["parent"], {"key": PARENT})
        self.assertEqual(fields["project"], {"key": "SNAPDENG"})
        self.assertEqual(fields["issuetype"], {"name": "Task"})
        self.assertEqual(fields["priority"], {"name": "Medium"})
        text = tracker.adf_to_text(fields["description"])
        self.assertIn(f"GitHub-Advisory: {EMBER}", text)
        self.assertIn(
            f"Link: https://github.com/canonical/snapd/security/advisories/{EMBER}",
            text,
        )
        self.assertIn("CVE: CVE-2024-1728", text)
        self.assertIn("Severity: medium", text)
        self.assertIn("CVSS: CVSS:4.0/AV:L (5.7)", text)
        self.assertIn("Reporter: reporter", text)
        self.assertIn("Affected: snapd < 2.70", text)
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "31"}}])
        self.assertEqual(client.updates, [])

    def test_reports_hide_the_vulnerability_title(self):
        title = "CVE-2024-5300: systemd-userdbd (Ember Falcon)"
        ghsa = "GHSA-tt1e-redc-0001"
        bare = "CVE-2024-5300: systemd-userdbd"
        bare_id = "GHSA-tt1e-redc-0002"
        client = FakeClient(
            [
                sample_advisory(ghsa, title, cve="CVE-2024-5300"),
                sample_advisory(bare_id, bare, description="short"),
            ],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        markdown = tracker.render_markdown(report)
        rendered = tracker.render_json(report)
        for text in (markdown, rendered):
            self.assertIn(ghsa, text)
            self.assertIn("Ember Falcon", text)
            self.assertNotIn("systemd-userdbd", text)
            self.assertNotIn("CVE-2024-5300", text)
        description = tracker.adf_to_text(
            posts(client, "/rest/api/3/issue")[0]["fields"]["description"]
        )
        self.assertIn(title, description)
        messages = "\n".join(mattermost_texts(client))
        self.assertNotIn("systemd-userdbd", messages)
        self.assertNotIn(bare_id, messages)
        self.assertIn("advisory-tracker: new advisory detected", messages)
        self.assertIn(MAINTENANCE_TEXT, messages)

        stderr = io.StringIO()
        stdout = io.StringIO()
        log_client = FakeClient(
            [
                sample_advisory(ghsa, title, cve="CVE-2024-5300"),
                sample_advisory(bare_id, bare, description="short"),
            ],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = tracker.main(
                ["sync", "--config", CONFIG_PATH],
                env=environment(),
                client=log_client,
                now=NOW,
            )
        self.assertEqual(code, 0)
        logged = stdout.getvalue() + stderr.getvalue()
        self.assertIn(ghsa, logged)
        self.assertIn("Ember Falcon", logged)
        self.assertIn(bare_id, stderr.getvalue())
        self.assertNotIn("systemd-userdbd", logged)
        self.assertNotIn("CVE-2024-5300", logged)

    def test_titles_are_not_published_as_codenames(self):
        leaked = "GHSA-k3rn-m3m0-0001"
        titled = "GHSA-pr1v-esc0-0002"
        client = FakeClient(
            [
                sample_advisory(EMBER, "pins a map (Ember Falcon)"),
                sample_advisory(
                    leaked, "snapd (Kernel Memory Corruption)", description="short"
                ),
                sample_advisory(titled, "pins a map"),
            ],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                jira_issue("SNAPDENG-21", "Privilege Escalation", "Triage", titled),
            ],
        )
        report = run(client)
        markdown = tracker.render_markdown(report)
        rendered = tracker.render_json(report)
        messages = "\n".join(mattermost_texts(client))
        for text in (markdown, rendered):
            self.assertIn("Ember Falcon", text)
        for text in (markdown, rendered, messages):
            self.assertNotIn("Kernel Memory Corruption", text)
            self.assertNotIn("Privilege Escalation", text)
        self.assertNotIn("Privilege Escalation", str(client.updates))
        self.assertNotIn("Kernel Memory Corruption", str(client.updates))
        leaked_row = next(row for row in report.advisories if row.ghsa_id == leaked)
        published = json.loads(rendered)["advisories"]
        leaked_json = next(row for row in published if row["ghsa_id"] == leaked)
        self.assertIsNone(leaked_json["codename"])
        self.assertNotIn(leaked, messages)
        self.assertIn(MAINTENANCE_TEXT, messages)
        self.assertEqual(leaked_row.codename, "Kernel Memory Corruption")

    def test_critical_advisory_sets_the_mapped_priority(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", severity="critical")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        self.assertEqual(report.advisories[0].action, "created")
        fields = posts(client, "/rest/api/3/issue")[0]["fields"]
        self.assertEqual(fields["priority"], {"name": "Highest"})

    def test_codename_child_is_not_duplicated(self):
        manual = jira_issue(
            "SNAPDENG-21", "Ember Falcon", "Triage", description="manual notes"
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), manual],
        )
        report = run(client)
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-21")
        self.assertEqual(report.advisories[0].action, "unchanged")
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

    def test_existing_reference_is_not_created_again(self):
        labeled = jira_issue("SNAPDENG-21", "Ember Falcon", "Triage", EMBER)
        described = jira_issue(
            "SNAPDENG-22",
            "Lively Willow",
            "Triage",
            description=f"GitHub-Advisory: {WILLOW}",
        )
        client = FakeClient(
            [
                sample_advisory(EMBER, "pins a map (Ember Falcon)"),
                sample_advisory(WILLOW, "priv esc (aka Lively Willow)"),
            ],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                labeled,
                described,
            ],
        )
        client.properties["SNAPDENG-21"] = {"advisory_state": "triage", "stalls": {}}
        client.properties["SNAPDENG-22"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        actions = {row.ghsa_id: row.action for row in report.advisories}
        self.assertEqual(actions, {EMBER: "unchanged", WILLOW: "unchanged"})
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(client.updates, [])
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(posts(client, "/comment"), [])

    def test_not_ready_is_flagged_without_a_ticket(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", description="short")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "not_ready")
        self.assertIn("description is shorter than 80 characters", row.reasons)
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        messages = [
            json.loads(body.decode())["text"]
            for method, url, body in client.calls
            if url.startswith("https://hooks.mattermost.test/")
        ]
        self.assertEqual(messages, [MAINTENANCE_TEXT])
        self.assertNotIn(EMBER, messages[0])
        self.assertNotIn("Ember Falcon", messages[0])

    def test_missing_severity_is_created_for_triage(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", severity=None)],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.reasons, [])
        self.assertEqual(row.action, "created")
        fields = posts(client, "/rest/api/3/issue")[0]["fields"]
        self.assertNotIn("priority", fields)
        self.assertIn("Severity: not set", tracker.adf_to_text(fields["description"]))
        self.assertIn("CVE: not set", tracker.adf_to_text(fields["description"]))
        self.assertEqual(len(mattermost_texts(client)), 1)
        self.assertIn(
            "advisory-tracker: new advisory detected", mattermost_texts(client)[0]
        )
        self.assertNotIn("not ready", mattermost_texts(client)[0])

    def test_existing_petname_is_synced_onto_the_advisory(self):
        ghsa = "GHSA-hg3v-p5hf-j33r"
        issue = jira_issue("SNAPDENG-37558", "Salty Compass", "In Progress", ghsa)
        client = FakeClient(
            [sample_advisory(ghsa, "CVE-2024-5300: systemd-userdbd", state="draft")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        client.properties["SNAPDENG-37558"] = {"advisory_state": "draft", "stalls": {}}
        report = run(client)
        row = report.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(report.errors, [])
        self.assertEqual(row.codename, "Salty Compass")
        self.assertEqual(row.summary, "CVE-2024-5300: systemd-userdbd (Salty Compass)")
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(
            client.updates,
            [
                (
                    (
                        "https://api.github.com/repos/canonical/snapd/"
                        f"security-advisories/{ghsa}"
                    ),
                    {"summary": "CVE-2024-5300: systemd-userdbd (Salty Compass)"},
                )
            ],
        )

        client.updates.clear()
        client.calls.clear()
        client.advisories[0]["summary"] = "CVE-2024-5300: systemd-userdbd"
        preview = run(client, command="summary")
        preview_row = preview.advisories[0]
        self.assertEqual(preview_row.codename, "Salty Compass")
        self.assertEqual(preview_row.summary, "CVE-2024-5300: systemd-userdbd")
        self.assertEqual(client.updates, [])

    def test_existing_ticket_without_a_codename_is_tracked(self):
        ghsa = "GHSA-hg3v-p5hf-j33r"
        issue = jira_issue(
            "SNAPDENG-37558", "snapd allows pinning a bpf map", "Triage", ghsa
        )
        client = FakeClient(
            [sample_advisory(ghsa, "pins a bpf map")],
            [jira_issue("SNAPDENG-10", "not a codename", "Triage", QUIET), issue],
        )
        client.properties["SNAPDENG-37558"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client, command="summary")
        self.assertIsNone(report.confirmation_error)
        self.assertEqual(report.errors, [])
        row = report.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(row.action, "tracked")
        self.assertEqual(row.issue_key, "SNAPDENG-37558")
        self.assertIsNone(row.codename)
        self.assertEqual(client.updates, [])

    def test_multiple_tickets_are_tracked_without_creating_another(self):
        tracking = jira_issue(
            "SNAPDENG-37557", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        pull = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "In Review",
            description=f"Pull request for {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                tracking,
                pull,
                parent_epic(assignee=ADA),
            ],
        )
        client.properties["SNAPDENG-37557"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertEqual(
            row.error,
            f"{EMBER} SNAPDENG-37557 Done, SNAPDENG-37802 In Review: "
            "advisory state published != least progressed draft (SNAPDENG-37802), "
            "expected Done",
        )
        self.assertEqual(row.issue_key, "SNAPDENG-37557")
        self.assertEqual(row.issue_keys, ["SNAPDENG-37557", "SNAPDENG-37802"])
        self.assertEqual(row.issue_status, "In Review")
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "33"}}])
        self.assertEqual(client.updates, [])
        self.assertEqual(
            client._issue("SNAPDENG-37557")["fields"]["status"]["name"], "Done"
        )
        self.assertEqual(
            client._issue("SNAPDENG-37802")["fields"]["status"]["name"], "In Review"
        )

    def test_split_advisory_matches_least_progressed_triage(self):
        tracking = jira_issue("SNAPDENG-37557", "Ember Falcon", "Triage", EMBER)
        part = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "In Review",
            description=f"Pull request for {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="triage")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                tracking,
                part,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(row.action, "unchanged")
        self.assertEqual(row.issue_status, "Triage")
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(client.updates, [])

    def test_split_sync_leaves_a_more_progressed_ticket(self):
        tracking = jira_issue("SNAPDENG-37557", "Ember Falcon", "Triage", EMBER)
        review = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "In Review",
            description=f"Pull request for {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="draft")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                tracking,
                review,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(row.action, "transitioned")
        self.assertTrue(row.status_aligned)
        self.assertEqual(row.issue_status, "In Progress")
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "32"}}])
        self.assertEqual(
            client._issue("SNAPDENG-37557")["fields"]["status"]["name"], "In Progress"
        )
        self.assertEqual(
            client._issue("SNAPDENG-37802")["fields"]["status"]["name"], "In Review"
        )

    def test_split_sync_moves_every_ticket_at_the_least_status(self):
        first = jira_issue("SNAPDENG-37557", "Ember Falcon", "Triage", EMBER)
        second = jira_issue(
            "SNAPDENG-37802",
            "second part",
            "Triage",
            description=f"GitHub-Advisory: {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                first,
                second,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(row.action, "transitioned")
        self.assertEqual(row.issue_status, "Done")
        self.assertEqual(
            posts(client, "/transitions"),
            [{"transition": {"id": "33"}}, {"transition": {"id": "33"}}],
        )
        self.assertEqual(
            client._issue("SNAPDENG-37557")["fields"]["status"]["name"], "Done"
        )
        self.assertEqual(
            client._issue("SNAPDENG-37802")["fields"]["status"]["name"], "Done"
        )

    def test_split_sync_does_not_regress_a_more_progressed_ticket(self):
        review = jira_issue("SNAPDENG-37557", "Ember Falcon", "In Review", EMBER)
        done = jira_issue(
            "SNAPDENG-37802",
            "finished part",
            "Done",
            description=f"GitHub-Advisory: {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="triage")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                review,
                done,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIn(
            "least progressed draft (SNAPDENG-37557), expected Triage",
            row.error or "",
        )
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(
            client._issue("SNAPDENG-37557")["fields"]["status"]["name"], "In Review"
        )
        self.assertEqual(
            client._issue("SNAPDENG-37802")["fields"]["status"]["name"], "Done"
        )

    def test_split_summary_and_dry_run_do_not_transition(self):
        tracking = jira_issue("SNAPDENG-37557", "Ember Falcon", "Triage", EMBER)
        review = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "In Review",
            description=f"Pull request for {EMBER}",
        )
        catalog = [
            jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
            tracking,
            review,
        ]
        advisory = sample_advisory(
            EMBER, "pins a map (Ember Falcon)", state="published"
        )
        summary = run(FakeClient([advisory], catalog), command="summary")
        self.assertEqual(summary.advisories[0].action, "error")
        self.assertIn(
            "least progressed triage (SNAPDENG-37557), expected Done",
            summary.advisories[0].error or "",
        )
        client = FakeClient([advisory], catalog)
        report = run(client, dry_run=True)
        row = report.advisories[0]
        self.assertEqual(row.action, "would_transition")
        self.assertIn(
            "least progressed triage (SNAPDENG-37557), expected Done",
            row.error or "",
        )
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(
            client._issue("SNAPDENG-37557")["fields"]["status"]["name"], "Triage"
        )
        self.assertEqual(
            client._issue("SNAPDENG-37802")["fields"]["status"]["name"], "In Review"
        )

    def test_split_sync_reports_a_missing_transition(self):
        tracking = jira_issue("SNAPDENG-37557", "Ember Falcon", "Triage", EMBER)
        review = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "In Review",
            description=f"Pull request for {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                tracking,
                review,
            ],
        )
        client.transition_catalog = [("32", "In Progress")]
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIn(": no transition", row.error or "")
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(
            client._issue("SNAPDENG-37557")["fields"]["status"]["name"], "Triage"
        )
        self.assertEqual(
            client._issue("SNAPDENG-37802")["fields"]["status"]["name"], "In Review"
        )

    def test_container_epic_is_not_an_outside_ticket(self):
        epic = jira_issue(
            PARENT,
            "snapd advisories",
            "Triage",
            description=f"GitHub-Advisory: {EMBER}",
            parent=None,
        )
        quiet = jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        child = jira_issue("SNAPDENG-30", "Ember Falcon", "Triage", EMBER)
        tracked = run(FakeClient([advisory], [quiet, epic, child]))
        row = tracked.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(row.issue_keys, ["SNAPDENG-30"])

        created = run(FakeClient([advisory], [quiet, epic]))
        self.assertEqual(created.advisories[0].action, "created")
        self.assertIsNone(created.advisories[0].error)
        self.assertNotIn(PARENT, created.advisories[0].issue_keys)

    def test_outside_ticket_blocks_creation(self):
        outside = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "In Review",
            description=f"GitHub-Advisory: {EMBER}",
            parent=None,
        )
        quiet = jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        message = (
            f"{EMBER} SNAPDENG-37802: associated Jira ticket "
            f"is not a child of {PARENT}"
        )
        client = FakeClient([advisory], [quiet, outside])
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIsNone(row.issue_status)
        self.assertEqual(row.issue_keys, ["SNAPDENG-37802"])
        self.assertEqual(row.error, message)
        self.assertIn(message, report.errors)
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(mattermost_texts(client), [MAINTENANCE_TEXT])
        self.assertNotIn(EMBER, mattermost_texts(client)[0])

        summary = run(FakeClient([advisory], [quiet, outside]), command="summary")
        self.assertEqual(summary.advisories[0].error, message)

        other = jira_issue(
            "SNAPDENG-37803",
            "notes",
            "Triage",
            description=f"see {EMBER}",
            parent=None,
        )
        plural = run(FakeClient([advisory], [quiet, outside, other]))
        self.assertEqual(
            plural.advisories[0].error,
            f"{EMBER} SNAPDENG-37802, SNAPDENG-37803: associated Jira tickets "
            f"are not children of {PARENT}",
        )

    def test_outside_ticket_does_not_hold_back_the_epic_child(self):
        tracking = jira_issue("SNAPDENG-37557", "Ember Falcon", "Done", EMBER)
        pull = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "In Review",
            description=f"Pull request for {EMBER}",
            parent=None,
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                tracking,
                pull,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertEqual(
            row.error,
            f"{EMBER} SNAPDENG-37802: associated Jira ticket "
            f"is not a child of {PARENT}",
        )
        self.assertEqual(row.issue_status, "Done")
        self.assertEqual(row.issue_keys, ["SNAPDENG-37557", "SNAPDENG-37802"])
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(
            client._issue("SNAPDENG-37802")["fields"]["status"]["name"], "In Review"
        )

    def test_done_and_wont_do_match_published(self):
        done = jira_issue("SNAPDENG-37557", "Ember Falcon", "Done", EMBER)
        declined = jira_issue(
            "SNAPDENG-37802",
            "declined part",
            "Won't Do",
            description=f"GitHub-Advisory: {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                done,
                declined,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(row.action, "unchanged")
        self.assertEqual(row.issue_status, "Done")
        self.assertEqual(posts(client, "/transitions"), [])

    def test_only_wont_do_matches_closed(self):
        first = jira_issue("SNAPDENG-37557", "Ember Falcon", "Won't Do", EMBER)
        second = jira_issue(
            "SNAPDENG-37802",
            "declined part",
            "Won't Do",
            description=f"GitHub-Advisory: {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="closed")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                first,
                second,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertIsNone(row.error)
        self.assertEqual(row.issue_status, "Won't Do")
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(client.updates, [])

    def test_unknown_split_status_is_an_error(self):
        tracking = jira_issue("SNAPDENG-37557", "Ember Falcon", "Backlog", EMBER)
        part = jira_issue(
            "SNAPDENG-37802",
            "fix the pin",
            "Done",
            description=f"GitHub-Advisory: {EMBER}",
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                tracking,
                part,
            ],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIn("Jira status Backlog is not in status_progress", row.error or "")
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(
            client._issue("SNAPDENG-37557")["fields"]["status"]["name"], "Backlog"
        )

    def test_status_progress_config_is_validated(self):
        def loaded():
            with open(CONFIG_PATH, encoding="utf-8") as handle:
                return tracker.parse_yaml(handle.read())

        raw = loaded()
        raw["status_progress"] = {}
        with self.assertRaises(tracker.TrackerError) as empty:
            tracker.config_from_mapping(raw)
        self.assertIn("status_progress must not be empty", str(empty.exception))
        raw = loaded()
        raw["status_progress"]["Triage"] = "bogus"
        with self.assertRaises(tracker.TrackerError) as unknown:
            tracker.config_from_mapping(raw)
        self.assertIn(
            "status_progress.Triage is not an advisory state", str(unknown.exception)
        )
        raw = loaded()
        raw["status_progress"][2] = "triage"
        with self.assertRaises(tracker.TrackerError) as bad_name:
            tracker.config_from_mapping(raw)
        self.assertIn("status name must be a string", str(bad_name.exception))

    def test_same_word_mismatch_includes_expected_status(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Done", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="triage")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client, command="summary")
        self.assertEqual(
            report.advisories[0].error,
            f"{EMBER} SNAPDENG-40: advisory state triage "
            "!= Jira status Done, expected Triage",
        )

    def test_summary_reports_state_mismatch_without_changing_the_advisory(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client, command="summary")
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertEqual(
            row.error,
            f"{EMBER} SNAPDENG-40: advisory state published "
            "!= Jira status Triage, expected Done",
        )
        self.assertIn(row.error, report.errors)
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(client.updates, [])
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "Triage"
        )

    def test_drifted_jira_status_is_synced_from_the_advisory(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        client.properties["SNAPDENG-40"] = {"advisory_state": "published", "stalls": {}}
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "transitioned")
        self.assertEqual(row.issue_status, "Done")
        self.assertIsNone(row.error)
        self.assertEqual(report.errors, [])
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "33"}}])
        self.assertEqual(client.updates, [])

    def test_unsyncable_jira_status_is_reported(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Backlog", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertEqual(
            row.error,
            f"{EMBER} SNAPDENG-40: advisory state published "
            "!= Jira status Backlog, expected Done: no transition",
        )
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "Backlog"
        )
        self.assertNotIn("advisory_state", client.properties.get("SNAPDENG-40", {}))
        self.assertEqual(client.updates, [])

    def test_existing_ticket_follows_published_advisory_state(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        client.properties["SNAPDENG-40"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "transitioned")
        self.assertEqual(row.issue_status, "Done")
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "33"}}])
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(
            client.properties["SNAPDENG-40"]["advisory_state"], "published"
        )

    def test_published_advisory_without_a_ticket_is_not_backfilled(self):
        client = FakeClient(
            [
                sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published"),
                sample_advisory(WILLOW, "priv esc (aka Lively Willow)", state="closed"),
            ],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        self.assertEqual({row.action for row in report.advisories}, {"skipped"})
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(client.updates, [])
        self.assertFalse(
            any(
                url.startswith("https://hooks.mattermost.test/")
                for _, url, _ in client.calls
            )
        )

    def test_stall_channels_and_cooldown(self):
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                issue,
                parent_epic(assignee=ADA),
            ],
        )
        client.properties["SNAPDENG-30"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertEqual(
            row.stalls,
            [
                tracker.Stall(
                    "SNAPDENG-30",
                    ["not assigned: no assignee within 2 days of ticket creation"],
                )
            ],
        )
        self.assertEqual(
            row.notifications,
            ["sent jira_comment assigned", "sent mattermost assigned"],
        )
        body = posts(client, "/comment")[0]["body"]
        panel = body["content"][0]
        self.assertEqual(panel["attrs"], {"panelType": "warning"})
        self.assertEqual(panel["content"][0]["attrs"], {"level": 4})
        comment = tracker.adf_to_text(body)
        self.assertIn("advisory-tracker: not assigned", comment)
        self.assertIn("@Ada", comment)
        self.assertIn(
            "Warning: SNAPDENG-30 (Ember Falcon): "
            "not assigned: no assignee within 2 days of ticket creation",
            comment,
        )
        self.assertIn(f"Link: {row.url}", comment)
        self.assertEqual(mattermost_texts(client, MAINTENANCE_HOOK), [MAINTENANCE_TEXT])
        self.assertEqual(mattermost_texts(client, NEW_ADVISORY_HOOK), [])
        self.assertEqual(client.properties["SNAPDENG-30"]["advisory_state"], "triage")
        self.assertEqual(
            set(client.properties["SNAPDENG-30"]["stalls"]["assigned"]),
            {"mattermost", "jira_comment"},
        )

        quiet = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                jira_issue(
                    "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
                ),
            ],
        )
        quiet.properties["SNAPDENG-30"] = {
            "advisory_state": "triage",
            "stalls": {"assigned": (NOW - timedelta(days=1)).isoformat()},
        }
        cooled = run(quiet)
        cooled_row = next(item for item in cooled.advisories if item.ghsa_id == EMBER)
        self.assertEqual(
            cooled_row.stalls,
            [
                tracker.Stall(
                    "SNAPDENG-30",
                    ["not assigned: no assignee within 2 days of ticket creation"],
                )
            ],
        )
        self.assertEqual(cooled_row.notifications, ["cooldown assigned"])
        self.assertEqual(posts(quiet, "/comment"), [])
        self.assertEqual(mattermost_texts(quiet), [])

    def test_failed_mattermost_does_not_repeat_the_jira_comment(self):
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                issue,
                parent_epic(assignee=ADA),
            ],
        )
        client.properties["SNAPDENG-30"] = {"advisory_state": "triage", "stalls": {}}
        client.mattermost_status = 500
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertEqual(row.notifications, ["sent jira_comment assigned"])
        self.assertIn("cannot send mattermost message: HTTP 500", row.error or "")
        self.assertEqual(len(posts(client, "/comment")), 1)
        assigned = client.properties["SNAPDENG-30"]["stalls"]["assigned"]
        self.assertEqual(set(assigned), {"jira_comment"})
        jira_stamp = assigned["jira_comment"]

        again = run(client)
        again_row = next(item for item in again.advisories if item.ghsa_id == EMBER)
        self.assertEqual(again_row.notifications, ["cooldown jira_comment assigned"])
        self.assertIn("cannot send mattermost message: HTTP 500", again_row.error or "")
        self.assertEqual(len(posts(client, "/comment")), 1)
        self.assertEqual(len(mattermost_texts(client)), 2)
        self.assertEqual(
            client.properties["SNAPDENG-30"]["stalls"]["assigned"],
            {"jira_comment": jira_stamp},
        )

        client.mattermost_status = 200
        recovered = run(client)
        recovered_row = next(
            item for item in recovered.advisories if item.ghsa_id == EMBER
        )
        self.assertEqual(
            recovered_row.notifications,
            ["cooldown jira_comment assigned", "sent mattermost assigned"],
        )
        self.assertEqual(len(posts(client, "/comment")), 1)
        stored = client.properties["SNAPDENG-30"]["stalls"]["assigned"]
        self.assertEqual(stored["jira_comment"], jira_stamp)
        self.assertIn("mattermost", stored)

    def test_channel_selection_limits_sends(self):
        config = replace(
            tracker.load_config(CONFIG_PATH),
            notify={"created": (), "not_ready": (), "stall": ("mattermost",)},
        )
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                issue,
                parent_epic(assignee=ADA),
            ],
        )
        client.properties["SNAPDENG-30"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client, config=config)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertEqual(row.notifications, ["sent mattermost assigned"])
        self.assertEqual(posts(client, "/comment"), [])

    def test_stall_comment_mentions_the_ticket_assignee(self):
        created = (NOW - timedelta(days=8)).strftime("%Y-%m-%dT%H:%M:%S.000+0000")
        issue = jira_issue(
            "SNAPDENG-30",
            "Ember Falcon",
            "Triage",
            EMBER,
            created=created,
            assignee=ADA,
        )
        self._assert_mention(issue, parent_epic(assignee=RAE), "ada-id", "@Ada")

    def test_stall_comment_mentions_the_epic_assignee(self):
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        self._assert_mention(issue, parent_epic(assignee=RAE), "rae-id", "@Rae")

    def test_stall_comment_mentions_the_epic_reporter(self):
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        self._assert_mention(issue, parent_epic(reporter=RAE), "rae-id", "@Rae")

    def test_stall_comment_without_a_person_has_no_mention(self):
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                issue,
                parent_epic(),
            ],
        )
        client.properties["SNAPDENG-30"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertIsNone(row.error)
        self.assertEqual(len(posts(client, "/comment")), 1)
        self.assertEqual(mention_nodes(posts(client, "/comment")[0]["body"]), [])
        self.assertIn("assigned", client.properties["SNAPDENG-30"]["stalls"])

    def test_missing_epic_still_posts_the_stall_comment(self):
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        client.properties["SNAPDENG-30"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertIn("HTTP 404", row.error)
        self.assertEqual(len(posts(client, "/comment")), 1)
        self.assertEqual(mention_nodes(posts(client, "/comment")[0]["body"]), [])
        self.assertEqual(len(mattermost_texts(client)), 1)
        self.assertIn("assigned", client.properties["SNAPDENG-30"]["stalls"])

    def test_assigned_ticket_without_an_account_id_is_not_replaced(self):
        created = (NOW - timedelta(days=8)).strftime("%Y-%m-%dT%H:%M:%S.000+0000")
        issue = jira_issue(
            "SNAPDENG-30",
            "Ember Falcon",
            "Triage",
            EMBER,
            created=created,
            assignee={"displayName": "Ada"},
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                issue,
                parent_epic(assignee=RAE),
            ],
        )
        client.properties["SNAPDENG-30"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertIsNone(row.error)
        self.assertEqual(mention_nodes(posts(client, "/comment")[0]["body"]), [])

    def _assert_mention(self, issue, epic, account_id, text):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                issue,
                epic,
            ],
        )
        client.properties["SNAPDENG-30"] = {"advisory_state": "triage", "stalls": {}}
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertIsNone(row.error)
        body = posts(client, "/comment")[0]["body"]
        mentions = mention_nodes(body)
        self.assertEqual(
            [(item["attrs"]["id"], item["attrs"]["text"]) for item in mentions],
            [(account_id, text)],
        )
        self.assertNotIn("@", mattermost_texts(client)[0])
        self.assertTrue(client.properties["SNAPDENG-30"]["stalls"])

    def test_summary_is_read_only_json(self):
        issue = jira_issue(
            "SNAPDENG-30", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        client = FakeClient(
            [
                sample_advisory(EMBER, "pins a map (Ember Falcon)"),
                sample_advisory(WILLOW, "priv esc (aka Lively Willow)", severity=None),
                sample_advisory(KEEN, "pins a map (Keen Yak)", description="short"),
            ],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        stdout = io.StringIO()
        stderr = io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = tracker.main(
                ["summary", "--format", "json", "--config", CONFIG_PATH],
                env=environment(),
                client=client,
                now=NOW,
            )
        self.assertEqual(code, 0)
        self.assertIn("is not ready:", stderr.getvalue())
        payload = json.loads(stdout.getvalue())
        self.assertEqual(payload["parent"], PARENT)
        self.assertEqual(payload["mode"], "summary")
        self.assertIsNone(payload["confirmation_error"])
        by_id = {row["ghsa_id"]: row for row in payload["advisories"]}
        self.assertEqual(by_id[EMBER]["action"], "tracked")
        self.assertEqual(by_id[EMBER]["state"], "triage")
        self.assertEqual(by_id[EMBER]["issue_status"], "Triage")
        self.assertEqual(by_id[EMBER]["expected_status"], "Triage")
        self.assertEqual(by_id[EMBER]["issue_key"], "SNAPDENG-30")
        self.assertEqual(
            by_id[EMBER]["stalls"],
            [
                {
                    "issue_key": "SNAPDENG-30",
                    "reasons": [
                        "not assigned: no assignee within 2 days of ticket creation"
                    ],
                }
            ],
        )
        self.assertEqual(by_id[EMBER]["codename"], "Ember Falcon")
        self.assertTrue(by_id[WILLOW]["ready"])
        self.assertEqual(by_id[WILLOW]["reasons"], [])
        self.assertEqual(by_id[WILLOW]["action"], "untracked")
        self.assertFalse(by_id[KEEN]["ready"])
        self.assertEqual(by_id[KEEN]["action"], "not_ready")
        self.assertNotIn("missing severity", stderr.getvalue())
        for method, url, _ in client.calls:
            if method == "PUT":
                self.fail(url)
            if method == "POST" and "search/jql" not in url:
                self.fail(url)

    def test_summary_omits_settled_advisories_without_an_error(self):
        published = jira_issue("SNAPDENG-40", "Ember Falcon", "Done", EMBER)
        closed = jira_issue("SNAPDENG-42", "Calm Mink", "Won't Do", CALM)
        open_issue = jira_issue("SNAPDENG-41", "Lively Willow", "Triage", WILLOW)
        client = FakeClient(
            [
                sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published"),
                sample_advisory(CALM, "local socket race (Calm Mink)", state="closed"),
                sample_advisory(WILLOW, "priv esc (aka Lively Willow)", state="triage"),
            ],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                published,
                closed,
                open_issue,
            ],
        )
        report = run(client, command="summary")
        self.assertIsNone(report.advisories[0].error)
        self.assertIsNone(report.advisories[1].error)
        markdown = tracker.render_markdown(report, omit_settled=True)
        self.assertNotIn(EMBER, markdown)
        self.assertNotIn(CALM, markdown)
        self.assertIn(WILLOW, markdown)
        self.assertIn(
            "Omitting 2 published or closed advisories with no error.", markdown
        )
        payload = json.loads(tracker.render_json(report))
        self.assertEqual(payload["omitted_advisories"], 2)
        self.assertEqual([row["ghsa_id"] for row in payload["advisories"]], [WILLOW])

    def test_summary_keeps_settled_advisory_with_an_error(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client, command="summary")
        self.assertIn(report.advisories[0].error, report.errors)
        markdown = tracker.render_markdown(report, omit_settled=True)
        self.assertIn(EMBER, markdown)
        self.assertIn("advisory state published != Jira status Triage", markdown)
        payload = json.loads(tracker.render_json(report))
        self.assertEqual(payload["omitted_advisories"], 0)
        self.assertEqual([row["ghsa_id"] for row in payload["advisories"]], [EMBER])

    def test_sync_markdown_lists_settled_advisories(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Done", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), issue],
        )
        report = run(client)
        markdown = tracker.render_markdown(report)
        self.assertIn(EMBER, markdown)
        self.assertNotIn("Omitting", markdown)
        payload = json.loads(tracker.render_json(report))
        self.assertNotIn("omitted_advisories", payload)
        self.assertEqual(payload["advisories"][0]["ghsa_id"], EMBER)

    def test_confirmation_failure_refuses_writes(self):
        client = FakeClient([sample_advisory(EMBER, "pins a map (Ember Falcon)")], [])
        stdout = io.StringIO()
        stderr = io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = tracker.main(
                ["sync", "--config", CONFIG_PATH],
                env=environment(),
                client=client,
                now=NOW,
            )
        self.assertEqual(code, 2)
        self.assertIn("no child tickets", stderr.getvalue())
        self.assertIn("refused", stdout.getvalue())
        self.assertNotIn("would_create", stdout.getvalue())
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(posts(client, "/transitions"), [])

    def test_github_forbidden_names_the_advisory_token(self):
        client = FakeClient(
            [], [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)]
        )
        client.github_status = 403
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertEqual(caught.exception.status, 403)
        self.assertIn("SNAPD_ADVISORY_GITHUB_TOKEN", str(caught.exception))

    def test_advisory_pages_are_followed(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.extra_advisories = [
            sample_advisory(WILLOW, "priv esc (aka Lively Willow)", state="published")
        ]
        report = run(client)
        self.assertEqual({row.ghsa_id for row in report.advisories}, {EMBER, WILLOW})
        self.assertEqual({row.action for row in report.advisories}, {"skipped"})

    def test_dry_run_does_not_create(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, dry_run=True)
        self.assertEqual(report.mode, "dry-run")
        self.assertEqual(report.advisories[0].action, "would_create")
        self.assertIn("would mattermost created", report.advisories[0].notifications)
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(mattermost_texts(client), [])

    def test_created_ticket_is_announced_once(self):
        title = "CVE-2024-5300: systemd-userdbd (Ember Falcon)"
        client = FakeClient(
            [sample_advisory(EMBER, title, cve="CVE-2024-5300")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        self.assertEqual(report.advisories[0].action, "created")
        self.assertEqual(mattermost_texts(client, MAINTENANCE_HOOK), [])
        self.assertEqual(len(mattermost_texts(client, NEW_ADVISORY_HOOK)), 1)
        message = mattermost_texts(client, NEW_ADVISORY_HOOK)[0]
        self.assertIn("advisory-tracker: new advisory detected", message)
        self.assertNotIn("⚠️", message)
        self.assertNotIn("Warning:", message)
        self.assertIn("SNAPDENG-500 (Ember Falcon): Jira ticket created", message)
        self.assertIn(f"GitHub-Advisory: {EMBER}", message)
        self.assertIn(f"Link: {report.advisories[0].url}", message)
        self.assertNotIn("systemd-userdbd", message)
        again = run(client)
        self.assertNotEqual(again.advisories[0].action, "created")
        self.assertEqual(len(mattermost_texts(client)), 1)

        quiet = FakeClient(
            [sample_advisory(EMBER, title, cve="CVE-2024-5300")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        env = environment()
        env["ADVISORY_TRACKER_NOTIFY"] = "false"
        silent = run(quiet, env=env)
        self.assertEqual(silent.advisories[0].action, "created")
        self.assertIn("notify disabled", silent.advisories[0].notifications)
        self.assertEqual(mattermost_texts(quiet), [])

    def test_dry_run_reports_one_maintenance_link(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", description="short")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, dry_run=True)
        self.assertIn(
            "would mattermost maintenance", report.advisories[0].notifications
        )
        self.assertEqual(mattermost_texts(client), [])

    def test_remaining_error_posts_the_maintenance_link_again(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            quiet_catalog(issue),
        )
        client.transition_catalog = [("32", "In Progress")]
        first = run(client)
        self.assertIn("no transition", first.advisories[0].error or "")
        self.assertEqual(mattermost_texts(client, MAINTENANCE_HOOK), [MAINTENANCE_TEXT])
        self.assertEqual(mattermost_texts(client, NEW_ADVISORY_HOOK), [])
        run(client)
        self.assertEqual(
            mattermost_texts(client, MAINTENANCE_HOOK),
            [MAINTENANCE_TEXT, MAINTENANCE_TEXT],
        )

    def test_missing_new_advisory_webhook_still_creates_the_ticket(self):
        env = environment()
        del env["MATTERMOST_NEW_ADVISORY_WEBHOOK_URL"]
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, env=env)
        row = report.advisories[0]
        self.assertEqual(row.action, "created")
        self.assertIn(
            "missing MATTERMOST_NEW_ADVISORY_WEBHOOK_URL",
            row.error or "",
        )
        self.assertEqual(len(posts(client, "/rest/api/3/issue")), 1)
        self.assertEqual(mattermost_texts(client, NEW_ADVISORY_HOOK), [])
        self.assertEqual(mattermost_texts(client, MAINTENANCE_HOOK), [MAINTENANCE_TEXT])

    def test_missing_run_id_does_not_post_maintenance(self):
        env = environment()
        del env["GITHUB_RUN_ID"]
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", description="short")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, env=env)
        self.assertIn("missing GITHUB_RUN_ID", report.advisories[0].error or "")
        self.assertEqual(mattermost_texts(client), [])
        self.assertNotIn(EMBER, client.properties.get(PARENT, {}).get("not_ready", {}))

    def test_create_permission_off_does_not_create(self):
        env = environment()
        env["ADVISORY_TRACKER_CREATE"] = "false"
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, env=env)
        row = report.advisories[0]
        self.assertEqual(row.action, "create_disabled")
        self.assertIsNone(row.error)
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertIn(
            "Permissions: create off, notify on, sync on",
            tracker.render_markdown(report),
        )
        payload = json.loads(tracker.render_json(report))
        self.assertEqual(
            payload["permissions"],
            {"create": False, "notify": True, "sync": True},
        )
        dry = run(client, env=env, dry_run=True)
        self.assertEqual(dry.advisories[0].action, "create_disabled")

    def test_notify_permission_off_still_creates_and_syncs(self):
        env = environment()
        env["ADVISORY_TRACKER_NOTIFY"] = "false"
        late = jira_issue(
            "SNAPDENG-40",
            "Ember Falcon",
            "Triage",
            EMBER,
            created=OLD_STAMP,
        )
        client = FakeClient(
            [
                sample_advisory(EMBER, "pins a map (Ember Falcon)", state="draft"),
                sample_advisory(WILLOW, "keeps a log (Lively Willow)"),
            ],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET), late],
        )
        report = run(client, env=env)
        ember = next(row for row in report.advisories if row.ghsa_id == EMBER)
        willow = next(row for row in report.advisories if row.ghsa_id == WILLOW)
        self.assertEqual(ember.action, "transitioned")
        self.assertEqual(ember.issue_status, "In Progress")
        self.assertTrue(ember.stalls)
        self.assertIn("notify disabled", ember.notifications)
        self.assertEqual(willow.action, "created")
        self.assertEqual(mattermost_texts(client), [])
        self.assertEqual(posts(client, "/comment"), [])
        self.assertEqual(len(posts(client, "/rest/api/3/issue")), 1)

    def test_sync_permission_off_creates_and_alerts_without_transition(self):
        env = environment()
        env["ADVISORY_TRACKER_SYNC"] = "false"
        late = jira_issue(
            "SNAPDENG-40",
            "Lively Willow",
            "Triage",
            WILLOW,
            created=OLD_STAMP,
        )
        client = FakeClient(
            [
                sample_advisory(EMBER, "pins a map (Ember Falcon)"),
                sample_advisory(
                    WILLOW, "keeps a log (Lively Willow)", state="published"
                ),
            ],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                late,
                parent_epic(assignee=ADA),
            ],
        )
        report = run(client, env=env)
        ember = next(row for row in report.advisories if row.ghsa_id == EMBER)
        willow = next(row for row in report.advisories if row.ghsa_id == WILLOW)
        self.assertEqual(ember.action, "created")
        self.assertEqual(ember.issue_status, "Backlog")
        self.assertIsNone(ember.error)
        self.assertTrue(willow.stalls)
        self.assertIsNone(willow.error)
        self.assertEqual(report.errors, [])
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(len(posts(client, "/rest/api/3/issue")), 1)
        self.assertEqual(len(mattermost_texts(client)), 2)
        self.assertEqual(len(posts(client, "/comment")), 1)

    def test_choose_codename_skips_existing_names(self):
        self.assertEqual(tracker.petname_from_summary("Salty Compass"), "Salty Compass")
        self.assertEqual(
            tracker.petname_from_summary(
                "Salty Compass: CVE-2024-5300: systemd-userdbd"
            ),
            "Salty Compass",
        )
        self.assertEqual(
            tracker.petname_from_summary("systemd-userdbd (Salty Compass)"),
            "Salty Compass",
        )
        self.assertIsNone(
            tracker.petname_from_summary("CVE-2024-5300: systemd-userdbd")
        )
        self.assertEqual(tracker.format_petname("keen-yak"), "Keen Yak")
        self.assertEqual(
            tracker.format_petname("relishing lionfish"), "Relishing Lionfish"
        )
        self.assertEqual(
            tracker.codename_key("Keen Yak"), tracker.codename_key("keen yak 2")
        )
        used = {
            tracker.codename_key(name)
            for name in ("Ember Falcon", "Quiet Lantern", "Lively Willow 2")
        }
        values = iter(
            ["keen yak 2", "EMBER-FALCON", "quiet-lantern", "lively willow", "keen yak"]
        )
        self.assertEqual(
            tracker.choose_codename(lambda: next(values), used), "Keen Yak"
        )
        self.assertIn(tracker.codename_key("Keen Yak"), used)
        values = iter(["keen yak", "calm mink"])
        self.assertEqual(
            tracker.choose_codename(lambda: next(values), used), "Calm Mink"
        )
        with self.assertRaises(tracker.TrackerError) as caught:
            tracker.choose_codename(lambda: "calm mink", used)
        self.assertIn("no unused petname after 20 draws", str(caught.exception))

    def test_used_codenames_come_from_advisories_and_children(self):
        advisories = [
            sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published"),
            sample_advisory(WILLOW, "priv esc (aka Lively Willow)", state="closed"),
        ]
        children = [
            issue_view(summary="Quiet Lantern"),
            issue_view(key="SNAPDENG-11", summary="Bold Otter 2"),
            issue_view(key="SNAPDENG-12", summary="not a codename"),
        ]
        used = tracker.collect_used_codenames(advisories, children)
        self.assertIn(tracker.codename_key("Ember Falcon"), used)
        self.assertIn(tracker.codename_key("Lively Willow"), used)
        self.assertIn(tracker.codename_key("Quiet Lantern"), used)
        self.assertIn(tracker.codename_key("Bold Otter"), used)
        self.assertNotIn("not a codename", used)

    def test_petname_words_asks_for_two_words(self):
        class Result:
            returncode = 0
            stdout = "keen yak\n"

        recorded = {}

        def fake_run(args, check, capture_output, text, timeout):
            recorded["args"] = args
            recorded["check"] = check
            return Result()

        original = tracker.subprocess.run
        tracker.subprocess.run = fake_run
        try:
            self.assertEqual(tracker.petname_words(), "keen yak")
        finally:
            tracker.subprocess.run = original
        self.assertEqual(
            recorded["args"], ["petname", "--words", "2", "--separator", " "]
        )
        self.assertFalse(recorded["check"])

    def test_dry_run_does_not_generate_a_name(self):
        def source():
            raise AssertionError("generated a name")

        client = FakeClient(
            [sample_advisory(KEEN, "pins a map")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, dry_run=True, name_source=source)
        self.assertEqual(report.advisories[0].action, "would_create")
        self.assertIsNone(report.advisories[0].codename)
        self.assertEqual(client.updates, [])

    def test_generated_names_are_unique_and_patched_after_create(self):
        client = FakeClient(
            [
                sample_advisory(EMBER, "old report (Ember Falcon)", state="published"),
                sample_advisory(KEEN, "pins a map"),
                sample_advisory(CALM, "local socket race"),
            ],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                jira_issue("SNAPDENG-11", "Bold Otter 2", "Triage"),
            ],
        )
        report = run(
            client,
            name_source=names(
                "EMBER-FALCON",
                "quiet lantern",
                "bold otter",
                "keen yak",
                "keen yak",
                "calm mink",
            ),
        )
        by_id = {row.ghsa_id: row for row in report.advisories}
        self.assertEqual(by_id[EMBER].action, "skipped")
        self.assertEqual(by_id[KEEN].action, "created")
        self.assertEqual(by_id[CALM].action, "created")
        self.assertEqual(by_id[KEEN].codename, "Keen Yak")
        self.assertEqual(by_id[CALM].codename, "Calm Mink")
        self.assertEqual(by_id[KEEN].summary, "pins a map (Keen Yak)")
        self.assertEqual(by_id[CALM].summary, "local socket race (Calm Mink)")
        created = posts(client, "/rest/api/3/issue")
        self.assertEqual(
            [item["fields"]["summary"] for item in created], ["Keen Yak", "Calm Mink"]
        )
        self.assertEqual(
            [payload["summary"] for _, payload in client.updates],
            ["pins a map (Keen Yak)", "local socket race (Calm Mink)"],
        )
        create_at = [
            index
            for index, (method, url, _) in enumerate(client.calls)
            if method == "POST" and url.endswith("/rest/api/3/issue")
        ]
        patch_at = [
            index
            for index, (method, _, _) in enumerate(client.calls)
            if method == "PATCH"
        ]
        self.assertLess(create_at[0], patch_at[0])
        self.assertLess(patch_at[0], create_at[1])
        self.assertLess(create_at[1], patch_at[1])
        self.assertTrue(client.updates[0][0].endswith("/" + KEEN))
        self.assertTrue(client.updates[1][0].endswith("/" + CALM))

    def test_failed_advisory_rename_is_retried_without_a_second_ticket(self):
        drawn = {"count": 0}

        def source():
            drawn["count"] += 1
            return "keen yak"

        client = FakeClient(
            [sample_advisory(KEEN, "pins a map")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.patch_status = 500
        report = run(client, name_source=source)
        row = report.advisories[0]
        self.assertEqual(row.action, "created")
        self.assertEqual(row.issue_key, "SNAPDENG-500")
        self.assertIn("cannot update advisory summary", row.error or "")
        self.assertEqual(drawn["count"], 1)
        self.assertEqual(client.advisories[0]["summary"], "pins a map")
        self.assertEqual(client.catalog[-1]["fields"]["summary"], "Keen Yak")

        client.calls.clear()
        client.updates.clear()
        client.patch_status = 200

        def boom():
            raise AssertionError("generated a second name")

        again = run(client, name_source=boom)
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])
        self.assertEqual(
            [
                issue["key"]
                for issue in client.catalog
                if issue["key"] == "SNAPDENG-500"
            ],
            ["SNAPDENG-500"],
        )
        self.assertEqual(again.advisories[0].codename, "Keen Yak")
        self.assertEqual(again.advisories[0].summary, "pins a map (Keen Yak)")
        self.assertIsNone(again.advisories[0].error)
        self.assertEqual(
            client.updates,
            [
                (
                    (
                        "https://api.github.com/repos/canonical/snapd/"
                        f"security-advisories/{KEEN}"
                    ),
                    {"summary": "pins a map (Keen Yak)"},
                )
            ],
        )

    def test_dry_run_would_transition_without_writing(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            quiet_catalog(issue),
        )
        report = run(client, dry_run=True)
        row = report.advisories[0]
        self.assertEqual(row.action, "would_transition")
        self.assertEqual(row.issue_status, "Triage")
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "Triage"
        )
        self.assertNotIn("SNAPDENG-40", client.properties)
        self.assertEqual(posts(client, "/transitions"), [])

    def test_dry_run_would_rename_an_existing_petname(self):
        ghsa = "GHSA-hg3v-p5hf-j33r"
        issue = jira_issue("SNAPDENG-37558", "Salty Compass", "In Progress", ghsa)
        client = FakeClient(
            [sample_advisory(ghsa, "CVE-2024-5300: systemd-userdbd", state="draft")],
            quiet_catalog(issue),
        )
        report = run(client, dry_run=True)
        row = report.advisories[0]
        self.assertIn("would rename advisory Salty Compass", row.notifications)
        self.assertEqual(row.summary, "CVE-2024-5300: systemd-userdbd")
        self.assertEqual(client.updates, [])
        self.assertEqual(posts(client, "/transitions"), [])

    def test_draft_advisory_transitions_to_in_progress(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="draft")],
            quiet_catalog(issue),
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "transitioned")
        self.assertEqual(row.issue_status, "In Progress")
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "32"}}])

    def test_closed_advisory_transitions_to_wont_do(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Done", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="closed")],
            quiet_catalog(issue),
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "transitioned")
        self.assertEqual(row.issue_status, "Won't Do")
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "34"}}])
        self.assertEqual(client.properties["SNAPDENG-40"]["advisory_state"], "closed")

    def test_single_in_review_ticket_is_moved_to_in_progress(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "In Review", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="draft")],
            quiet_catalog(issue),
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "transitioned")
        self.assertEqual(row.issue_status, "In Progress")
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "32"}}])
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "In Progress"
        )

    def _assert_failed_stall_kept_the_transition(self, client, report, detail):
        row = report.advisories[0]
        self.assertEqual(row.action, "transitioned")
        self.assertEqual(row.issue_status, "Done")
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "Done"
        )
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "33"}}])
        self.assertIn(detail, row.error or "")
        self.assertIn(f"{EMBER}: {detail}", report.errors)
        stored = client.properties["SNAPDENG-40"]
        self.assertEqual(stored["advisory_state"], "published")
        self.assertEqual(set(stored["stalls"]["assigned"]), {"jira_comment"})
        self.assertEqual(row.notifications, ["sent jira_comment assigned"])
        self.assertEqual(len(posts(client, "/comment")), 1)

    def test_mattermost_failure_does_not_undo_the_transition(self):
        client = aging_published_client()
        client.mattermost_status = 500
        report = run(client, config=config_with_open_done_stage())
        self._assert_failed_stall_kept_the_transition(
            client, report, "cannot send mattermost message: HTTP 500"
        )

    def test_missing_mattermost_webhook_does_not_undo_the_transition(self):
        client = aging_published_client()
        env = environment()
        del env["MATTERMOST_MAINTENANCE_WEBHOOK_URL"]
        report = run(client, config=config_with_open_done_stage(), env=env)
        self._assert_failed_stall_kept_the_transition(
            client,
            report,
            "cannot send mattermost message: "
            "missing MATTERMOST_MAINTENANCE_WEBHOOK_URL",
        )

    def test_advisory_summary_forbidden_aborts_the_run(self):
        ghsa = "GHSA-hg3v-p5hf-j33r"
        issue = jira_issue("SNAPDENG-37558", "Salty Compass", "In Progress", ghsa)
        client = FakeClient(
            [sample_advisory(ghsa, "CVE-2024-5300: systemd-userdbd", state="draft")],
            quiet_catalog(issue),
        )
        client.patch_status = 403
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertEqual(caught.exception.status, 403)
        self.assertIn("SNAPD_ADVISORY_GITHUB_TOKEN", str(caught.exception))
        self.assertEqual(
            client.advisories[0]["summary"], "CVE-2024-5300: systemd-userdbd"
        )
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

    def test_rename_failure_does_not_block_sync(self):
        issue = jira_issue(
            "SNAPDENG-40", "Ember Falcon", "Triage", EMBER, created=OLD_STAMP
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map", state="draft")],
            quiet_catalog(issue) + [parent_epic(assignee=ADA)],
        )
        client.patch_status = 500
        report = run(client)
        row = report.advisories[0]
        self.assertIn("cannot update advisory summary", row.error or "")
        self.assertEqual(row.issue_status, "In Progress")
        self.assertEqual(posts(client, "/transitions"), [{"transition": {"id": "32"}}])
        self.assertEqual(len(posts(client, "/comment")), 1)
        self.assertTrue(row.stalls)

    def test_advisory_list_must_be_a_list(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.github_body = b'{"message":"nope"}'
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertIn("response was not a list", str(caught.exception))

    def test_create_without_a_key_does_not_transition(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.omit_create_key = True
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIn("response did not include a key", row.error or "")
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual([issue["key"] for issue in client.catalog], ["SNAPDENG-10"])

    def test_property_read_failure_does_not_transition(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            quiet_catalog(issue),
        )
        client.property_status = 500
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIn("cannot read advisory property for SNAPDENG-40", row.error or "")
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "Triage"
        )
        self.assertEqual(posts(client, "/transitions"), [])

    def test_missing_ghsa_id_creates_nothing(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        advisory["ghsa_id"] = ""
        client = FakeClient(
            [advisory],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertEqual(row.error, "advisory is missing a GHSA id")
        self.assertIn(row.error, report.errors)
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

    def test_unknown_advisory_state_is_not_transitioned(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="withdrawn")],
            quiet_catalog(issue),
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIn(
            "advisory state withdrawn is not in the state map", row.error or ""
        )
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "Triage"
        )
        self.assertEqual(posts(client, "/transitions"), [])

    def test_existing_ticket_still_reports_that_it_is_not_ready(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        advisory = sample_advisory(
            EMBER, "pins a map (Ember Falcon)", description="short"
        )
        client = FakeClient([advisory], quiet_catalog(issue))
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "unchanged")
        self.assertIn("description is shorter than 80 characters", row.reasons)
        self.assertEqual(mattermost_texts(client), [MAINTENANCE_TEXT])
        self.assertNotIn(EMBER, mattermost_texts(client)[0])
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

        summary_client = FakeClient([advisory], quiet_catalog(issue))
        summary = run(summary_client, command="summary")
        summary_row = summary.advisories[0]
        self.assertEqual(summary_row.action, "tracked")
        self.assertIn("description is shorter than 80 characters", summary_row.reasons)
        self.assertEqual(summary_row.notifications, [])
        self.assertEqual(mattermost_texts(summary_client), [])

    def test_short_published_advisory_without_a_ticket_is_skipped(self):
        client = FakeClient(
            [
                sample_advisory(
                    EMBER,
                    "pins a map (Ember Falcon)",
                    state="published",
                    description="short",
                )
            ],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "skipped")
        self.assertEqual(mattermost_texts(client), [])
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

    def test_not_ready_is_not_sent_again_within_seven_days(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", description="short")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        first = run(client)
        self.assertEqual(first.advisories[0].action, "not_ready")
        self.assertEqual(len(mattermost_texts(client)), 1)
        second = run(client)
        self.assertEqual(second.advisories[0].notifications, ["cooldown not-ready"])
        self.assertEqual(len(mattermost_texts(client)), 1)

    def test_linked_ticket_keeps_the_epic_not_ready_cooldown(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", description="short")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        run(client)
        self.assertEqual(len(mattermost_texts(client)), 1)
        self.assertIn(EMBER, client.properties[PARENT]["not_ready"])
        client.catalog.append(
            jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        )
        second = run(client)
        self.assertEqual(second.advisories[0].notifications, ["cooldown not-ready"])
        self.assertEqual(len(mattermost_texts(client)), 1)

    def test_legacy_ticket_not_ready_stamp_is_kept(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", description="short")],
            quiet_catalog(issue),
        )
        client.properties["SNAPDENG-40"] = {"not_ready": NOW_STAMP}
        report = run(client)
        self.assertEqual(report.advisories[0].notifications, ["cooldown not-ready"])
        self.assertEqual(mattermost_texts(client), [])

    def test_jira_error_body_is_not_reported(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.create_error_body = json.dumps(
            {
                "errorMessages": ["systemd-userdbd is invalid"],
                "errors": {"description": "CVE-2024-5300: systemd-userdbd"},
            }
        ).encode()
        report = run(client)
        text = f"{report.advisories[0].error or ''} {' '.join(report.errors)}"
        self.assertNotIn("systemd-userdbd", text)
        self.assertIn("HTTP 400", report.advisories[0].error or "")

    def test_comment_pages_continue_until_total(self):
        created = (NOW - timedelta(days=8)).strftime("%Y-%m-%dT%H:%M:%S.000+0000")
        issue = jira_issue(
            "SNAPDENG-30",
            "Ember Falcon",
            "Triage",
            EMBER,
            created=created,
            assignee=True,
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                issue,
            ],
        )
        client.comments["SNAPDENG-30"] = [
            {"body": "advisory-tracker: not assigned\nnote", "created": created},
            {
                "body": "triaged the report",
                "created": created,
                "author": {"accountType": "atlassian"},
            },
        ]
        client.comment_page_size = 1
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertEqual(row.stalls, [])
        self.assertEqual(mattermost_texts(client), [])
        self.assertEqual(posts(client, "/comment"), [])
        comment_gets = [
            url
            for method, url, _body in client.calls
            if method == "GET" and "/comment?" in url
        ]
        self.assertGreaterEqual(len(comment_gets), 2)

    def test_epic_property_failure_does_not_send_not_ready(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", description="short")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.property_status = 500
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.action, "not_ready")
        self.assertIn(
            f"cannot read advisory property for {PARENT}: HTTP 500", row.error or ""
        )
        self.assertEqual(mattermost_texts(client), [MAINTENANCE_TEXT])
        self.assertNotIn(PARENT, client.properties)

        client.property_status = None
        again = run(client)
        self.assertIsNone(again.advisories[0].error)
        self.assertEqual(mattermost_texts(client), [MAINTENANCE_TEXT, MAINTENANCE_TEXT])

    def test_stall_comment_lands_on_the_least_progressed_ticket(self):
        review = jira_issue(
            "SNAPDENG-37557",
            "Ember Falcon",
            "In Review",
            EMBER,
            created=OLD_STAMP,
        )
        triage = jira_issue(
            "SNAPDENG-37802",
            "second part",
            "Triage",
            description=f"GitHub-Advisory: {EMBER}",
            created=OLD_STAMP,
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="triage")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                review,
                triage,
                parent_epic(assignee=ADA),
            ],
        )
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertIsNone(row.error)
        self.assertEqual(posts(client, "/transitions"), [])
        self.assertEqual(set(client.comments), {"SNAPDENG-37557", "SNAPDENG-37802"})
        reason = "not assigned: no assignee within 2 days of ticket creation"
        self.assertEqual(
            row.stalls,
            [
                tracker.Stall("SNAPDENG-37557", [reason]),
                tracker.Stall("SNAPDENG-37802", [reason]),
            ],
        )
        markdown = tracker.render_markdown(report)
        self.assertIn(f"- `SNAPDENG-37557` Ember Falcon: {reason}", markdown)
        self.assertIn(f"- `SNAPDENG-37802` Ember Falcon: {reason}", markdown)
        self.assertIn(
            f"SNAPDENG-37557: {reason}, SNAPDENG-37802: {reason}",
            markdown,
        )

    def test_finished_sibling_is_not_read_while_a_late_task_is_commented(self):
        done = jira_issue("SNAPDENG-37557", "Ember Falcon", "Done", EMBER)
        triage = jira_issue(
            "SNAPDENG-37802",
            "second part",
            "Triage",
            description=f"GitHub-Advisory: {EMBER}",
            created=OLD_STAMP,
        )
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="triage")],
            [
                jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET),
                done,
                triage,
                parent_epic(assignee=ADA),
            ],
        )
        report = run(client)
        row = next(item for item in report.advisories if item.ghsa_id == EMBER)
        self.assertIsNone(row.error)
        self.assertEqual(set(client.comments), {"SNAPDENG-37802"})
        self.assertFalse(
            any("/issue/SNAPDENG-37557/comment" in url for _, url, _ in client.calls)
        )
        self.assertTrue(
            any("/issue/SNAPDENG-37802/comment" in url for _, url, _ in client.calls)
        )

    def test_finished_ticket_does_not_read_comments(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Done", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            quiet_catalog(issue),
        )
        run(client)
        self.assertFalse(
            any(url.endswith("/comment") for _method, url, _body in client.calls)
        )

    def test_step_summary_omits_a_settled_apply_row(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Done", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            quiet_catalog(issue),
        )
        with tempfile.TemporaryDirectory() as directory:
            summary = os.path.join(directory, "summary.md")
            env = environment()
            env["GITHUB_STEP_SUMMARY"] = summary
            stdout = io.StringIO()
            with redirect_stdout(stdout), redirect_stderr(io.StringIO()):
                code = tracker.main(
                    ["sync", "--config", CONFIG_PATH],
                    env=env,
                    client=client,
                    now=NOW,
                )
            with open(summary, encoding="utf-8") as handle:
                step = handle.read()
        self.assertEqual(code, 0)
        self.assertIn(EMBER, stdout.getvalue())
        self.assertNotIn(EMBER, step)
        self.assertIn("Mode: apply", step)
        self.assertIn("Omitting 1 published or closed advisories with no error.", step)

    def test_untracked_summary_status_cell_is_empty(self):
        client = FakeClient(
            [sample_advisory(WILLOW, "priv esc (aka Lively Willow)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        report = run(client, command="summary")
        markdown = tracker.render_markdown(report, omit_settled=True)
        self.assertIn(
            "| Lively Willow GHSA-p962-3gwv-5ppc | triage |  |  | untracked |",
            markdown,
        )

    def test_jira_search_pages_are_followed(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            quiet_catalog(issue),
        )
        client.search_page_size = 1
        report = run(client)
        row = report.advisories[0]
        self.assertEqual(row.issue_key, "SNAPDENG-40")
        self.assertEqual(row.action, "unchanged")
        self.assertEqual(posts(client, "/rest/api/3/issue"), [])

    def test_jira_search_pagination_must_end(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.search_forever = True
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertIn(
            "cannot search Jira: pagination did not end", str(caught.exception)
        )

    def test_github_pagination_must_end(self):
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            [jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)],
        )
        client.github_always_next = True
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertIn(
            "cannot list advisories: pagination did not end", str(caught.exception)
        )

    def test_github_token_prefers_the_advisory_token(self):
        self.assertEqual(
            tracker.github_token(
                {
                    "SNAPD_ADVISORY_GITHUB_TOKEN": "advisory",
                    "GITHUB_TOKEN": "actions",
                }
            ),
            "advisory",
        )
        self.assertEqual(tracker.github_token({"GITHUB_TOKEN": "actions"}), "actions")
        with self.assertRaises(tracker.TrackerError) as caught:
            tracker.github_token({})
        self.assertIn(
            "set SNAPD_ADVISORY_GITHUB_TOKEN or GITHUB_TOKEN", str(caught.exception)
        )

    def test_petname_failure_is_reported(self):
        class Result:
            returncode = 1
            stdout = ""

        original = tracker.subprocess.run
        tracker.subprocess.run = lambda *args, **kwargs: Result()
        try:
            with self.assertRaises(tracker.TrackerError) as caught:
                tracker.petname_words()
        finally:
            tracker.subprocess.run = original
        self.assertIn("petname failed", str(caught.exception))

    def test_config_rejects_structural_mistakes(self):
        def loaded():
            with open(CONFIG_PATH, encoding="utf-8") as handle:
                return tracker.parse_yaml(handle.read())

        raw = loaded()
        raw["parent_key"] = "OTHER-1"
        with self.assertRaises(tracker.TrackerError) as parent:
            tracker.config_from_mapping(raw)
        self.assertIn("parent_key must belong to project_key", str(parent.exception))

        raw = loaded()
        raw["progress"]["stages"].append(
            {"name": "assigned", "within_days": 2, "requires": {"assignee": True}}
        )
        with self.assertRaises(tracker.TrackerError) as duplicate:
            tracker.config_from_mapping(raw)
        self.assertIn("duplicate stage assigned", str(duplicate.exception))

        raw = loaded()
        raw["progress"]["stages"][0]["requires"]["human_comments_min"] = 1
        with self.assertRaises(tracker.TrackerError) as both:
            tracker.config_from_mapping(raw)
        self.assertIn("exactly one requirement", str(both.exception))

        raw = loaded()
        raw["progress"]["stages"][0]["within_days"] = 0
        with self.assertRaises(tracker.TrackerError) as days:
            tracker.config_from_mapping(raw)
        self.assertIn("within_days must be a positive integer", str(days.exception))

        with self.assertRaises(tracker.TrackerError) as dup_key:
            tracker.parse_yaml("parent_key: SNAPDENG-1\nparent_key: SNAPDENG-2\n")
        self.assertIn("duplicate key parent_key", str(dup_key.exception))

    def test_sync_mismatch_exits_with_an_error_annotation(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Backlog", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)", state="published")],
            quiet_catalog(issue),
        )
        stdout = io.StringIO()
        stderr = io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = tracker.main(
                ["sync", "--config", CONFIG_PATH],
                env=environment(),
                client=client,
                now=NOW,
            )
        self.assertEqual(code, 1)
        self.assertIn("::error::", stderr.getvalue())
        self.assertIn("no transition", stderr.getvalue())
        self.assertEqual(
            client._issue("SNAPDENG-40")["fields"]["status"]["name"], "Backlog"
        )

    def test_main_writes_the_step_summary_and_output(self):
        issue = jira_issue("SNAPDENG-40", "Ember Falcon", "Triage", EMBER)
        client = FakeClient(
            [sample_advisory(EMBER, "pins a map (Ember Falcon)")],
            quiet_catalog(issue),
        )
        with tempfile.TemporaryDirectory() as directory:
            summary = os.path.join(directory, "summary.md")
            output = os.path.join(directory, "report.json")
            env = environment()
            env["GITHUB_STEP_SUMMARY"] = summary
            stdout = io.StringIO()
            with redirect_stdout(stdout), redirect_stderr(io.StringIO()):
                code = tracker.main(
                    [
                        "summary",
                        "--format",
                        "json",
                        "--config",
                        CONFIG_PATH,
                        "--output",
                        output,
                    ],
                    env=env,
                    client=client,
                    now=NOW,
                )
            with open(output, encoding="utf-8") as handle:
                written = handle.read()
            with open(summary, encoding="utf-8") as handle:
                summary_text = handle.read()
            self.assertEqual(code, 0)
            self.assertEqual(stdout.getvalue(), written)
            self.assertIn(EMBER, written)
            self.assertIn("# Advisory tracker", summary_text)


def names(*values):
    pending = iter(values)

    def source():
        try:
            return next(pending)
        except StopIteration as exc:
            raise AssertionError("petname was drawn too often") from exc

    return source


if __name__ == "__main__":
    unittest.main()
