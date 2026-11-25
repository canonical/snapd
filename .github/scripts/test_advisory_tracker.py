#!/usr/bin/env python3

import contextlib
import io
import json
import os
import sys
import tempfile
import unittest
from datetime import UTC, datetime
from unittest.mock import patch

import advisory_tracker as tracker

CONFIG_PATH = os.path.normpath(
    os.path.join(os.path.dirname(__file__), "..", "advisory-tracker_config.yaml")
)
NOW = datetime(2026, 11, 12, 15, 0, tzinfo=UTC)
NOW_STAMP = "2026-11-12T15:00:00.000+0000"
PARENT = "SNAPDENG-37541"
EMBER = "GHSA-95j9-cvc3-mmgg"
WILLOW = "GHSA-p962-3gwv-5ppc"
QUIET = "GHSA-jhwq-ffw7-65j7"
BUG_LINK_FIELD = "customfield_10050"
NEW_ADVISORY_HOOK = "https://hooks.mattermost.test/new-advisory"
RUN_LINK = "https://github.com/canonical/snapd/actions/runs/4242"
NOT_CREATED_TEXT = "\n".join(
    [
        "## :exclamation: advisory-tracker: a Jira ticket was not created",
        "",
        f"Link: `{RUN_LINK}`",
    ]
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


class GitHubHTTPError(Exception):
    def __init__(self, status):
        super().__init__(f"HTTP {status}")
        self.status = status
        self.data = {"message": "secret-github-body"}


class _FakeAdvisory:
    def __init__(self, client, data):
        self._client = client
        self.raw_data = data

    def edit(self, summary=None, **kwargs):
        del kwargs
        if self._client.patch_status != 200:
            raise GitHubHTTPError(self._client.patch_status)
        ghsa = self.raw_data.get("ghsa_id")
        self._client.updates.append((ghsa, {"summary": summary}))
        self.raw_data["summary"] = summary


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
    issue_type="Bug",
    parent=PARENT,
    label=None,
    bug_link=None,
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
    issue = {
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
    if bug_link:
        issue["fields"][BUG_LINK_FIELD] = bug_link
    return issue


def anchor():
    return jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", QUIET)


class FakeClient(tracker.HttpClient):
    def __init__(self, advisories, catalog):
        self.advisories = advisories
        self.extra_advisories = None
        self.catalog = catalog
        self.parent_issue = None
        self.calls = []
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
        self.edits = []
        self.edit_status = None
        self.jira_fields = [{"id": BUG_LINK_FIELD, "name": "Bug Link"}]
        self.next_number = 500

    def request(self, method, url, headers, body):
        self.calls.append((method, url, body))
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

    def get_repo(self, name, **kwargs):
        del name, kwargs
        if self.github_status != 200:
            raise GitHubHTTPError(self.github_status)
        return self

    def get_repository_advisories(self, **kwargs):
        del kwargs
        if self.github_status != 200:
            raise GitHubHTTPError(self.github_status)
        pages = [self.advisories]
        if self.extra_advisories is not None:
            pages.append(self.extra_advisories)
        for page in pages:
            for item in page:
                yield _FakeAdvisory(self, item)

    def get_repository_advisory(self, ghsa_id, **kwargs):
        del kwargs
        if self.github_status != 200:
            raise GitHubHTTPError(self.github_status)
        pages = [self.advisories]
        if self.extra_advisories is not None:
            pages.append(self.extra_advisories)
        for page in pages:
            for item in page:
                if item.get("ghsa_id") == ghsa_id:
                    return _FakeAdvisory(self, item)
        raise GitHubHTTPError(404)

    def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
        del kwargs
        payload = data or {}
        token = payload.get("nextPageToken") or ""
        return self._search(payload["jql"], token)

    def get_issue(self, issue_id_or_key, fields=None, **kwargs):
        del fields, kwargs
        if self.parent_issue and self.parent_issue["key"] == issue_id_or_key:
            return self.parent_issue
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
        link = fields.get(BUG_LINK_FIELD)
        if isinstance(link, str) and link != "":
            issue["fields"][BUG_LINK_FIELD] = link
        self.catalog.append(issue)
        return {"id": "100", "key": key}

    def get_fields(self, **kwargs):
        del kwargs
        return self.jira_fields

    def edit_issue(self, issue_id_or_key, data=None, **kwargs):
        del kwargs
        if self.edit_status is not None:
            raise _jira_http_error(self.edit_status)
        fields = dict((data or {}).get("fields") or {})
        self.edits.append((issue_id_or_key, fields))
        if self.parent_issue and self.parent_issue["key"] == issue_id_or_key:
            self.parent_issue["fields"].update(fields)
        for issue in self.catalog:
            if issue["key"] == issue_id_or_key:
                issue["fields"].update(fields)
        return None


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
        github=client,
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
        self.assertEqual(config.issue_type, "Bug")
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

    def test_missing_github_client_names_the_package(self):
        with patch.dict(sys.modules, {"github": None}):
            with self.assertRaises(tracker.TrackerError) as caught:
                tracker.connect_github("token")
        self.assertIn("PyGithub", str(caught.exception))

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
        self.assertEqual(
            tracker.petname_from_summary(f"Ember Falcon: {EMBER}"), "Ember Falcon"
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
        self.assertEqual(fields[0]["summary"], f"Ember Falcon: {EMBER}")
        self.assertEqual(fields[0]["parent"], {"key": PARENT})
        self.assertEqual(fields[0]["issuetype"], {"name": "Bug"})
        self.assertEqual(fields[0]["priority"], {"name": "Medium"})
        self.assertEqual(fields[0]["labels"], [EMBER])
        self.assertEqual(
            fields[0][BUG_LINK_FIELD],
            f"https://github.com/canonical/snapd/security/advisories/{EMBER}",
        )
        self.assertEqual(client.edits, [])
        self.assertNotIn("status", fields[0])
        description = tracker.adf_to_text(fields[0]["description"])
        self.assertEqual(description, f"GitHub-Advisory: {EMBER}")
        self.assertNotIn("CVE-2026-1", description)
        self.assertNotIn("pins a map", description)
        self.assertEqual(report.advisories[0].action, "created")
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-500")
        markdown = tracker.render_markdown(report)
        self.assertIn(
            f"## Created\n\n- `{EMBER}`: SNAPDENG-500 (Ember Falcon)\n",
            markdown,
        )
        self.assertIn(f"Ember Falcon {EMBER}", markdown)
        self.assertNotIn(f"{EMBER} {EMBER}", markdown)

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
        self.assertEqual(created_fields(client)[0]["summary"], f"Keen Yak: {EMBER}")
        self.assertEqual(report.advisories[0].codename, "Keen Yak")
        self.assertNotIn("Fix The Pin", created_fields(client)[0]["summary"])

    def test_private_title_suffix_is_not_a_codename(self):
        advisory = sample_advisory(EMBER, "Issue (Local Privilege Escalation)")
        client = FakeClient([advisory], [anchor()])
        report = run(client, name_source=lambda: "keen yak")
        self.assertEqual(created_fields(client)[0]["summary"], f"Keen Yak: {EMBER}")
        self.assertNotIn("Privilege", created_fields(client)[0]["summary"])
        self.assertEqual(report.advisories[0].codename, "Keen Yak")
        markdown = tracker.render_markdown(report)
        self.assertIn(
            f"## Created\n\n- `{EMBER}`: SNAPDENG-500 (Keen Yak)\n", markdown
        )
        self.assertNotIn("Privilege", markdown)

    def test_critical_advisory_sets_the_mapped_priority(self):
        advisory = sample_advisory(
            EMBER, "pins a map (Ember Falcon)", severity="critical"
        )
        client = FakeClient([advisory], [anchor()])
        run(client)
        self.assertEqual(created_fields(client)[0]["priority"], {"name": "Highest"})

    def test_missing_severity_defaults_to_medium(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)", severity=None)
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        self.assertEqual(created_fields(client)[0]["priority"], {"name": "Medium"})
        self.assertEqual(report.advisories[0].action, "created")

    def test_unknown_severity_defaults_to_medium(self):
        advisory = sample_advisory(
            EMBER, "pins a map (Ember Falcon)", severity="urgent"
        )
        client = FakeClient([advisory], [anchor()])
        run(client)
        self.assertEqual(created_fields(client)[0]["priority"], {"name": "Medium"})

    def test_petname_alone_does_not_claim_another_ticket(self):
        advisory = sample_advisory(EMBER, "pins a map (Quiet Lantern)")
        client = FakeClient([advisory], [anchor()])
        report = run(client)
        self.assertEqual(report.advisories[0].action, "created")
        self.assertEqual(report.advisories[0].issue_key, "SNAPDENG-500")
        self.assertNotIn("SNAPDENG-10", report.advisories[0].issue_keys)
        self.assertEqual(
            created_fields(client)[0]["summary"], f"Quiet Lantern: {EMBER}"
        )
        self.assertEqual(client.edits, [])

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

    def test_empty_bug_link_is_filled(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)],
        )
        run(client)
        self.assertEqual(
            client.edits,
            [
                (
                    "SNAPDENG-20",
                    {
                        BUG_LINK_FIELD: (
                            "https://github.com/canonical/snapd"
                            f"/security/advisories/{EMBER}"
                        )
                    },
                )
            ],
        )

    def test_existing_bug_link_is_left_alone(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        existing = jira_issue(
            "SNAPDENG-20",
            "Ember Falcon",
            "Triage",
            EMBER,
            bug_link="https://example.test/kept",
        )
        client = FakeClient([advisory], [anchor(), existing])
        run(client)
        self.assertEqual(client.edits, [])
        self.assertEqual(
            existing["fields"][BUG_LINK_FIELD], "https://example.test/kept"
        )

    def test_dry_run_records_an_empty_bug_link(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)],
        )
        report = run(client, dry_run=True)
        self.assertEqual(client.edits, [])
        self.assertIn("would set bug link", report.advisories[0].notifications)

    def test_bug_link_field_override_is_used(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.jira_fields = []
        run(client, env=environment(JIRA_BUG_LINK_FIELD="customfield_9"))
        self.assertEqual(
            created_fields(client)[0]["customfield_9"], advisory["html_url"]
        )

    def test_missing_bug_link_field_names_the_override(self):
        client = FakeClient([], [anchor()])
        client.jira_fields = []
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertIn("JIRA_BUG_LINK_FIELD", str(caught.exception))

    def test_bug_link_auth_failure_aborts(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient(
            [advisory],
            [anchor(), jira_issue("SNAPDENG-20", "Ember Falcon", "Triage", EMBER)],
        )
        client.edit_status = 403
        with self.assertRaises(tracker.TrackerError) as caught:
            run(client)
        self.assertEqual(caught.exception.status, 403)

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
        markdown = tracker.render_markdown(report)
        self.assertIn("## Created\n\nNone.\n\n## Not created\n", markdown)
        self.assertIn(f"| {EMBER} |", markdown)
        self.assertNotIn(f"{EMBER} {EMBER}", markdown)

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
        self.assertTrue(any("HTTP 403" in error for error in report.errors))
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
        self.assertIn(
            "## :bug: advisory-tracker: new advisory detected",
            created[0],
        )
        self.assertIn("Jira ticket created: SNAPDENG-500", created[0])
        self.assertIn(
            "Link: `https://github.com/canonical/snapd"
            f"/security/advisories/{EMBER}`",
            created[0],
        )
        self.assertIn(f"GitHub-Advisory: {EMBER}", created[0])
        self.assertEqual(texts.count(NOT_CREATED_TEXT), 2)
        self.assertEqual(len(created_fields(client)), 1)

    def test_confirmation_failure_refuses_creation(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        task = jira_issue(
            "SNAPDENG-10", "Quiet Lantern", "Triage", QUIET, issue_type="Task"
        )
        client = FakeClient([advisory], [task])
        report = run(client)
        self.assertIn(
            "does not match child issue type Task", report.confirmation_error
        )
        self.assertEqual(report.advisories[0].action, "refused")
        self.assertEqual(created_fields(client), [])
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])
        self.assertIn("Task", tracker.render_markdown(report))

    def test_a_matching_child_confirms_a_mixed_set(self):
        config = tracker.load_config(CONFIG_PATH)
        task = tracker.issue_from_json(
            jira_issue("SNAPDENG-10", "Quiet Lantern", "Triage", issue_type="Task")
        )
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

    def test_summary_appends_then_replaces_the_tracking_table(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.parent_issue = jira_issue(
            PARENT,
            "snapd security advisories",
            "To Do",
            issue_type="Epic",
            parent=None,
        )
        client.parent_issue["fields"]["description"] = {
            "type": "doc",
            "version": 1,
            "content": [
                {
                    "type": "paragraph",
                    "content": [
                        {
                            "type": "text",
                            "text": "This epic groups security advisories for snapd.",
                        }
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {
                            "type": "text",
                            "text": "Each child card tracks one advisory.",
                        }
                    ],
                },
            ],
        }
        run(client, command="summary")
        description = client.parent_issue["fields"]["description"]
        text = tracker.adf_to_text(description)
        self.assertIn("This epic groups security advisories for snapd.", text)
        self.assertIn("Each child card tracks one advisory.", text)
        self.assertIn("Last updated 2026-11-12 15:00 UTC.", text)
        self.assertIn(EMBER, text)
        link = description["content"][3]["content"][1]["marks"][0]["attrs"]["href"]
        self.assertEqual(link, RUN_LINK)
        self.assertEqual(description["content"][4]["type"], "table")
        run(client, command="summary")
        again = client.parent_issue["fields"]["description"]["content"]
        self.assertEqual(
            tracker.adf_to_text(again[0]),
            "This epic groups security advisories for snapd.",
        )
        self.assertEqual(
            tracker.adf_to_text(again[1]),
            "Each child card tracks one advisory.",
        )
        headings = [node for node in again if node.get("type") == "heading"]
        self.assertEqual(len(headings), 1)
        self.assertEqual(len([node for node in again if node.get("type") == "table"]), 1)

    def test_dry_run_leaves_the_parent_description(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.parent_issue = jira_issue(
            PARENT, "snapd security advisories", "To Do", issue_type="Epic", parent=None
        )
        original = client.parent_issue["fields"]["description"]
        run(client, dry_run=True)
        self.assertEqual(client.parent_issue["fields"]["description"], original)
        self.assertFalse(
            any("description" in fields for _key, fields in client.edits)
        )

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
        self.assertEqual(
            created_fields(client)[0]["summary"], f"Lively Willow: {EMBER}"
        )
        self.assertEqual(advisory["summary"], "pins a map (Lively Willow)")
        self.assertEqual(report.advisories[0].codename, "Lively Willow")
        text = mattermost_texts(client)[0]
        self.assertIn("Jira ticket created: SNAPDENG-500", text)

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
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "workflow_dispatch",
                    "INPUT_ACTION": "summary",
                    "INPUT_DRY_RUN": "true",
                }
            ),
            "summary-dry-run",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "repository_dispatch",
                    "PAYLOAD_ACTION": "summary",
                    "PAYLOAD_DRY_RUN": "true",
                }
            ),
            "summary-dry-run",
        )
        self.assertEqual(
            tracker.resolve_mode(
                {
                    "GITHUB_EVENT_NAME": "workflow_dispatch",
                    "INPUT_ACTION": "summary",
                }
            ),
            "summary",
        )

    def test_summary_dry_run_leaves_the_parent_description(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.parent_issue = jira_issue(
            PARENT, "snapd security advisories", "To Do", issue_type="Epic", parent=None
        )
        original = client.parent_issue["fields"]["description"]
        run(client, command="summary", dry_run=True)
        self.assertEqual(client.parent_issue["fields"]["description"], original)
        self.assertFalse(
            any("description" in fields for _key, fields in client.edits)
        )

    def test_summary_parent_404_fails_the_run(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        report = run(client, command="summary")
        self.assertTrue(any("HTTP 404" in error for error in report.errors))
        stdout = io.StringIO()
        stderr = io.StringIO()
        with (
            patch.object(tracker, "connect_github", return_value=client),
            patch.object(tracker, "connect_jira", return_value=client),
            contextlib.redirect_stdout(stdout),
            contextlib.redirect_stderr(stderr),
        ):
            code = tracker.main(
                ["summary", "--config", CONFIG_PATH],
                env=environment(),
                client=client,
                now=NOW,
            )
        self.assertEqual(code, 1)
        self.assertIn("HTTP 404", stderr.getvalue())

    def test_create_without_a_key_is_an_error_and_notifies(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        client.omit_create_key = True
        report = run(client)
        self.assertEqual(report.advisories[0].action, "error")
        self.assertIn("did not include a key", report.advisories[0].error)
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])

    def test_petname_failure_is_not_created(self):
        advisory = sample_advisory(EMBER, "pins a map")

        def fail():
            raise tracker.TrackerError("cannot generate codename: petname failed")

        client = FakeClient([advisory], [anchor()])
        report = run(client, name_source=fail)
        row = report.advisories[0]
        self.assertEqual(row.action, "error")
        self.assertIn("cannot generate codename: petname failed", row.error)
        markdown = tracker.render_markdown(report)
        self.assertIn(f"## Not created\n\n- `{EMBER}`: {row.error}\n", markdown)
        self.assertEqual(mattermost_texts(client), [NOT_CREATED_TEXT])

    def test_later_run_does_not_repeat_the_creation_message(self):
        advisory = sample_advisory(EMBER, "pins a map (Ember Falcon)")
        client = FakeClient([advisory], [anchor()])
        run(client)
        self.assertEqual(len(mattermost_texts(client)), 1)
        run(client)
        self.assertEqual(len(mattermost_texts(client)), 1)
        self.assertEqual(
            client.catalog[-1]["fields"]["summary"], f"Ember Falcon: {EMBER}"
        )
