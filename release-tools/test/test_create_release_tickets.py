#!/usr/bin/python3
"""Unit tests for create-release-tickets.py.

The script filename contains a hyphen, so tests load it with importlib.
FakeJiraClient duck-types the JiraCloud methods apply_plan uses, so
tests can run offline.
"""

# pylint: disable=missing-class-docstring,missing-function-docstring,duplicate-code

import importlib.util
import inspect
import os
import re
import sys
import unittest
from contextlib import contextmanager
from io import StringIO
from unittest.mock import patch


def load_module():
    """Load the hyphenated create-release-tickets.py script as a module."""
    path = os.path.join(
        os.path.dirname(os.path.abspath(__file__)),
        "..",
        "create-release-tickets.py",
    )
    spec = importlib.util.spec_from_file_location("create_release_tickets", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


crt = load_module()
AC_FIELD = "customfield_10064"
TEAM_FIELD = "customfield_10001"
POINTS_FIELD = "customfield_10016"
FAKE_TEAM_IDS = {
    crt.DEFAULT_TEAM: "team-emea",
    crt.CROSS_DISTRO_TEAM: "team-cross",
    crt.AMER_TEAM: "team-amer",
}


def run_main(argv):
    out = StringIO()
    err = StringIO()
    old_stdout, old_stderr = sys.stdout, sys.stderr
    try:
        sys.stdout = out
        sys.stderr = err
        rc = crt.main(argv)
    finally:
        sys.stdout = old_stdout
        sys.stderr = old_stderr
    return rc, out.getvalue(), err.getvalue()


@contextmanager
def ubuntu_distro_info_stub(func):
    original = crt.ubuntu_distro_info
    crt.ubuntu_distro_info = func
    try:
        yield
    finally:
        crt.ubuntu_distro_info = original


def _field_names(fields):
    if fields is None:
        return None
    if isinstance(fields, str):
        return [name for name in fields.split(",") if name]
    return list(fields)


class FakeJiraClient:  # pylint: disable=too-many-instance-attributes
    """In-memory stand-in for the JiraCloud methods apply_plan uses."""

    def __init__(
        self,
        project="SNAPDENG",
        versions=None,
        epics=None,
        issues=None,
        field_catalog=None,
    ):
        self.project = project
        self.versions = list(versions or [])
        self.epics = list(epics or [])
        self.created = []
        self.updated = []
        self.created_versions = []
        self.created_version_bodies = []
        self.issues = {}
        self.field_catalog = field_catalog
        self.transitioned = []
        self._n = 0
        for issue in self.epics:
            self.issues[issue["key"]] = issue
        for issue in issues or []:
            self.issues[issue["key"]] = issue

    def get_project_versions(self, project_id_or_key, expand=None, data=None, **kwargs):
        del project_id_or_key, expand, data, kwargs
        return [{"name": name} for name in self.versions]

    def get_project(
        self, project_id_or_key, expand=None, properties=None, data=None, **kwargs
    ):
        del expand, properties, data, kwargs
        return {"id": "10000", "key": project_id_or_key}

    def create_version(self, data=None, **kwargs):
        del kwargs
        body = dict(data or {})
        self.created_version_bodies.append(body)
        name = body.get("name")
        self.created_versions.append(name)
        self.versions.append(name)
        return {"name": name, "projectId": body.get("projectId")}

    def get_fields(self, data=None, **kwargs):
        del data, kwargs
        if self.field_catalog is not None:
            return list(self.field_catalog)
        return [
            {"id": AC_FIELD, "name": "Acceptance Criteria"},
            {"id": TEAM_FIELD, "name": "Team"},
            {"id": POINTS_FIELD, "name": "Story Points"},
        ]

    def get_field_auto_complete_for_query_string(
        self, field_name=None, field_value=None, **kwargs
    ):
        del field_name, kwargs
        results = []
        for name, team_id in FAKE_TEAM_IDS.items():
            if field_value == name:
                results.append({"value": team_id, "displayName": name})
        return {"results": results}

    def get_issue(self, issue_id_or_key, fields=None, **kwargs):
        del kwargs
        issue = self.issues[issue_id_or_key]
        names = _field_names(fields)
        if names is None:
            return issue
        selected = {name: issue["fields"].get(name) for name in names}
        return {"key": issue_id_or_key, "fields": selected}

    def create_issue(self, update_history=None, data=None, **kwargs):
        del update_history, kwargs
        fields = dict((data or {}).get("fields") or {})
        self._n += 1
        key = f"{self.project}-{1000 + self._n}"
        fields.setdefault("labels", [])
        fields.setdefault("status", {"name": "To Do"})
        issue = {"key": key, "fields": fields}
        self.created.append(issue)
        self.issues[key] = issue
        if fields.get("issuetype", {}).get("name") == "Epic":
            self.epics.append(issue)
        return {"key": key}

    def edit_issue(self, issue_id_or_key, data=None, **kwargs):
        del kwargs
        fields = (data or {}).get("fields") or {}
        self.updated.append((issue_id_or_key, fields))
        if issue_id_or_key in self.issues:
            self.issues[issue_id_or_key]["fields"].update(fields)

    def get_transitions(self, issue_id_or_key, **kwargs):
        del issue_id_or_key, kwargs
        return {
            "transitions": [
                {"id": "21", "name": "Triage", "to": {"name": "Triaged"}},
            ]
        }

    def do_transition(self, issue_id_or_key, data=None, **kwargs):
        del kwargs
        transition_id = ((data or {}).get("transition") or {}).get("id")
        self.transitioned.append((issue_id_or_key, str(transition_id)))
        if issue_id_or_key in self.issues:
            self.issues[issue_id_or_key]["fields"]["status"] = {"name": "Triaged"}

    def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
        del kwargs
        jql = (data or {}).get("jql") or ""
        return {"issues": self._issues_for_jql(jql)}

    def _issues_for_jql(self, jql):
        if "issuetype = Epic" in jql:
            issues = list(self.epics)
            seen = {issue["key"] for issue in issues}
            for issue in self.issues.values():
                if issue["key"] in seen:
                    continue
                if issue.get("fields", {}).get("issuetype", {}).get("name") == "Epic":
                    issues.append(issue)
            return issues
        parent_match = re.search(r"parent = ([A-Z][A-Z0-9]+-\d+)", jql)
        if parent_match:
            parent_key = parent_match.group(1)
            return [
                issue
                for issue in self.issues.values()
                if crt.issue_parent_key(issue) == parent_key
            ]
        if "issuetype != Epic" in jql:
            fix_match = re.search(r'fixVersion = "([^"]*)"', jql)
            fix_version = fix_match.group(1) if fix_match else None
            epic_keys = {issue["key"] for issue in self.epics}
            matches = []
            for issue in self.issues.values():
                issue_fields = issue.get("fields", {})
                if issue["key"] in epic_keys:
                    continue
                if issue_fields.get("issuetype", {}).get("name") == "Epic":
                    continue
                if not crt.is_generated_issue(issue):
                    continue
                versions = issue_fields.get("fixVersions") or []
                names = {
                    item.get("name") if isinstance(item, dict) else item
                    for item in versions
                }
                if fix_version is not None and fix_version not in names:
                    continue
                matches.append(issue)
            return matches
        return []


def sample_plan(mod=crt, **kwargs):
    """build_plan for 2.78 with typical series, plus any override kwargs."""
    params = {
        "variant": "major",
        "version": "2.78",
        "devel": "resolute",
        "targets": ["jammy", "noble", "plucky"],
    }
    params.update(kwargs)
    return mod.build_plan(**params)


class TestVersionAndNaming(unittest.TestCase):
    def test_major_version_is_jira_version(self):
        self.assertEqual(crt.jira_version_for("major", "2.78"), "2.78")

    def test_major_rejects_patch_version(self):
        with self.assertRaises(RuntimeError) as cm:
            crt.jira_version_for("major", "2.78.1")
        self.assertEqual(
            str(cm.exception),
            'cannot use version "2.78.1" with variant "major", expected X.YY',
        )

    def test_bugfix_uses_major_jira_version(self):
        self.assertEqual(crt.jira_version_for("bugfix", "2.78.1"), "2.78")

    def test_security_uses_major_jira_version(self):
        self.assertEqual(crt.jira_version_for("security", "2.78.2"), "2.78")

    def test_bugfix_rejects_major_version(self):
        with self.assertRaises(RuntimeError) as cm:
            crt.jira_version_for("bugfix", "2.78")
        self.assertEqual(
            str(cm.exception),
            'cannot use version "2.78" with variant "bugfix", expected X.YY.Z',
        )

    def test_epic_summaries(self):
        self.assertEqual(
            crt.epic_summary_for("major", "2.78"),
            "Snapd Major Release 2.78",
        )
        self.assertEqual(
            crt.epic_summary_for("bugfix", "2.78.1"),
            "Snapd Bugfix Release 2.78.1",
        )
        self.assertEqual(
            crt.epic_summary_for("security", "2.78.1"),
            "Snapd Security Release 2.78.1",
        )

    def test_variant_from_version(self):
        self.assertEqual(crt.variant_from_version("2.78"), "major")
        self.assertEqual(crt.variant_from_version("2.77.1"), "bugfix")
        self.assertEqual(
            crt.variant_from_version("2.77.1", security=True),
            "security",
        )

    def test_variant_from_version_rejects_security_on_major(self):
        with self.assertRaises(RuntimeError) as cm:
            crt.variant_from_version("2.78", security=True)
        self.assertEqual(
            str(cm.exception),
            'cannot use --security with version "2.78", expected X.YY.Z',
        )

    def test_jira_version_override(self):
        plan = sample_plan(jira_version="snapd 2.78-hotfix")
        self.assertEqual(plan.jira_version, "snapd 2.78-hotfix")

    def test_default_fix_version_is_snapd_prefixed(self):
        plan = sample_plan()
        self.assertEqual(plan.jira_version, "snapd 2.78")
        bugfix = sample_plan(variant="bugfix", version="2.78.1")
        self.assertEqual(bugfix.jira_version, "snapd 2.78")


class TestTaskTitles(unittest.TestCase):
    def test_major_task_summaries(self):
        plan = sample_plan()
        self.assertEqual(
            [task.summary for task in plan.tasks],
            [
                "Cut release 2.78",
                "Build and upload BETA snapd snaps",
                "Build and upload snapd debs",
                "Complete Snapd Team QA BETA validation",
                "Monitor Certification Team BETA validation",
                "Request upload to resolute-proposed "
                "and facilitate LP bug verification",
                "Request upload to {jammy,noble,plucky}-proposed "
                "and facilitate LP bug verification",
                "Cross-distro: releases Arch, openSUSE & Amazon",
                "Cross-distro: Fedora and Debian",
            ],
        )

    def test_task_story_points(self):
        plan = sample_plan()
        self.assertEqual(
            [task.points for task in plan.tasks],
            [3, 2, 2, 3, 1, 2, 2, 3, 3],
        )
        self.assertEqual(sum(task.points for task in plan.tasks), 21)
        bugfix = sample_plan(variant="bugfix", version="2.78.1")
        security = sample_plan(variant="security", version="2.78.1")
        self.assertEqual(
            [task.points for task in bugfix.tasks],
            [task.points for task in plan.tasks],
        )
        self.assertEqual(
            [task.points for task in security.tasks],
            [task.points for task in plan.tasks],
        )

    def test_bugfix_cut_title_uses_patch_version(self):
        plan = sample_plan(variant="bugfix", version="2.78.1")
        self.assertEqual(plan.tasks[0].summary, "Cut release 2.78.1")
        self.assertEqual(plan.jira_version, "snapd 2.78")
        self.assertEqual(plan.epic_summary, "Snapd Bugfix Release 2.78.1")

    def test_only_a_major_release_creates_the_release_branch(self):
        """RELEASE.md: patch releases reuse the existing release/X.YY branch."""
        plan = sample_plan(variant="major", version="2.78")
        self.assertEqual(
            plan.tasks[0].checklist[0],
            "Create the release/2.78 branch from master",
        )
        for variant, version in (("bugfix", "2.78.1"), ("security", "2.78.2")):
            plan = sample_plan(variant=variant, version=version)
            self.assertEqual(
                plan.tasks[0].checklist[0],
                "Confirm the release/2.78 branch exists "
                "(patch releases do not get a new branch)",
            )

    def test_security_task_titles_are_prefixed(self):
        plan = sample_plan(variant="security", version="2.77.1")
        summaries = [task.summary for task in plan.tasks]
        self.assertEqual(len(summaries), 9)
        self.assertTrue(
            all(title.startswith(crt.SECURITY_TITLE_PREFIX) for title in summaries)
        )
        self.assertEqual(summaries[0], "Security: Cut release 2.77.1")
        self.assertFalse(
            any(
                item.startswith(crt.SECURITY_TITLE_PREFIX)
                for item in plan.tasks[0].checklist
            )
        )

    def test_default_team_is_emea_except_cross_distro(self):
        plan = sample_plan()
        self.assertEqual(plan.team, crt.DEFAULT_TEAM)
        teams = [crt.task_team(plan, task) for task in plan.tasks]
        self.assertEqual(teams[:-2], [crt.DEFAULT_TEAM] * 7)
        self.assertEqual(teams[-2:], [crt.CROSS_DISTRO_TEAM, crt.CROSS_DISTRO_TEAM])
        self.assertTrue(plan.tasks[-1].cross_distro)
        self.assertFalse(plan.tasks[0].cross_distro)
        self.assertIn(
            "Publish the GitHub release created when uploading tarballs",
            plan.tasks[3].checklist,
        )
        self.assertTrue(
            any("snapd QA tests" in item for item in plan.tasks[5].checklist)
        )
        self.assertTrue(
            any("snapd QA tests" in item for item in plan.tasks[6].checklist)
        )

    def test_beta_snaps_closes_github_milestone(self):
        plan = sample_plan()
        for task in plan.tasks:
            has_milestone = any("GitHub milestone" in item for item in task.checklist)
            if task.summary == "Build and upload BETA snapd snaps":
                self.assertTrue(has_milestone)
            else:
                self.assertFalse(has_milestone)

    def test_beta_snaps_requires_fips_provider_verification(self):
        plan = sample_plan()
        beta = next(
            task
            for task in plan.tasks
            if task.summary == "Build and upload BETA snapd snaps"
        )
        checklist = list(beta.checklist)
        verify = "Verify the FIPS snap loads the FIPS provider on a FIPS-enabled system"
        self.assertIn(verify, checklist)
        self.assertLess(
            checklist.index("Build snapd snaps on Launchpad (including FIPS)"),
            checklist.index(verify),
        )
        self.assertLess(
            checklist.index(verify),
            checklist.index("Promote FIPS revisions to fips-updates/beta"),
        )

    def test_candidate_promotion_gates_and_comms(self):
        plan = sample_plan()
        checklist = list(plan.tasks[3].checklist)
        self.assertEqual(
            checklist[-6:],
            [
                "Obtain QA sign-off to promote to candidate",
                "Confirm the certification team beta sign-off",
                "Promote snapd snap revisions to candidate",
                "Update the snapd roadmap for candidate",
                "Create the release forum post and publicize the move to candidate",
                "Publish the GitHub release created when uploading tarballs",
            ],
        )

    def test_stable_release_follows_documented_order(self):
        plan = sample_plan()
        checklist = list(plan.tasks[6].checklist)
        self.assertEqual(
            checklist[-5:],
            [
                "Confirm the candidate soak week, the WSL smoke tests, "
                "and that no stable-stopper bugs are open",
                "Request progressive stable promotion of the snapd snap",
                "Update the snapd roadmap once the progressive release completes",
                "Request the move to -updates",
                "Mark milestone bugs as Fix released",
            ],
        )

    def test_amer_team_does_not_override_cross_distro(self):
        plan = sample_plan(team=crt.AMER_TEAM)
        self.assertEqual(plan.team, crt.AMER_TEAM)
        teams = [crt.task_team(plan, task) for task in plan.tasks]
        self.assertEqual(teams[:-2], [crt.AMER_TEAM] * 7)
        self.assertEqual(teams[-2:], [crt.CROSS_DISTRO_TEAM, crt.CROSS_DISTRO_TEAM])

    def test_parse_team_maps_short_and_full_names(self):
        self.assertEqual(crt.parse_team(None), crt.DEFAULT_TEAM)
        self.assertEqual(crt.parse_team("EMEA"), crt.DEFAULT_TEAM)
        self.assertEqual(crt.parse_team("AMER"), crt.AMER_TEAM)
        self.assertEqual(crt.parse_team("Cross-distro"), crt.CROSS_DISTRO_TEAM)
        self.assertEqual(crt.parse_team("amer"), crt.AMER_TEAM)
        self.assertEqual(crt.parse_team("cross-distro"), crt.CROSS_DISTRO_TEAM)
        self.assertEqual(crt.parse_team("SnapD AMER"), crt.AMER_TEAM)
        self.assertEqual(crt.parse_team("snapd emea"), crt.DEFAULT_TEAM)
        self.assertEqual(crt.parse_team("SnapD Cross-distro"), crt.CROSS_DISTRO_TEAM)

    def test_parse_team_rejects_unknown_names(self):
        for raw in ("SRE", "SnapD"):
            with self.assertRaises(crt.UsageError) as cm:
                crt.parse_team(raw)
            message = str(cm.exception)
            self.assertIn(f'invalid --team "{raw}"', message)
            self.assertIn('expected one of "EMEA", "AMER", "Cross-distro"', message)

    def test_parse_targets_strips_whitespace(self):
        self.assertEqual(
            crt.parse_targets("jammy, noble, plucky", "resolute"),
            ["jammy", "noble", "plucky"],
        )

    def test_parse_targets_rejects_empty(self):
        with self.assertRaises(RuntimeError) as cm:
            crt.parse_targets(" , ", "resolute")
        self.assertEqual(
            str(cm.exception),
            "cannot parse --lts-targets, expected a comma-separated list of series",
        )

    def test_parse_targets_rejects_the_development_series(self):
        with self.assertRaises(RuntimeError) as cm:
            crt.parse_targets("jammy, noble", "noble")
        self.assertEqual(
            str(cm.exception),
            'cannot list development series "noble" in --lts-targets, '
            "it already gets its own noble-proposed task",
        )

    def test_task_parameters_are_known_context(self):
        allowed = set(crt.TASK_CONTEXT_KEYS)
        for fn in crt.TASKS:
            names = set(inspect.signature(fn).parameters)
            self.assertTrue(
                names <= allowed,
                f"{fn.__name__} uses unknown inputs {names - allowed}",
            )

    def test_call_task_rejects_unknown_input(self):
        def task_needs_series(*, series):
            return crt.TaskSpec(summary=series, checklist=(), points=1)

        with self.assertRaises(RuntimeError) as cm:
            crt.call_task(task_needs_series, {"version": "2.78"})
        self.assertEqual(
            str(cm.exception),
            "cannot fill task_needs_series, unknown input series",
        )


class TestPayloads(unittest.TestCase):
    def test_epic_payload_shape(self):
        plan = sample_plan()
        fields = crt.epic_fields("SNAPDENG", plan, AC_FIELD)
        self.assertEqual(fields["project"], {"key": "SNAPDENG"})
        self.assertEqual(fields["issuetype"], {"name": "Epic"})
        self.assertEqual(fields["summary"], "Snapd Major Release 2.78")
        self.assertEqual(fields["fixVersions"], [{"name": "snapd 2.78"}])
        self.assertEqual(fields["parent"], {"key": "SNAPDENG-34819"})
        self.assertEqual(fields["labels"], [crt.GENERATED_LABEL])
        self.assertEqual(fields["description"]["type"], "doc")
        self.assertEqual(crt.task_item_texts(fields["description"]), [])
        self.assertEqual(
            crt.task_item_texts(fields[AC_FIELD]),
            [task.summary for task in plan.tasks],
        )
        hrefs = crt.linked_hrefs(fields["description"])
        self.assertIn(crt.RELEASE_MD_URL, hrefs)
        self.assertIn(crt.SRU_PACKAGE_NOTES_URL, hrefs)
        self.assertIn(crt.SRU_SNAPD_UPDATES_URL, hrefs)
        self.assertNotIn(POINTS_FIELD, fields)

    def test_instructions_are_shared_with_dry_run(self):
        plan = sample_plan()
        out = StringIO()
        crt.print_plan(plan, out=out)
        text = out.getvalue()
        self.assertIn(crt.INSTRUCTION_HEADING, text)
        fields = crt.epic_fields("SNAPDENG", plan, AC_FIELD)
        hrefs = crt.linked_hrefs(fields["description"])
        for url in (
            crt.RELEASE_MD_URL,
            crt.SRU_PACKAGE_NOTES_URL,
            crt.SRU_SNAPD_UPDATES_URL,
        ):
            self.assertIn(url, text)
            self.assertIn(url, hrefs)

    def test_task_payload_shape(self):
        plan = sample_plan()
        task = plan.tasks[0]
        fields = crt.task_fields("SNAPDENG", plan, task, "SNAPDENG-1001", AC_FIELD)
        self.assertEqual(fields["project"], {"key": "SNAPDENG"})
        self.assertEqual(fields["issuetype"], {"name": "Task"})
        self.assertEqual(fields["summary"], "Cut release 2.78")
        self.assertEqual(fields["fixVersions"], [{"name": "snapd 2.78"}])
        self.assertEqual(fields["parent"], {"key": "SNAPDENG-1001"})
        self.assertEqual(fields["labels"], [crt.GENERATED_LABEL])
        self.assertEqual(crt.task_item_texts(fields["description"]), [])
        self.assertEqual(crt.task_item_texts(fields[AC_FIELD]), list(task.checklist))
        self.assertNotIn(POINTS_FIELD, fields)
        hrefs = crt.linked_hrefs(fields["description"])
        self.assertIn(crt.RELEASE_MD_URL, hrefs)
        with_team = crt.task_fields(
            "SNAPDENG",
            plan,
            task,
            "SNAPDENG-1001",
            AC_FIELD,
            team_field=TEAM_FIELD,
            team_id="team-emea",
            story_points_field=POINTS_FIELD,
        )
        self.assertEqual(with_team[TEAM_FIELD], "team-emea")
        self.assertEqual(with_team[POINTS_FIELD], 3)

    def test_epic_name_field_from_env(self):
        plan = sample_plan()
        original = os.environ.get("JIRA_EPIC_NAME_FIELD")
        os.environ["JIRA_EPIC_NAME_FIELD"] = "customfield_10011"
        try:
            fields = crt.epic_fields("SNAPDENG", plan, AC_FIELD)
        finally:
            if original is None:
                del os.environ["JIRA_EPIC_NAME_FIELD"]
            else:
                os.environ["JIRA_EPIC_NAME_FIELD"] = original
        self.assertEqual(fields["customfield_10011"], plan.epic_summary)


class TestDryRunAndApply(unittest.TestCase):
    def test_dry_run_prints_plan_without_client(self):
        plan = sample_plan()
        out = StringIO()
        crt.print_plan(plan, out=out)
        text = out.getvalue()
        self.assertIn(
            "Dry-run (pass --apply to create issues, --force to update)", text
        )
        self.assertIn("Fix Version: snapd 2.78", text)
        self.assertIn("Parent: SNAPDENG-34819", text)
        self.assertIn("Epic: Snapd Major Release 2.78", text)
        self.assertIn("Team: SnapD EMEA", text)
        self.assertIn("Team: SnapD Cross-distro", text)
        self.assertIn("Description:", text)
        self.assertIn("Acceptance Criteria:", text)
        self.assertIn("Follow the snapd release process:", text)
        self.assertIn("Cut release 2.78", text)
        self.assertIn("Task: Cut release 2.78", text)
        self.assertIn("Points: 3", text)
        epic_block, _sep, rest = text.partition("Task:")
        self.assertNotIn("Points:", epic_block)
        self.assertIn("Points: 3", rest)
        self.assertIn("Cross-distro: Fedora and Debian", text)

    def test_main_dry_run_does_not_need_credentials(self):
        original_email = os.environ.pop("JIRA_EMAIL", None)
        original_token = os.environ.pop("JIRA_API_TOKEN", None)
        try:
            rc, out, _err = run_main(
                [
                    "2.78",
                    "--dev-target",
                    "resolute",
                    "--lts-targets",
                    "jammy,noble,plucky",
                ]
            )
        finally:
            if original_email is not None:
                os.environ["JIRA_EMAIL"] = original_email
            if original_token is not None:
                os.environ["JIRA_API_TOKEN"] = original_token
        self.assertEqual(rc, 0)
        self.assertIn("Dry-run", out)

    def test_apply_summary_uses_browse_links(self):
        plan = sample_plan()
        result = crt.ApplyResult(
            epic_key="SNAPDENG-37457",
            tasks=(
                ("SNAPDENG-37458", "Cut release 2.77.1"),
                ("SNAPDENG-37459", "Build and upload BETA snapd snaps"),
            ),
            created_keys=("SNAPDENG-37459",),
            unexpected=(("SNAPDENG-50", "Investigate beta flake"),),
            epic_created=False,
            updated=True,
        )
        out = StringIO()
        crt.print_apply_result(plan, result, out=out, hyperlinks=True)
        text = out.getvalue()
        epic = crt.issue_link("SNAPDENG-37457")
        task = crt.issue_link("SNAPDENG-37458")
        created = crt.issue_link("SNAPDENG-37459")
        extra = crt.issue_link("SNAPDENG-50")
        parent = crt.issue_link(plan.parent_epic)
        self.assertIn(f"Updated {epic}: Snapd Major Release 2.78", text)
        self.assertIn(f"  {task}: Cut release 2.77.1\n", text)
        self.assertIn(f"  {created}: Build and upload BETA snapd snaps [created]", text)
        self.assertIn(f"  {extra}: Investigate beta flake", text)
        self.assertIn(f"Epic: {epic}", text)
        self.assertIn(f"Parent: {parent}", text)
        self.assertIn("\033]8;;" + crt.browse_url("SNAPDENG-37458"), text)

    def test_apply_summary_plain_keys_without_hyperlinks(self):
        plan = sample_plan()
        result = crt.ApplyResult(
            epic_key="SNAPDENG-37457",
            tasks=(("SNAPDENG-37458", "Cut release 2.77.1"),),
            created_keys=(),
            unexpected=(),
            epic_created=True,
        )
        out = StringIO()
        crt.print_apply_result(plan, result, out=out, hyperlinks=False)
        text = out.getvalue()
        self.assertIn("Created SNAPDENG-37457: Snapd Major Release 2.78", text)
        self.assertIn("  SNAPDENG-37458: Cut release 2.77.1\n", text)
        self.assertIn("Epic: SNAPDENG-37457", text)
        self.assertNotIn("https://", text)

    def test_apply_summary_found_when_existing_not_updated(self):
        plan = sample_plan()
        result = crt.ApplyResult(
            epic_key="SNAPDENG-37457",
            tasks=(("SNAPDENG-37458", "Cut release 2.77.1"),),
            created_keys=(),
            unexpected=(),
            epic_created=False,
            updated=False,
        )
        out = StringIO()
        crt.print_apply_result(plan, result, out=out, hyperlinks=False)
        text = out.getvalue()
        self.assertIn("Found SNAPDENG-37457: Snapd Major Release 2.78", text)
        self.assertNotIn("Updated SNAPDENG-37457", text)

    def test_apply_creates_epic_tasks_and_patches_checklist(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        result = crt.apply_plan(plan, client)
        self.assertEqual(result.epic_key, "SNAPDENG-1001")
        self.assertEqual(len(result.tasks), 9)
        self.assertEqual(len(client.created), 10)
        self.assertEqual(client.created[0]["fields"]["issuetype"]["name"], "Epic")
        self.assertEqual(
            client.created[0]["fields"]["parent"],
            {"key": "SNAPDENG-34819"},
        )
        self.assertEqual(
            client.created[0]["fields"]["fixVersions"],
            [{"name": "snapd 2.78"}],
        )
        for issue in client.created:
            self.assertEqual(issue["fields"]["labels"], [crt.GENERATED_LABEL])
        self.assertEqual(
            client.created[0]["fields"][TEAM_FIELD],
            FAKE_TEAM_IDS[crt.DEFAULT_TEAM],
        )
        for issue in client.created[1:-2]:
            self.assertEqual(
                issue["fields"][TEAM_FIELD],
                FAKE_TEAM_IDS[crt.DEFAULT_TEAM],
            )
        for issue in client.created[-2:]:
            self.assertEqual(
                issue["fields"][TEAM_FIELD],
                FAKE_TEAM_IDS[crt.CROSS_DISTRO_TEAM],
            )
        for issue in client.created[1:]:
            self.assertEqual(issue["fields"]["issuetype"]["name"], "Task")
            self.assertEqual(issue["fields"]["parent"], {"key": "SNAPDENG-1001"})
            self.assertEqual(issue["fields"]["fixVersions"], [{"name": "snapd 2.78"}])
        self.assertNotIn(POINTS_FIELD, client.created[0]["fields"])
        created_points = [issue["fields"][POINTS_FIELD] for issue in client.created[1:]]
        self.assertEqual(created_points, [task.points for task in plan.tasks])
        self.assertEqual(len(client.updated), 1)
        patched_key, patched_fields = client.updated[0]
        self.assertEqual(patched_key, "SNAPDENG-1001")
        self.assertEqual(patched_fields["parent"], {"key": "SNAPDENG-34819"})
        self.assertEqual(patched_fields["fixVersions"], [{"name": "snapd 2.78"}])
        self.assertNotIn(POINTS_FIELD, patched_fields)
        texts = crt.task_item_texts(patched_fields[AC_FIELD])
        self.assertEqual(texts[0], "SNAPDENG-1002 Cut release 2.78")
        self.assertEqual(texts[-1], "SNAPDENG-1010 Cross-distro: Fedora and Debian")
        self.assertEqual(result.unexpected, ())
        self.assertTrue(result.epic_created)
        self.assertFalse(result.updated)
        self.assertEqual(len(result.created_keys), 9)
        keys = [result.epic_key] + [key for key, _ in result.tasks]
        for key in keys:
            self.assertEqual(
                client.issues[key]["fields"]["status"]["name"],
                "Triaged",
            )
        self.assertEqual(len(client.transitioned), 10)

    def test_apply_amer_team_skips_cross_distro(self):
        plan = sample_plan(team=crt.AMER_TEAM)
        client = FakeJiraClient(versions=["snapd 2.78"])
        crt.apply_plan(plan, client)
        self.assertEqual(
            client.created[0]["fields"][TEAM_FIELD],
            FAKE_TEAM_IDS[crt.AMER_TEAM],
        )
        self.assertEqual(
            client.created[1]["fields"][TEAM_FIELD],
            FAKE_TEAM_IDS[crt.AMER_TEAM],
        )
        self.assertEqual(
            client.created[-1]["fields"][TEAM_FIELD],
            FAKE_TEAM_IDS[crt.CROSS_DISTRO_TEAM],
        )

    def test_already_triaged_is_not_transitioned_again(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        crt.apply_plan(plan, client)
        self.assertEqual(len(client.transitioned), 10)
        crt.apply_plan(plan, client)
        self.assertEqual(len(client.transitioned), 10)
        crt.apply_plan(plan, client, force=True)
        self.assertEqual(len(client.transitioned), 10)

    def test_rerun_does_not_reset_in_progress_status(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        key = first.tasks[0][0]
        client.issues[key]["fields"]["status"] = {"name": "In Progress"}
        client.issues[first.epic_key]["fields"]["status"] = {"name": "In Progress"}
        crt.apply_plan(plan, client, force=True)
        self.assertEqual(client.issues[key]["fields"]["status"]["name"], "In Progress")
        self.assertEqual(
            client.issues[first.epic_key]["fields"]["status"]["name"],
            "In Progress",
        )

    def test_rerun_triages_generated_todo_after_partial_apply(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        key = first.tasks[0][0]
        client.issues[key]["fields"]["status"] = {"name": "To Do"}
        client.issues[first.epic_key]["fields"]["status"] = {"name": "To Do"}
        crt.apply_plan(plan, client)
        self.assertEqual(client.issues[key]["fields"]["status"]["name"], "To Do")
        self.assertEqual(
            client.issues[first.epic_key]["fields"]["status"]["name"],
            "To Do",
        )
        crt.apply_plan(plan, client, force=True)
        self.assertEqual(client.issues[key]["fields"]["status"]["name"], "Triaged")
        self.assertEqual(
            client.issues[first.epic_key]["fields"]["status"]["name"],
            "Triaged",
        )

    def test_missing_triaged_transition(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        client.get_transitions = lambda _key: {"transitions": []}
        with self.assertRaises(RuntimeError) as cm:
            crt.apply_plan(plan, client)
        self.assertIn('cannot move SNAPDENG-1001 to "Triaged"', str(cm.exception))

    def test_empty_transitions_payload(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        client.get_transitions = lambda _key: None
        with self.assertRaises(RuntimeError) as cm:
            crt.apply_plan(plan, client)
        self.assertIn('cannot move SNAPDENG-1001 to "Triaged"', str(cm.exception))

    def test_unstarted_statuses(self):
        for name in ("To Do", "to do", "TODO", "Open", ""):
            issue = {"fields": {"status": {"name": name}}}
            self.assertTrue(crt.is_unstarted_status(issue), name)
        for name in ("In Progress", "Triaged", "Done"):
            issue = {"fields": {"status": {"name": name}}}
            self.assertFalse(crt.is_unstarted_status(issue), name)
        self.assertTrue(crt.is_unstarted_status({"fields": {"status": None}}))

    def test_idempotent_rerun_reuses_epic_and_tasks(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        second = crt.apply_plan(plan, client)
        self.assertEqual(second.epic_key, first.epic_key)
        self.assertEqual(second.tasks, first.tasks)
        self.assertFalse(second.epic_created)
        self.assertFalse(second.updated)
        self.assertEqual(second.created_keys, ())
        self.assertEqual(len(client.created), 10)
        self.assertEqual(len(client.updated), 1)

    def test_existing_epic_gets_parent_link(self):
        plan = sample_plan()
        client = FakeJiraClient(
            versions=["snapd 2.78"],
            epics=[
                {
                    "key": "SNAPDENG-9",
                    "fields": {
                        "summary": "Snapd Major Release 2.78",
                        "description": None,
                    },
                }
            ],
        )
        result = crt.apply_plan(plan, client)
        self.assertEqual(result.epic_key, "SNAPDENG-9")
        self.assertFalse(result.updated)
        self.assertNotIn("parent", client.issues["SNAPDENG-9"]["fields"])
        result = crt.apply_plan(plan, client, force=True)
        self.assertTrue(result.updated)
        self.assertEqual(
            client.issues["SNAPDENG-9"]["fields"]["parent"],
            {"key": "SNAPDENG-34819"},
        )

    def test_parent_override(self):
        plan = sample_plan(parent_epic="SNAPDENG-1")
        fields = crt.epic_fields("SNAPDENG", plan, AC_FIELD)
        self.assertEqual(fields["parent"], {"key": "SNAPDENG-1"})

    def test_unlinked_task_is_attached_to_epic(self):
        plan = sample_plan()
        orphan = {
            "key": "SNAPDENG-20",
            "fields": {
                "issuetype": {"name": "Task"},
                "summary": "Cut release 2.78",
                "fixVersions": [{"name": "snapd 2.78"}],
                "labels": [crt.GENERATED_LABEL],
            },
        }
        client = FakeJiraClient(
            versions=["snapd 2.78"],
            epics=[
                {
                    "key": "SNAPDENG-9",
                    "fields": {
                        "summary": "Snapd Major Release 2.78",
                        "description": None,
                    },
                }
            ],
            issues=[orphan],
        )
        result = crt.apply_plan(plan, client)
        self.assertEqual(result.epic_key, "SNAPDENG-9")
        self.assertEqual(result.created_keys, ())
        self.assertNotEqual(result.tasks[0][0] if result.tasks else None, "SNAPDENG-20")
        self.assertIsNone(crt.issue_parent_key(client.issues["SNAPDENG-20"]))
        result = crt.apply_plan(plan, client, force=True)
        self.assertEqual(result.tasks[0], ("SNAPDENG-20", "Cut release 2.78"))
        self.assertNotIn("SNAPDENG-20", result.created_keys)
        self.assertEqual(
            client.issues["SNAPDENG-20"]["fields"]["parent"],
            {"key": "SNAPDENG-9"},
        )
        self.assertEqual(client.issues["SNAPDENG-20"]["fields"][POINTS_FIELD], 3)
        self.assertEqual(len(result.created_keys), 8)

    def test_unlinked_unlabeled_task_is_not_attached(self):
        plan = sample_plan()
        orphan = {
            "key": "SNAPDENG-20",
            "fields": {
                "issuetype": {"name": "Task"},
                "summary": "Cut release 2.78",
                "fixVersions": [{"name": "snapd 2.78"}],
                "labels": [],
            },
        }
        client = FakeJiraClient(
            versions=["snapd 2.78"],
            epics=[
                {
                    "key": "SNAPDENG-9",
                    "fields": {
                        "summary": "Snapd Major Release 2.78",
                        "description": None,
                    },
                }
            ],
            issues=[orphan],
        )
        result = crt.apply_plan(plan, client)
        self.assertEqual(result.created_keys, ())
        self.assertNotEqual(result.tasks[0][0] if result.tasks else None, "SNAPDENG-20")
        self.assertIn("SNAPDENG-20", client.issues)
        self.assertIsNone(crt.issue_parent_key(client.issues["SNAPDENG-20"]))
        self.assertNotIn(POINTS_FIELD, client.issues["SNAPDENG-20"]["fields"])
        result = crt.apply_plan(plan, client, force=True)
        self.assertNotEqual(result.tasks[0][0], "SNAPDENG-20")
        self.assertIsNone(crt.issue_parent_key(client.issues["SNAPDENG-20"]))
        self.assertNotIn(POINTS_FIELD, client.issues["SNAPDENG-20"]["fields"])
        self.assertEqual(len(result.created_keys), 9)

    def test_matching_unlinked_task_filters_candidates(self):
        def issue(key, summary, labels, parent=None):
            fields = {"summary": summary, "labels": labels}
            if parent is not None:
                fields["parent"] = {"key": parent}
            return {"key": key, "fields": fields}

        summary = "Cut release 2.78"
        issues = [
            issue("SNAPDENG-1", "Cut release 2.78.1", [crt.GENERATED_LABEL]),
            issue("SNAPDENG-2", summary, []),
            issue("SNAPDENG-3", summary, [crt.GENERATED_LABEL], parent="SNAPDENG-99"),
            issue("SNAPDENG-4", summary, [crt.GENERATED_LABEL]),
        ]
        found = crt.matching_unlinked_task(issues, summary, "SNAPDENG-9")
        self.assertEqual(found["key"], "SNAPDENG-4")
        # A task already under this epic is reusable, one under another is not.
        owned = issue("SNAPDENG-5", summary, [crt.GENERATED_LABEL], parent="SNAPDENG-9")
        self.assertEqual(
            crt.matching_unlinked_task([owned], summary, "SNAPDENG-9")["key"],
            "SNAPDENG-5",
        )
        self.assertIsNone(crt.matching_unlinked_task(issues[:3], summary, "SNAPDENG-9"))

    def test_existing_task_keeps_epic_parent(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        crt.apply_plan(plan, client, force=True)
        for key, _summary in first.tasks:
            self.assertEqual(
                client.issues[key]["fields"]["parent"],
                {"key": first.epic_key},
            )

    def test_idempotent_creates_only_missing_tasks(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        missing = first.tasks[-1]
        del client.issues[missing[0]]
        result = crt.apply_plan(plan, client)
        self.assertEqual(result.epic_key, first.epic_key)
        self.assertEqual(result.created_keys, ())
        self.assertNotIn(missing[0], [key for key, _summary in result.tasks])
        result = crt.apply_plan(plan, client, force=True)
        self.assertEqual(len(result.created_keys), 1)
        self.assertEqual(result.tasks[-1][1], missing[1])
        self.assertNotEqual(result.tasks[-1][0], missing[0])

    def test_unexpected_children_are_marked(self):
        plan = sample_plan()
        extra = {
            "key": "SNAPDENG-50",
            "fields": {
                "summary": "Investigate beta flake",
                "parent": {"key": "SNAPDENG-9"},
                "labels": [],
            },
        }
        client = FakeJiraClient(
            versions=["snapd 2.78"],
            epics=[
                {
                    "key": "SNAPDENG-9",
                    "fields": {
                        "summary": "Snapd Major Release 2.78",
                        "description": None,
                    },
                }
            ],
            issues=[extra],
        )
        result = crt.apply_plan(plan, client)
        self.assertEqual(result.epic_key, "SNAPDENG-9")
        self.assertEqual(
            result.unexpected, (("SNAPDENG-50", "Investigate beta flake"),)
        )
        self.assertEqual(result.tasks, ())
        self.assertEqual(result.created_keys, ())
        self.assertEqual(client.updated, [])
        self.assertEqual(client.issues["SNAPDENG-50"]["fields"]["labels"], [])
        result = crt.apply_plan(plan, client, force=True)
        self.assertEqual(
            result.unexpected, (("SNAPDENG-50", "Investigate beta flake"),)
        )
        self.assertEqual(client.issues["SNAPDENG-50"]["fields"]["labels"], [])
        self.assertNotIn("fixVersions", client.issues["SNAPDENG-50"]["fields"])
        patched_key, patched_fields = client.updated[-1]
        self.assertEqual(patched_key, "SNAPDENG-9")
        texts = crt.task_item_texts(patched_fields[AC_FIELD])
        self.assertIn("UNEXPECTED: SNAPDENG-50 Investigate beta flake", texts)
        self.assertEqual(len(result.tasks), 9)

    def test_generated_unexpected_child_is_labeled(self):
        plan = sample_plan()
        extra = {
            "key": "SNAPDENG-50",
            "fields": {
                "summary": "Investigate beta flake",
                "parent": {"key": "SNAPDENG-9"},
                "labels": [crt.GENERATED_LABEL],
            },
        }
        client = FakeJiraClient(
            versions=["snapd 2.78"],
            epics=[
                {
                    "key": "SNAPDENG-9",
                    "fields": {
                        "summary": "Snapd Major Release 2.78",
                        "description": None,
                    },
                }
            ],
            issues=[extra],
        )
        result = crt.apply_plan(plan, client)
        self.assertEqual(
            result.unexpected, (("SNAPDENG-50", "Investigate beta flake"),)
        )
        self.assertEqual(
            client.issues["SNAPDENG-50"]["fields"]["labels"],
            [crt.GENERATED_LABEL],
        )
        result = crt.apply_plan(plan, client, force=True)
        self.assertEqual(
            result.unexpected, (("SNAPDENG-50", "Investigate beta flake"),)
        )
        self.assertIn(
            crt.UNEXPECTED_LABEL,
            client.issues["SNAPDENG-50"]["fields"]["labels"],
        )

    def test_expected_task_loses_unexpected_label(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        key = first.tasks[0][0]
        client.issues[key]["fields"]["labels"] = [
            crt.GENERATED_LABEL,
            crt.UNEXPECTED_LABEL,
        ]
        crt.apply_plan(plan, client)
        self.assertIn(
            crt.UNEXPECTED_LABEL,
            client.issues[key]["fields"]["labels"],
        )
        crt.apply_plan(plan, client, force=True)
        self.assertNotIn(
            crt.UNEXPECTED_LABEL,
            client.issues[key]["fields"]["labels"],
        )
        self.assertIn(
            crt.GENERATED_LABEL,
            client.issues[key]["fields"]["labels"],
        )

    def test_preserves_epic_checkbox_state(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        criteria = client.issues[first.epic_key]["fields"][AC_FIELD]
        for node in criteria["content"]:
            if node.get("type") == "taskList":
                node["content"][0]["attrs"]["state"] = "DONE"
                break
        crt.apply_plan(plan, client)
        texts = crt.task_items_with_state(
            client.issues[first.epic_key]["fields"][AC_FIELD]
        )
        self.assertEqual(texts[0], (f"{first.tasks[0][0]} {first.tasks[0][1]}", "DONE"))
        crt.apply_plan(plan, client, force=True)
        texts = crt.task_items_with_state(
            client.issues[first.epic_key]["fields"][AC_FIELD]
        )
        self.assertEqual(texts[0], (f"{first.tasks[0][0]} {first.tasks[0][1]}", "DONE"))
        self.assertEqual(texts[1][1], "TODO")

    def test_force_rewrites_existing_task_criteria(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=["snapd 2.78"])
        first = crt.apply_plan(plan, client)
        key = first.tasks[0][0]
        criteria = client.issues[key]["fields"][AC_FIELD]
        for node in criteria["content"]:
            if node.get("type") == "taskList":
                node["content"][0]["attrs"]["state"] = "DONE"
                break
        crt.apply_plan(plan, client)
        texts = crt.task_items_with_state(client.issues[key]["fields"][AC_FIELD])
        self.assertEqual(texts[0][1], "DONE")
        crt.apply_plan(plan, client, force=True)
        texts = crt.task_items_with_state(client.issues[key]["fields"][AC_FIELD])
        self.assertEqual(texts[0][1], "TODO")

    def test_force_skips_unlabeled_matching_task(self):
        plan = sample_plan()
        criteria = crt.acceptance_criteria(plan.tasks[0].checklist)
        for node in criteria["content"]:
            if node.get("type") == "taskList":
                node["content"][0]["attrs"]["state"] = "DONE"
                break
        child = {
            "key": "SNAPDENG-20",
            "fields": {
                "issuetype": {"name": "Task"},
                "summary": "Cut release 2.78",
                "parent": {"key": "SNAPDENG-9"},
                "labels": [],
                AC_FIELD: criteria,
            },
        }
        client = FakeJiraClient(
            versions=["snapd 2.78"],
            epics=[
                {
                    "key": "SNAPDENG-9",
                    "fields": {
                        "summary": "Snapd Major Release 2.78",
                        "description": None,
                    },
                }
            ],
            issues=[child],
        )
        result = crt.apply_plan(plan, client, force=True)
        self.assertEqual(result.tasks[0], ("SNAPDENG-20", "Cut release 2.78"))
        texts = crt.task_items_with_state(
            client.issues["SNAPDENG-20"]["fields"][AC_FIELD]
        )
        self.assertEqual(texts[0][1], "DONE")
        self.assertEqual(client.issues["SNAPDENG-20"]["fields"]["labels"], [])
        self.assertNotIn("fixVersions", client.issues["SNAPDENG-20"]["fields"])
        self.assertNotIn(TEAM_FIELD, client.issues["SNAPDENG-20"]["fields"])
        self.assertNotIn(
            "SNAPDENG-20",
            [key for key, _tid in client.transitioned],
        )

    def test_missing_jira_version(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=[])
        with self.assertRaises(RuntimeError) as cm:
            crt.apply_plan(plan, client)
        self.assertEqual(
            str(cm.exception),
            'cannot find Jira version "snapd 2.78" in SNAPDENG; '
            "create it in Jira first or pass --create-version",
        )

    def test_empty_versions_payload(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=[])
        client.get_project_versions = lambda *_args, **_kwargs: None
        with self.assertRaises(RuntimeError) as cm:
            crt.apply_plan(plan, client)
        self.assertIn('cannot find Jira version "snapd 2.78"', str(cm.exception))

    def test_create_version_for_major(self):
        plan = sample_plan()
        client = FakeJiraClient(versions=[])
        result = crt.apply_plan(plan, client, create_version=True)
        self.assertEqual(client.created_versions, ["snapd 2.78"])
        self.assertEqual(result.epic_key, "SNAPDENG-1001")

    def test_create_version_posts_numeric_project_id(self):
        self.assertEqual(
            crt.version_create_body({"id": "10000", "key": "SNAPDENG"}, "snapd 2.78"),
            {"name": "snapd 2.78", "projectId": 10000},
        )
        client = FakeJiraClient(versions=[])
        crt.create_project_version(client, "snapd 2.78")
        self.assertEqual(
            client.created_version_bodies,
            [{"name": "snapd 2.78", "projectId": 10000}],
        )

    def test_create_version_requires_project_id(self):
        for payload in ({"key": "SNAPDENG"}, {"id": "not-a-number"}, None):
            with self.assertRaises(RuntimeError) as cm:
                crt.version_create_body(payload, "snapd 2.78")
            self.assertEqual(
                str(cm.exception),
                "cannot find Jira project id for SNAPDENG",
            )

    def test_search_helpers_handle_empty_payload(self):
        calls = []

        class EmptySearch:
            def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
                del kwargs
                calls.append(data)
                return None

        jira = EmptySearch()
        self.assertIsNone(crt.find_epic(jira, "Snapd Major Release 2.78"))
        self.assertEqual(crt.list_children(jira, "SNAPDENG-9"), [])
        self.assertEqual(crt.find_generated_issues(jira, "snapd 2.78"), [])
        self.assertEqual(
            [body["jql"] for body in calls],
            [
                "project = SNAPDENG AND issuetype = Epic "
                'AND summary ~ "Snapd Major Release 2.78"',
                "parent = SNAPDENG-9 ORDER BY created ASC",
                "project = SNAPDENG AND issuetype != Epic "
                f'AND labels = "{crt.GENERATED_LABEL}" '
                'AND fixVersion = "snapd 2.78"',
            ],
        )
        self.assertEqual(calls[1]["fields"], ["summary", "labels", "parent"])

    def test_find_epic_filters_fuzzy_summary_matches(self):
        """summary ~ is a text match, so only an exact summary may be reused."""

        class FuzzySearch:
            def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
                del data, kwargs
                return {
                    "issues": [
                        {
                            "key": "SNAPDENG-8",
                            "fields": {"summary": "Snapd Major Release 2.78.1"},
                        },
                        {
                            "key": "SNAPDENG-9",
                            "fields": {"summary": "Snapd Major Release 2.78"},
                        },
                    ]
                }

        jira = FuzzySearch()
        found = crt.find_epic(jira, "Snapd Major Release 2.78")
        self.assertEqual(found["key"], "SNAPDENG-9")
        self.assertIsNone(crt.find_epic(jira, "Snapd Major Release 2.7"))

    def test_jql_string_escapes_quotes_and_backslashes(self):
        self.assertEqual(crt.jql_string("snapd 2.78"), '"snapd 2.78"')
        self.assertEqual(crt.jql_string('a"b'), '"a\\"b"')
        self.assertEqual(crt.jql_string("a\\b"), '"a\\\\b"')

    def test_search_follows_next_page_token(self):
        bodies = []

        class PagedJira:
            def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
                del kwargs
                bodies.append(data)
                if "nextPageToken" not in data:
                    return {
                        "issues": [{"key": "SNAPDENG-1"}],
                        "nextPageToken": "page-2",
                    }
                return {"issues": [{"key": "SNAPDENG-2"}]}

        issues = crt.search_jql(
            PagedJira(), "parent = SNAPDENG-9", ["summary"], limit=10
        )
        self.assertEqual(
            [issue["key"] for issue in issues],
            ["SNAPDENG-1", "SNAPDENG-2"],
        )
        self.assertEqual(bodies[1]["nextPageToken"], "page-2")
        self.assertEqual([body["maxResults"] for body in bodies], [10, 9])

    def test_search_stops_at_limit(self):
        bodies = []

        class EndlessJira:
            def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
                del kwargs
                bodies.append(data)
                return {
                    "issues": [{"key": f"SNAPDENG-{len(bodies)}"}],
                    "nextPageToken": "more",
                }

        issues = crt.search_jql(
            EndlessJira(), 'Team = "SnapD EMEA"', ["summary"], limit=1
        )
        self.assertEqual([issue["key"] for issue in issues], ["SNAPDENG-1"])
        self.assertEqual(len(bodies), 1)

    def test_list_children_pages_to_the_search_bound(self):
        bodies = []

        class PagedJira:
            def search_and_reconsile_issues_using_jql_post(self, data=None, **kwargs):
                del kwargs
                bodies.append(data)
                page = len(bodies)
                issues = [{"key": f"SNAPDENG-{page}"}]
                if page < 3:
                    return {"issues": issues, "nextPageToken": f"page-{page + 1}"}
                return {"issues": issues}

        children = crt.list_children(PagedJira(), "SNAPDENG-9")
        self.assertEqual(len(children), 3)
        self.assertEqual(bodies[0]["maxResults"], crt.SEARCH_PAGE_SIZE)
        self.assertEqual(
            [body.get("nextPageToken") for body in bodies],
            [None, "page-2", "page-3"],
        )

    def test_create_version_rejected_for_bugfix(self):
        # Rejected on the variant alone, whether or not the version exists.
        for versions in ([], ["snapd 2.78"]):
            plan = sample_plan(variant="bugfix", version="2.78.1")
            client = FakeJiraClient(versions=versions)
            with self.assertRaises(RuntimeError) as cm:
                crt.apply_plan(plan, client, create_version=True)
            self.assertEqual(
                str(cm.exception),
                'cannot use --create-version with variant "bugfix", '
                "Jira versions exist only for major releases",
            )
            self.assertEqual(client.created, [])
            self.assertEqual(client.created_versions, [])

    def test_acceptance_criteria_field_id_from_name(self):
        self.assertEqual(crt.acceptance_criteria_field_id(FakeJiraClient()), AC_FIELD)

    def test_acceptance_criteria_field_id_uses_env(self):
        original = os.environ.get("JIRA_ACCEPTANCE_CRITERIA_FIELD")
        os.environ["JIRA_ACCEPTANCE_CRITERIA_FIELD"] = "customfield_9"
        try:
            self.assertEqual(
                crt.acceptance_criteria_field_id(FakeJiraClient(field_catalog=[])),
                "customfield_9",
            )
        finally:
            if original is None:
                os.environ.pop("JIRA_ACCEPTANCE_CRITERIA_FIELD", None)
            else:
                os.environ["JIRA_ACCEPTANCE_CRITERIA_FIELD"] = original

    def test_acceptance_criteria_field_id_missing(self):
        client = FakeJiraClient(field_catalog=[])
        with self.assertRaises(RuntimeError) as cm:
            crt.acceptance_criteria_field_id(client)
        self.assertIn("cannot find Jira field", str(cm.exception))

    def test_story_points_field_id_from_name(self):
        self.assertEqual(crt.story_points_field_id(FakeJiraClient()), POINTS_FIELD)

    def test_story_points_field_id_uses_env(self):
        original = os.environ.get("JIRA_STORY_POINTS_FIELD")
        os.environ["JIRA_STORY_POINTS_FIELD"] = "customfield_16"
        try:
            self.assertEqual(
                crt.story_points_field_id(FakeJiraClient(field_catalog=[])),
                "customfield_16",
            )
        finally:
            if original is None:
                os.environ.pop("JIRA_STORY_POINTS_FIELD", None)
            else:
                os.environ["JIRA_STORY_POINTS_FIELD"] = original

    def test_story_points_field_id_missing(self):
        client = FakeJiraClient(
            field_catalog=[
                {"id": AC_FIELD, "name": "Acceptance Criteria"},
                {"id": TEAM_FIELD, "name": "Team"},
            ]
        )
        with self.assertRaises(RuntimeError) as cm:
            crt.story_points_field_id(client)
        self.assertEqual(
            str(cm.exception),
            'cannot find Jira field "Story Points"; '
            "set JIRA_STORY_POINTS_FIELD to the customfield id",
        )

    def test_issue_story_points_keeps_fraction(self):
        issue = {"fields": {POINTS_FIELD: 3.5}}
        self.assertEqual(crt.issue_story_points(issue, POINTS_FIELD), 3.5)
        issue = {"fields": {POINTS_FIELD: "3.5"}}
        self.assertEqual(crt.issue_story_points(issue, POINTS_FIELD), 3.5)

    def test_ensure_story_points_updates_fractional_mismatch(self):
        client = FakeJiraClient()
        client.issues["SNAPDENG-1"] = {
            "key": "SNAPDENG-1",
            "fields": {POINTS_FIELD: 3.5},
        }
        crt.ensure_story_points(client, "SNAPDENG-1", POINTS_FIELD, 3)
        self.assertEqual(client.issues["SNAPDENG-1"]["fields"][POINTS_FIELD], 3)

    def test_ensure_story_points_accepts_whole_float(self):
        client = FakeJiraClient()
        client.issues["SNAPDENG-1"] = {
            "key": "SNAPDENG-1",
            "fields": {POINTS_FIELD: 3.0},
        }
        crt.ensure_story_points(client, "SNAPDENG-1", POINTS_FIELD, 3)
        self.assertEqual(client.updated, [])
        self.assertEqual(client.issues["SNAPDENG-1"]["fields"][POINTS_FIELD], 3.0)

    def test_field_lookups_handle_empty_payload(self):
        """An empty /field body must still yield the actionable CLI error."""

        class EmptyFields:
            def get_fields(self, data=None, **kwargs):
                del data, kwargs
                return None

        jira = EmptyFields()
        for resolver, field in (
            (crt.acceptance_criteria_field_id, "Acceptance Criteria"),
            (crt.team_field_id, "Team"),
            (crt.story_points_field_id, "Story Points"),
        ):
            with self.assertRaises(RuntimeError) as cm:
                resolver(jira)
            self.assertIn(f'cannot find Jira field "{field}"', str(cm.exception))

    def test_apply_without_credentials(self):
        original_email = os.environ.pop("JIRA_EMAIL", None)
        original_token = os.environ.pop("JIRA_API_TOKEN", None)
        try:
            with self.assertRaises(RuntimeError) as cm:
                crt.credentials()
            self.assertEqual(
                str(cm.exception),
                "cannot find Jira credentials, please set JIRA_EMAIL and JIRA_API_TOKEN",
            )
        finally:
            if original_email is not None:
                os.environ["JIRA_EMAIL"] = original_email
            if original_token is not None:
                os.environ["JIRA_API_TOKEN"] = original_token

    def test_jira_error_message_for_http(self):
        err = Exception("nope")
        err.response = type("R", (), {"status_code": 400, "text": "nope"})()
        self.assertEqual(
            crt.jira_error_message(err),
            "cannot talk to Jira: HTTP 400: nope",
        )

    def test_jira_error_message_for_timeout(self):
        self.assertEqual(
            crt.jira_error_message(TimeoutError("timed out")),
            f"cannot talk to Jira: timed out after {crt.REQUEST_TIMEOUT_SECONDS}s",
        )

    def test_cli_prints_runtime_error(self):
        with patch.object(
            crt, "main", side_effect=RuntimeError("cannot find Jira field")
        ):
            err_buf = StringIO()
            with patch.object(sys, "stderr", err_buf):
                rc = crt.cli(["2.78", "--apply"])
        self.assertEqual(rc, 1)
        self.assertEqual(err_buf.getvalue(), "cannot find Jira field\n")

    def test_cli_prints_jira_io_error(self):
        err = Exception("connection refused")
        with patch.object(crt, "jira_io_errors", return_value=(Exception,)):
            with patch.object(crt, "main", side_effect=err):
                err_buf = StringIO()
                with patch.object(sys, "stderr", err_buf):
                    rc = crt.cli(["2.78", "--apply"])
        self.assertEqual(rc, 1)
        self.assertEqual(
            err_buf.getvalue(),
            "cannot talk to Jira: connection refused\n",
        )

    def test_connect_jira_uses_cloud_v3(self):
        try:
            import atlassian.jira as jira_mod
        except ImportError:
            raise unittest.SkipTest("atlassian-python-api is not installed")
        with patch.object(jira_mod, "JiraCloud") as mock_cls:
            mock_cls.return_value = object()
            crt.connect_jira("https://jira.example", "a@b.c", "token")
        mock_cls.assert_called_once_with(
            "https://jira.example",
            username="a@b.c",
            password="token",
            timeout=crt.REQUEST_TIMEOUT_SECONDS,
            api_version=3,
        )


class TestCLI(unittest.TestCase):
    def test_main_no_args_prints_cobra_help(self):
        rc, out, err = run_main([])
        self.assertEqual(rc, 2)
        self.assertEqual(err, "")
        self.assertIn("Usage:", out)
        self.assertIn("Flags:", out)
        self.assertNotIn("required", out.lower())
        self.assertNotIn("the following arguments are required", out.lower())

    def test_help_contains_examples_and_typed_flags(self):
        rc, out, err = run_main(["--help"])
        self.assertEqual(rc, 0)
        self.assertEqual(err, "")
        self.assertIn("Create Jira epic and tasks for a snapd release.", out)
        self.assertIn("JIRA_EMAIL", out)
        self.assertIn("JIRA_API_TOKEN", out)
        self.assertIn("classic unscoped API token", out)
        self.assertIn("atlassian-python-api", out)
        self.assertIn("pip install atlassian-python-api", out)
        self.assertIn(crt.JIRA_API_TOKEN_URL, out)
        self.assertLess(
            out.find("Create Jira epic and tasks for a snapd release."),
            out.find("Usage:"),
        )
        self.assertLess(out.find(crt.JIRA_API_TOKEN_URL), out.find("Usage:"))
        self.assertIn("Examples:", out)
        self.assertIn("--dev-target string", out)
        self.assertIn("--lts-targets strings", out)
        self.assertIn("--team string", out)
        self.assertNotIn("--Team string", out)
        self.assertIn("script-generated", out)
        self.assertIn("preexisting script-generated epic and tasks", out)
        self.assertLess(out.find("--team string"), out.find("--lts-targets strings"))
        self.assertLess(
            out.find("--lts-targets strings"), out.find("--dev-target string")
        )
        self.assertLess(out.find("--dev-target string"), out.find("-h, --help"))
        self.assertIn("  create-release-tickets.py <version> [flags]", out)
        for removed in (
            "--variant",
            "--jira-url",
            "--jira-version",
            "--parent",
            "--project",
        ):
            self.assertNotIn(removed, out)

    def test_help_short_flag(self):
        rc, out, _err = run_main(["-h"])
        self.assertEqual(rc, 0)
        self.assertIn("Usage:", out)
        self.assertIn("Flags:", out)

    def test_main_infers_major_from_version(self):
        rc, out, _err = run_main(
            ["2.78", "--dev-target", "resolute", "--lts-targets", "jammy,noble,plucky"]
        )
        self.assertEqual(rc, 0)
        self.assertIn("Snapd Major Release 2.78", out)

    def test_main_infers_bugfix_from_version(self):
        rc, out, _err = run_main(
            [
                "2.77.1",
                "--dev-target",
                "resolute",
                "--lts-targets",
                "jammy,noble,plucky",
            ]
        )
        self.assertEqual(rc, 0)
        self.assertIn("Snapd Bugfix Release 2.77.1", out)

    def test_main_security_flag_marks_patch(self):
        rc, out, _err = run_main(
            [
                "2.77.1",
                "--security",
                "--dev-target",
                "resolute",
                "--lts-targets",
                "jammy,noble,plucky",
            ]
        )
        self.assertEqual(rc, 0)
        self.assertIn("Snapd Security Release 2.77.1", out)
        self.assertIn("Security: Cut release 2.77.1", out)

    def test_invalid_team_is_cobra_style(self):
        rc, out, err = run_main(["2.78", "--team", "SRE"])
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn('invalid --team "SRE"', err)
        self.assertIn('"EMEA"', err)
        self.assertIn('"AMER"', err)
        self.assertIn('"Cross-distro"', err)

    def test_amer_team_printed_in_dry_run(self):
        rc, out, _err = run_main(
            [
                "2.78",
                "--team",
                "AMER",
                "--dev-target",
                "resolute",
                "--lts-targets",
                "jammy,noble,plucky",
            ]
        )
        self.assertEqual(rc, 0)
        self.assertIn("Team: SnapD AMER", out)
        self.assertIn("Team: SnapD Cross-distro", out)

    def test_team_flag_accepts_legacy_capitalized_alias(self):
        rc, out, _err = run_main(
            [
                "2.78",
                "--Team",
                "AMER",
                "--dev-target",
                "resolute",
                "--lts-targets",
                "jammy,noble,plucky",
            ]
        )
        self.assertEqual(rc, 0)
        self.assertIn(f"Team: {crt.AMER_TEAM}", out)

    def test_team_flag_accepts_full_jira_name(self):
        rc, out, _err = run_main(
            [
                "2.78",
                "--team",
                "SnapD AMER",
                "--dev-target",
                "resolute",
                "--lts-targets",
                "jammy,noble,plucky",
            ]
        )
        self.assertEqual(rc, 0)
        self.assertIn(f"Team: {crt.AMER_TEAM}", out)

    def test_abbreviated_flags_are_rejected(self):
        for flag in ("--app", "--for", "--create-v"):
            rc, out, err = run_main(["2.78", flag])
            self.assertEqual(rc, 2, flag)
            self.assertEqual(out, "")
            self.assertIn(f"Error: unknown flag: {flag}", err)

    def test_unknown_flag_is_cobra_style(self):
        rc, out, err = run_main(["2.78", "--variant", "major"])
        self.assertEqual(rc, 2)
        self.assertEqual(out, "")
        self.assertIn("Error: unknown flag: --variant", err)
        self.assertIn("Run 'create-release-tickets.py --help' for usage.", err)

    def test_default_ubuntu_series_drops_devel(self):
        def fake(args):
            if list(args) == ["--devel", "-c"]:
                return "resolute\n"
            if list(args) == ["--supported", "-c"]:
                return "jammy\nnoble\nplucky\nresolute\n"
            raise AssertionError(args)

        with ubuntu_distro_info_stub(fake):
            devel, targets = crt.default_ubuntu_series()
        self.assertEqual(devel, "resolute")
        self.assertEqual(targets, ["jammy", "noble", "plucky"])

    def test_explicit_dev_target_is_not_an_sru_target(self):
        def fake(args):
            if list(args) == ["--devel", "-c"]:
                return "resolute\n"
            if list(args) == ["--supported", "-c"]:
                return "jammy\nnoble\nplucky\nresolute\n"
            raise AssertionError(args)

        with ubuntu_distro_info_stub(fake):
            devel, targets = crt.resolve_ubuntu_series("noble", None)
        self.assertEqual(devel, "noble")
        self.assertEqual(targets, ["jammy", "plucky"])

    def test_explicit_lts_targets_are_used_verbatim(self):
        def fake(args):
            if list(args) == ["--devel", "-c"]:
                return "resolute\n"
            if list(args) == ["--supported", "-c"]:
                return "jammy\nnoble\nplucky\nresolute\n"
            raise AssertionError(args)

        with ubuntu_distro_info_stub(fake):
            devel, targets = crt.resolve_ubuntu_series(None, "jammy,noble")
        self.assertEqual(devel, "resolute")
        self.assertEqual(targets, ["jammy", "noble"])

    def test_lts_targets_cannot_repeat_the_development_series(self):
        """Otherwise the same series gets both a devel and an SRU task."""

        def fake(args):
            if list(args) == ["--devel", "-c"]:
                return "resolute\n"
            if list(args) == ["--supported", "-c"]:
                return "jammy\nnoble\nplucky\nresolute\n"
            raise AssertionError(args)

        with ubuntu_distro_info_stub(fake):
            # Explicit --dev-target listed again in --lts-targets.
            with self.assertRaises(RuntimeError) as cm:
                crt.resolve_ubuntu_series("noble", "jammy,noble")
            self.assertIn('development series "noble"', str(cm.exception))
            # The auto-detected devel series listed in --lts-targets.
            with self.assertRaises(RuntimeError) as cm:
                crt.resolve_ubuntu_series(None, "jammy,resolute")
            self.assertIn('development series "resolute"', str(cm.exception))

    def test_default_ubuntu_series_missing_tool(self):
        def fake(_args):
            raise FileNotFoundError("ubuntu-distro-info")

        with ubuntu_distro_info_stub(fake):
            with self.assertRaises(RuntimeError) as cm:
                crt.default_ubuntu_series()
        self.assertEqual(str(cm.exception), crt.DISTRO_SERIES_ERROR)
        # ubuntu-distro-info ships in distro-info, not in distro-info-data.
        self.assertIn("install distro-info or", crt.DISTRO_SERIES_ERROR)

    def test_main_uses_distro_info_defaults(self):
        def fake(args):
            if list(args) == ["--devel", "-c"]:
                return "resolute\n"
            if list(args) == ["--supported", "-c"]:
                return "jammy\nnoble\nplucky\nresolute\n"
            raise AssertionError(args)

        with ubuntu_distro_info_stub(fake):
            rc, out, _err = run_main(["2.78"])
        self.assertEqual(rc, 0)
        self.assertIn("resolute-proposed", out)
        self.assertIn("{jammy,noble,plucky}-proposed", out)

    def test_explicit_series_skips_distro_info(self):
        def boom(_args):
            raise AssertionError("should not call ubuntu-distro-info")

        with ubuntu_distro_info_stub(boom):
            rc, out, _err = run_main(
                ["2.78", "--dev-target", "resolute", "--lts-targets", "jammy,noble"]
            )
        self.assertEqual(rc, 0)
        self.assertIn("resolute-proposed", out)
        self.assertIn("{jammy,noble}-proposed", out)


if __name__ == "__main__":
    unittest.main()
