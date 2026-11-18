#!/usr/bin/env python3

import json
import os
import sys
import tempfile
import unittest
from datetime import UTC, datetime
from unittest.mock import patch

import advisory_tracker as tracker

CONFIG_PATH = os.path.normpath(
    os.path.join(os.path.dirname(__file__), "..", "advisory-tracker.yaml")
)
NOW = datetime(2026, 11, 12, 15, 0, tzinfo=UTC)
NOW_STAMP = "2026-11-12T15:00:00.000+0000"
PARENT = "SNAPDENG-37541"
EMBER = "GHSA-95j9-cvc3-mmgg"
WILLOW = "GHSA-p962-3gwv-5ppc"
QUIET = "GHSA-jhwq-ffw7-65j7"
NEW_ADVISORY_HOOK = "https://hooks.mattermost.test/new-advisory"
RUN_LINK = "https://github.com/canonical/snapd/actions/runs/4242"
NOT_CREATED_TEXT = (
    "advisory-tracker: a Jira ticket was not created\n" f"Link: {RUN_LINK}"
)


class _Response:
    def __init__(self, status, text):
        self.status_code = status
        self.text = text


class JiraHTTPError(Exception):
    def __init__(self, status, body=b""):
        super().__init__(f"HTTP {status}")
        text = body.decode() if isinstance(body, bytes) else str(body)
        self.response = _Response(status, text)


def _jira_http_error(status, body=b""):
    return JiraHTTPError(status, body or b"")
_PETNAME_DIR = None


def setUpModule():
    global _PETNAME_DIR
    _PETNAME_DIR = tempfile.TemporaryDirectory()
    adjectives = ("ember", "quiet", "keen", "calm", "lively")
    names = ("falcon", "lantern", "willow", "yak", "mink")
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


def environment(**overrides):
    env = {
        "SNAPD_ADVISORY_GITHUB_TOKEN": "ghs_test",
        "JIRA_EMAIL": "bot@example.com",
        "JIRA_API_TOKEN": "jira-token",
        "JIRA_BASE_URL": "https://warthogs.atlassian.net",
        "GITHUB_REPOSITORY": "canonical/snapd",
        "GITHUB_SERVER_URL": "https://github.com",
        "GITHUB_RUN_ID": "4242",
        "MATTERMOST_NEW_ADVISORY_WEBHOOK_URL": NEW_ADVISORY_HOOK,
        "ADVISORY_TRACKER_CREATE": "true",
        "ADVISORY_TRACKER_NOTIFY": "true",
    }
    env.update(overrides)
    return env


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


def jira_issue(
    key,
    summary,
    status,
    ghsa=None,
    description="",
    created=NOW_STAMP,
    issue_type="Task",
    parent=PARENT,
    label=None,
):
    labels = []
    if label is not None:
        labels.append(label)
    elif ghsa:
        labels.append(ghsa)
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
    return {
        "key": key,
        "fields": {
            "summary": summary,
            "status": {"name": status},
            "labels": labels,
            "description": body,
            "assignee": None,
            "reporter": None,
            "created": created,
            "issuetype": {"name": issue_type},
            "parent": {"key": parent} if parent else None,
        },
    }


def anchor():
    return jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)


class FakeClient(tracker.HttpClient):
    def __init__(self, advisories, catalog):
        self.advisories = advisories
        self.extra_advisories = None
        self.catalog = catalog
        self.calls = []
        self.page = 0
        self.github_status = 200
        self.patch_status = 200
        self.mattermost_status = 200
        self.mattermost_fail_after = None
        self._mattermost_posts = 0
        self.create_status = None
        self.create_error_body = None
        self.omit_create_key = False
        self.search_page_size = None
        self.created = []
        self.updates = []
        self.next_number = 500

    def request(self, method, url, headers, body):
        self.calls.append((method, url, body))
        if "api.github.com" in url:
            return self._github(method, url, body)
        if url.startswith("https://hooks.mattermost.test/"):
            self._mattermost_posts += 1
            status = self.mattermost_status
            if (
                self.mattermost_fail_after is not None
                and self._mattermost_posts > self.mattermost_fail_after
            ):
                status = 500
            payload = b"ok" if status < 300 else b"no"
            return tracker.HttpResult(status, {}, payload)
        return tracker.HttpResult(404, {}, b'{"errorMessages":["not found"]}')

    def _github(self, method, url, body):
        if method == "PATCH":
            if self.patch_status != 200:
                return tracker.HttpResult(self.patch_status, {}, b'{"message":"no"}')
            payload = json.loads(body.decode())
            self.updates.append((url, payload))
            ghsa = url.rstrip("/").split("/")[-1]
            for advisory in self.advisories:
                if advisory.get("ghsa_id") == ghsa:
                    advisory["summary"] = payload["summary"]
            return self._json(payload)
        if self.github_status != 200:
            return tracker.HttpResult(self.github_status, {}, b'{"message":"no"}')
        if self.extra_advisories is None:
            return tracker.HttpResult(200, {}, json.dumps(self.advisories).encode())
        if self.page == 0:
            self.page = 1
            header = f'<{url}&page=2>; rel="next"'
            return tracker.HttpResult(
                200, {"link": header}, json.dumps(self.advisories).encode()
            )
        return tracker.HttpResult(200, {}, json.dumps(self.extra_advisories).encode())

    def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
        del kwargs
        payload = data or {}
        token = payload.get("nextPageToken") or ""
        return self._search(payload["jql"], token)

    def get_issue(self, issue_id_or_key, fields=None, **kwargs):
        del fields, kwargs
        for issue in self.catalog:
            if issue["key"] == issue_id_or_key:
                return issue
        raise _jira_http_error(404)

    def create_issue(self, data=None, **kwargs):
        del kwargs
        if self.create_status is not None:
            raise _jira_http_error(self.create_status, self.create_error_body)
        fields = dict((data or {}).get("fields") or {})
        self.created.append(fields)
        if self.omit_create_key:
            return {"id": "100"}
        created = self._create(fields)
        return {"id": created["id"], "key": created["key"]}

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
        key = f"SNAPDENG-{self.next_number}"
        self.next_number += 1
        issue = jira_issue(
            key,
            fields["summary"],
            "Backlog",
            description=tracker.adf_to_text(fields["description"]),
            created=NOW_STAMP,
            issue_type=fields["issuetype"]["name"],
        )
        issue["fields"]["labels"] = list(fields["labels"])
        issue["fields"]["description"] = fields["description"]
        self.catalog.append(issue)
        return {"id": "100", "key": key}

    def _json(self, payload, status=200):
        return tracker.HttpResult(status, {}, json.dumps(payload).encode())


def run(
    client,
    advisories=None,
    command="sync",
    dry_run=False,
    name_source=None,
    env=None,
):
    if advisories is not None:
        client.advisories = advisories
    return tracker.execute(
        tracker.load_config(CONFIG_PATH),
        environment() if env is None else env,
        client,
        NOW,
        command,
        dry_run,
        name_source,
        jira=client,
    )


def mattermost_texts(client):
    return [
        json.loads(body.decode())["text"]
        for method, url, body in client.calls
        if method == "POST" and url == NEW_ADVISORY_HOOK
    ]


def created_fields(client):
    return client.created


class AdvisoryTrackerTests(unittest.TestCase):
    def test_shipped_config(self):
        config = tracker.load_config(CONFIG_PATH)
        self.assertEqual(config.parent_key, PARENT)
        self.assertEqual(config.project_key, "SNAPDENG")
        self.assertEqual(config.issue_type, "Task")
        self.assertEqual(config.create_for_states, ("triage", "draft"))
        self.assertEqual(config.priority_map["critical"], "Highest")
        self.assertEqual(config.min_description_chars, 80)
        self.assertEqual(config.notify["created"], ("mattermost",))
        self.assertEqual(config.notify["not_created"], ("mattermost",))
        self.assertNotIn("state_map", config.__dict__)

    def test_missing_jira_client_names_the_package(self):
        with patch.dict(sys.modules, {"atlassian.jira": None}):
            with self.assertRaises(tracker.TrackerError) as caught:
                tracker.connect_jira("https://jira.example", "a@b.c", "token")
        self.assertIn("atlassian-python-api", str(caught.exception))

    def test_unknown_channel_is_rejected(self):
        with open(CONFIG_PATH, encoding="utf-8") as handle:
            raw = tracker.parse_yaml(handle.read())
        raw["notify"]["created"] = ["jira_comment"]
        with self.assertRaises(tracker.TrackerError) as caught:
            tracker.config_from_mapping(raw)
        self.assertIn("unknown channel jira_comment", str(caught.exception))

    def test_codename_shapes(self):
        self.assertEqual(
            tracker.extract_codename("pins a map (Ember Falcon)"), "Ember Falcon"
        )
        self.assertEqual(
            tracker.extract_codename("pins a map (aka Ember Falcon)"), "Ember Falcon"
        )
        self.assertEqual(tracker.petname_from_summary("Ember Falcon"), "Ember Falcon")
        self.assertEqual(
            tracker.petname_from_summary("Ember Falcon: pins a map"), "Ember Falcon"
        )
        self.assertIsNone(tracker.petname_from_summary("pins a map"))

    def test_readiness_flags_missing_information(self):
        config = tracker.load_config(CONFIG_PATH)
        reasons = tracker.readiness_reasons(
            {"summary": "  ", "description": "short"}, config
        )
        self.assertIn("missing summary", reasons)
        self.assertIn("description is shorter than 80 characters", reasons)

    def test_compound_label_and_split_paragraph_match(self):
        label = f"see-{EMBER}-now"
        issue = tracker.issue_from_json(
            jira_issue("SNAPDENG-77", "notes", "Triage", label=label)
        )
        self.assertTrue(tracker.issue_references(issue, EMBER))
        text = tracker.adf_to_text(
            {
                "type": "paragraph",
                "content": [
                    {"type": "text", "text": "GHSA-95j9-"},
                    {"type": "text", "text": "cvc3-mmgg"},
                ],
            }
        )
        self.assertEqual(text, EMBER)

    def test_create_uses_codename_and_does_not_set_status(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)", cve="CVE-2026-1")
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        fields = created_fields(client)
        self.assertEqual(len(fields), 1)
        self.assertEqual(fields[0]["summary"], "Ember Falcon")
        self.assertEqual(fields[0]["parent"], {"key": PARENT})
        self.assertEqual(fields[0]["issuetype"], {"name": "Task"})
        self.assertEqual(fields[0]["labels"], [EMBER])
        self.assertNotIn("status", fields[0])
        description = tracker.adf_to_text(fields[0]["description"])
        self.assertIn(f"GitHub-Advisory: {EMBER}", description)
        self.assertIn("CVE: CVE-2026-1", description)
        self.assertEqual(report.advisories[0].action, "created")
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-500")

    def test_reports_hide_the_vulnerability_title(self):
        title = "pins a private map (Ember Falcon)"
        advisory = sample_advisory(EMBER, title, cve="CVE-2026-9")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)],
        )
        report = run(client, command="summary")
        text = tracker.render_json(report)
        self.assertNotIn(title, text)
        self.assertNotIn("CVE-2026-9", text)
        self.assertNotIn("private map", tracker.render_markdown(report))
        self.assertIn("Ember Falcon", text)

    def test_titles_are_not_published_as_codenames(self):
        advisory = sample_advisory(EMBER, "Fix The Pin")
        client = FakeClient([advisory], [anchor()])
        report = run(client, name_source=lambda: "keen yak")
        self.assertEqual(created_fields(client)[0]["summary"], "Keen Yak")
        self.assertEqual(report.advisories[0].codename, "Keen Yak")
        self.assertNotIn("Fix The Pin", created_fields(client)[0]["summary"])

    def test_critical_advisory_sets_the_mapped_priority(self):
        advisory = sample_advisory(
            EMBER, "pins a map (Ember Falcon)", severity="critical"
        )
        client = FakeClient([advisory], [anchor()])
        run(client)
        self.assertEqual(created_fields(client)[0]["priority"], {"name": "Highest"})

    def test_missing_severity_is_created_without_priority(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)", severity=None)
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        self.assertNotIn("priority", created_fields(client)[0])
        self.assertEqual(report.advisories[0].action, "created")

    def test_existing_reference_is_not_created_again(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)],
        )
        report = run(client)
        self.assertEqual(created_fields(client), [])
        self.assertEqual(report.advisories[0].action, "tracked")
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-20")
        self.assertEqual(mattermost_texts(client), [])

    def test_existing_petname_is_copied_onto_the_advisory(self):
        advisory = sample_advisory(EMBER, "pins a map")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)],
        )
        report = run(client)
        self.assertEqual(report.advisories[0].codename, "Ember Falcon")
        self.assertEqual(advisory["summary"], "pins a map (Ember Falcon)")
        self.assertEqual(created_fields(client), [])

    def test_existing_ticket_without_a_codename_is_tracked(self):
        advisory = sample_advisory(EMBER, "pins a map")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "fix the pin", "Triage", EMBER)],
        )
        report = run(client)
        self.assertEqual(report.advisories[0].action, "tracked")
        self.assertIsNone(report.advisories[0].codename)
        self.assertEqual(client.updates, [])

    def test_multiple_tickets_are_tracked_without_creating_another(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient(
            [advisory],
            [
                anchor(),
                jira_issue("SNAPDENG-22", "other", "Triage", EMBER),
                jira_issue("SNAPDENG-21", "Ember Falcon", "In Progress", EMBER),
            ],
        )
        report = run(client)
        self.assertEqual(created_fields(client), [])
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-21")
        self.assertEqual(
            report.advisories[0].issue_keys, ["SNAPDENG-21", "SNAPDENG-22"]
        )

    def test_outside_ticket_blocks_creation(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        outside = jira_issue(
            "SNAPDENG-77", "Ember Falcon", "Triage", EMBER, parent="SNAPDENG-1"
        )
        client = FakeClient([advisory], [outside])
        report = run(client)
        self.assertEqual(created_fields(client), [])
        self.assertEqual(report.advisories[0].action, "error")
        self.assertIn("not a child", report.advisories[0].error)
        self.assertIn(EMBER, report.advisories[0].error)
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])
        markdown = tracker.render_markdown(report)
        self.assertIn(EMBER, markdown)
        self.assertIn("not a child", markdown)
        self.assertNotIn("pins a map", markdown)

    def test_outside_ticket_does_not_block_an_epic_child(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        outside = jira_issue(
            "SNAPDENG-77", "notes", "Triage", EMBER, parent="SNAPDENG-1"
        )
        child = jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)
        client = FakeClient([advisory], [anchor(), outside, child])
        report = run(client)
        self.assertEqual(created_fields(client), [])
        self.assertEqual(report.advisories[0].action, "tracked")
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-20")
        self.assertEqual(mattermost_texts(client), [])

    def test_not_ready_is_flagged_without_a_ticket(self):
        advisory = sample_advisory(
            EMBER, "pins a map (Ember Falcon)", description="short"
        )
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        self.assertEqual(created_fields(client), [])
        self.assertEqual(report.advisories[0].action, "not_ready")
        self.assertIn("shorter than 80", report.advisories[0].reasons[0])
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])
        markdown = tracker.render_markdown(report)
        self.assertIn(EMBER, markdown)
        self.assertIn("shorter than 80", markdown)
        self.assertNotIn("pins a map", NOT_CREATED_TEXT)

    def test_two_not_ready_advisories_send_one_notice(self):
        first = sample_advisory(EMBER, "pins a map", description="short")
        second = sample_advisory(WILLOW, "other map", description="tiny")
        client = FakeClient([first, second], [anchor()])
        report = run(client)
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])
        self.assertIn("sent mattermost not_created", report.advisories[0].notifications)
        self.assertNotIn(
            "sent mattermost not_created", report.advisories[1].notifications
        )

    def test_failed_create_posts_one_summary_link(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.create_status = 400
        client.create_error_body = b'{"errorMessages":["secret-token-body"]}'
        report = run(client)
        self.assertEqual(report.advisories[0].action, "error")
        self.assertIn("HTTP 400", report.advisories[0].error)
        self.assertNotIn("secret-token-body", report.advisories[0].error)
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])
        self.assertIn(RUN_LINK, NOT_CREATED_TEXT)
        self.assertNotIn(EMBER, NOT_CREATED_TEXT)
        markdown = tracker.render_markdown(report)
        self.assertIn(EMBER, markdown)
        self.assertIn("HTTP 400", markdown)

    def test_not_created_post_failure_still_returns_the_report(self):
        advisory = sample_advisory(EMBER, "pins a map", description="short")
        client = FakeClient([advisory], [anchor()])
        client.mattermost_status = 403
        report = run(client)
        self.assertIn("HTTP 403", report.errors[-1])
        self.assertIn(EMBER, tracker.render_markdown(report))

    def test_jira_create_auth_failure_aborts_before_the_notice(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.create_status = 401
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertEqual(caught.exception.status, 401)
        self.assertEqual(mattermost_texts(client), [])

    def test_creation_notification_auth_failure_aborts(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.mattermost_status = 401
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertEqual(caught.exception.status, 401)
        self.assertEqual(client.catalog[-1]["key"], "SNAPDENG-500")

    def test_github_summary_forbidden_aborts_the_run(self):
        advisory = sample_advisory(EMBER, "pins a map")
        client = FakeClient([advisory], [anchor()])
        client.patch_status = 403
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client, name_source=lambda: "keen yak")
        self.assertEqual(caught.exception.status, 403)
        self.assertIn("SNAPD_ADVISORY_GITHUB_TOKEN", str(caught.exception))

    def test_published_advisory_without_a_ticket_is_not_backfilled(self):
        advisory = sample_advisory(
            EMBER, "pins a map (Ember Falcon)", state="published"
        )
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        self.assertEqual(report.advisories[0].action, "skipped")
        self.assertEqual(created_fields(client), [])
        self.assertEqual(mattermost_texts(client), [])

    def test_short_published_advisory_without_a_ticket_is_skipped(self):
        advisory = sample_advisory(
            EMBER, "pins a map", state="closed", description="short"
        )
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        self.assertEqual(report.advisories[0].action, "skipped")
        self.assertEqual(mattermost_texts(client), [])

    def test_create_permission_off_does_not_create_or_notify(self):
        advisory = sample_advisory(EMBER, "pins a map", description="short")
        ready = sample_advisory(WILLOW, "other map (Quiet Willow)")
        client = FakeClient([advisory, ready], [anchor()])
        report = run(client, env=environment(ADVISORY_TRACKER_CREATE="false"))
        self.assertEqual(created_fields(client), [])
        actions = {row.ghsa_id: row.action for row in report.advisories}
        self.assertEqual(actions[EMBER], "not_ready")
        self.assertEqual(actions[WILLOW], "create_disabled")
        self.assertEqual(mattermost_texts(client), [])
        self.assertNotIn(
            "not_created", " ".join(report.advisories[0].notifications)
        )

    def test_notify_permission_off_still_creates(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        report = run(client, env=environment(ADVISORY_TRACKER_NOTIFY="false"))
        self.assertEqual(report.advisories[0].action, "created")
        self.assertIn("notify disabled", report.advisories[0].notifications)
        self.assertEqual(mattermost_texts(client), [])

    def test_missing_webhook_still_creates_the_ticket(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        env = environment(MATTERMOST_NEW_ADVISORY_WEBHOOK_URL="")
        report = run(client, env=env)
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-500")
        self.assertIn("MATTERMOST_NEW_ADVISORY_WEBHOOK_URL", report.advisories[0].error)
        self.assertEqual(mattermost_texts(client), [])

    def test_missing_run_id_records_the_notice_error(self):
        advisory = sample_advisory(EMBER, "pins a map", description="short")
        client = FakeClient([advisory], [anchor()])
        env = environment(GITHUB_RUN_ID="")
        report = run(client, env=env)
        self.assertTrue(any("GITHUB_RUN_ID" in error for error in report.errors))
        self.assertEqual(mattermost_texts(client), [])
        self.assertIn(EMBER, tracker.render_markdown(report))

    def test_dry_run_records_the_notice_without_posting(self):
        advisory = sample_advisory(EMBER, "pins a map", description="short")
        client = FakeClient([advisory], [anchor()])
        report = run(client, dry_run=True)
        self.assertEqual(created_fields(client), [])
        self.assertEqual(mattermost_texts(client), [])
        self.assertIn(
            "would mattermost not_created", report.advisories[0].notifications
        )

    def test_dry_run_does_not_create_or_generate_a_name(self):
        advisory = sample_advisory(EMBER, "pins a map")

        def fail():
            raise AssertionError("petname was called")

        client = FakeClient([advisory], [anchor()])
        report = run(client, dry_run=True, name_source=fail)
        self.assertEqual(report.advisories[0].action, "would_create")
        self.assertIn("would mattermost created", report.advisories[0].notifications)
        self.assertEqual(created_fields(client), [])

    def test_created_ticket_is_announced_once(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        other = sample_advisory(WILLOW, "other", description="short")
        client = FakeClient([advisory, other], [anchor()])
        run(client)
        run(client)
        texts = mattermost_texts(client)
        created = [text for text in texts if "new advisory detected" in text]
        self.assertEqual(len(created), 1)
        self.assertIn("SNAPDENG-500 (Ember Falcon): Jira ticket created", created[0])
        self.assertIn(f"GitHub-Advisory: {EMBER}", created[0])
        self.assertEqual(texts.count(NOT_CREATED_TEXT), 2)
        self.assertEqual(len(created_fields(client)), 1)

    def test_confirmation_failure_refuses_creation(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        bug = jira_issue(
            "SNAPDENG-10", "Quiet Lantern", "Triage", QUIET, issue_type="Bug"
        )
        client = FakeClient([advisory], [bug])
        report = run(client)
        self.assertIn("does not match child issue type Bug", report.confirmation_error)
        self.assertEqual(report.advisories[0].action, "refused")
        self.assertEqual(created_fields(client), [])
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])
        self.assertIn("Bug", tracker.render_markdown(report))

    def test_a_matching_child_confirms_a_mixed_set(self):
        config = tracker.load_config(CONFIG_PATH)
        task = tracker.issue_from_json(anchor())
        bug = tracker.issue_from_json(
            jira_issue("SNAPDENG-11", "Quiet Lantern", "Triage", issue_type="Bug")
        )
        self.assertIsNone(tracker.confirm_children([bug, task], config))
        self.assertIn("no child", tracker.confirm_children([], config))

    def test_github_forbidden_names_the_advisory_token(self):
        client = FakeClient([], [anchor()])
        client.github_status = 403
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertIn("SNAPD_ADVISORY_GITHUB_TOKEN", str(caught.exception))
        self.assertEqual(caught.exception.status, 403)

    def test_advisory_pages_are_followed(self):
        first = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        second = sample_advisory(WILLOW, "other map (Quiet Willow)", state="published")
        client = FakeClient([first], [anchor()])
        client.extra_advisories = [second]
        report = run(client)
        self.assertEqual(
            [row.ghsa_id for row in report.advisories], [EMBER, WILLOW]
        )
        self.assertEqual(report.advisories[1].action, "skipped")

    def test_summary_is_read_only(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        report = run(client, command="summary")
        self.assertEqual(report.mode, "summary")
        self.assertEqual(report.advisories[0].action, "untracked")
        self.assertEqual(created_fields(client), [])
        self.assertEqual(mattermost_texts(client), [])

    def test_summary_omits_settled_advisories_without_an_error(self):
        published = sample_advisory(EMBER, "done (Ember Falcon)", state="published")
        triage = sample_advisory(WILLOW, "open (Quiet Willow)")
        client = FakeClient([published, triage], [anchor()])
        report = run(client, command="summary")
        text = tracker.render_json(report)
        self.assertNotIn(EMBER, text)
        self.assertIn(WILLOW, text)
        self.assertIn("omitted_advisories", text)

    def test_choose_codename_skips_existing_names(self):
        used = {"keen yak"}
        draws = iter(["keen yak", "keen-yak", "calm mink"])
        chosen = tracker.choose_codename(lambda: next(draws), used)
        self.assertEqual(chosen, "Calm Mink")

    def test_generated_name_is_unique_and_patched_after_create(self):
        advisory = sample_advisory(EMBER, "pins a map")
        client = FakeClient([advisory], [anchor()])
        report = run(client, name_source=lambda: "lively willow")
        self.assertEqual(created_fields(client)[0]["summary"], "Lively Willow")
        self.assertEqual(advisory["summary"], "pins a map (Lively Willow)")
        self.assertEqual(report.advisories[0].codename, "Lively Willow")
        text = mattermost_texts(client)[0]
        self.assertIn("SNAPDENG-500 (Lively Willow): Jira ticket created", text)

    def test_failed_advisory_rename_is_retried_without_a_second_ticket(self):
        advisory = sample_advisory(EMBER, "pins a map")
        client = FakeClient([advisory], [anchor()])
        client.patch_status = 500
        report = run(client, name_source=lambda: "keen yak")
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-500")
        self.assertTrue(report.advisories[0].error)
        client.patch_status = 200
        client.advisories = [advisory]
        again = run(client, name_source=lambda: "calm mink")
        self.assertEqual(len(created_fields(client)), 1)
        self.assertEqual(again.advisories[0].action, "tracked")
        self.assertEqual(advisory["summary"], "pins a map (Keen Yak)")

    def test_missing_ghsa_id_creates_nothing(self):
        advisory = sample_advisory("not-a-ghsa", "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        self.assertEqual(created_fields(client), [])
        self.assertEqual(report.advisories[0].action, "error")
        self.assertIn("missing a GHSA id", report.advisories[0].error)

    def test_search_pages_still_find_the_existing_ticket(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)],
        )
        client.search_page_size = 1
        report = run(client)
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-20")
        self.assertEqual(created_fields(client), [])

    def test_notify_off_records_a_blocked_advisory_without_posting(self):
        advisory = sample_advisory(EMBER, "pins a map", description="short")
        client = FakeClient([advisory], [anchor()])
        report = run(client, env=environment(ADVISORY_TRACKER_NOTIFY="false"))
        self.assertEqual(report.advisories[0].action, "not_ready")
        self.assertIn(
            "notify disabled not_created", report.advisories[0].notifications
        )
        self.assertEqual(mattermost_texts(client), [])

    def test_resolve_mode_uses_create_and_notify_only(self):
        self.assertEqual(
            tracker.resolve_mode(
                {"GITHUB_EVENT_NAME": "schedule", "ADVISORY_TRACKER_CREATE": "true"}
            ),
            "apply",
        )
        self.assertEqual(
            tracker.resolve_mode({"GITHUB_EVENT_NAME": "schedule"}),
            "skip",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "workflow_dispatch",
                    "INPUT_ACTION": "sync",
                    "INPUT_DRY_RUN": "true",
                    "INPUT_APPLY": "true",
                }
            ),
            "dry-run",
        )

    def test_create_without_a_key_is_an_error_and_notifies(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.omit_create_key = True
        report = run(client)
        self.assertEqual(report.advisories[0].action, "error")
        self.assertIn("did not include a key", report.advisories[0].error)
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])

    def test_later_run_does_not_repeat_the_creation_message(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        run(client)
        self.assertEqual(len(mattermost_texts(client)), 1)
        run(client)
        self.assertEqual(len(mattermost_texts(client)), 1)
        self.assertEqual(client.catalog[-1]["fields"]["summary"], "Ember Falcon")
