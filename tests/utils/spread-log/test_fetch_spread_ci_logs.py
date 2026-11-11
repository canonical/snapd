import sys
import tempfile
import unittest
from argparse import ArgumentTypeError
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))

from fetch_spread_ci_logs import (  # noqa: E402
    build_parser,
    create_output_dir,
    fetch_direct_jobs,
    parse_job_url,
)


class FetchSpreadCILogsTests(unittest.TestCase):
    def test_parse_snapd_job_url(self):
        self.assertEqual(
            parse_job_url("https://github.com/canonical/snapd/actions/runs/123/job/456"),
            ("123", "456"),
        )

    def test_parse_job_url_with_query(self):
        self.assertEqual(
            parse_job_url(
                "https://github.com/canonical/snapd/actions/runs/42680747006/"
                "job/129383722199?pr=17839"
            ),
            ("42680747006", "129383722199"),
        )

    def test_reject_foreign_job_url(self):
        with self.assertRaises(ArgumentTypeError):
            parse_job_url("https://example.com/canonical/snapd/actions/runs/123/job/456")

    def test_cli_accepts_multiple_job_urls(self):
        args = build_parser().parse_args(
            [
                "--job-url",
                "https://github.com/canonical/snapd/actions/runs/123/job/456",
                "https://github.com/canonical/snapd/actions/runs/789/job/012",
            ]
        )
        self.assertEqual(len(args.job_url), 2)

    def test_cli_accepts_session_dir(self):
        args = build_parser().parse_args(
            [
                "--session-dir",
                "/tmp/spread-failure-workflow",
                "--job-url",
                "https://github.com/canonical/snapd/actions/runs/123/job/456",
            ]
        )
        self.assertEqual(args.session_dir, Path("/tmp/spread-failure-workflow"))

    def test_create_output_dir_under_session_dir(self):
        with tempfile.TemporaryDirectory() as session_dir:
            output_dir = create_output_dir("123", Path(session_dir))
            second_output_dir = create_output_dir("456", Path(session_dir))
            self.assertEqual(output_dir.parent, Path(session_dir))
            self.assertEqual(second_output_dir.parent, Path(session_dir))
            self.assertNotEqual(output_dir, second_output_dir)
            self.assertTrue(output_dir.is_dir())
            self.assertTrue(second_output_dir.is_dir())

    def test_create_output_dir_defaults_to_workspace_log_dir(self):
        with tempfile.TemporaryDirectory() as workspace:
            with patch(
                "fetch_spread_ci_logs.Path.cwd", return_value=Path(workspace)
            ):
                output_dir = create_output_dir("123")
            self.assertEqual(output_dir.parent, Path(workspace) / ".log")
            self.assertTrue(output_dir.is_dir())

    def test_create_output_dir_requires_existing_session_dir(self):
        with tempfile.TemporaryDirectory() as temporary_dir:
            missing_session_dir = Path(temporary_dir) / "missing"
            with self.assertRaisesRegex(RuntimeError, "does not exist"):
                create_output_dir("123", missing_session_dir)

    def test_direct_job_fetch_stores_linked_log(self):
        job_url = (
            "https://github.com/canonical/snapd/actions/runs/42680747006/"
            "job/129383722199?pr=17839"
        )
        with tempfile.TemporaryDirectory() as session_dir:
            args = build_parser().parse_args(
                [
                    "--session-dir",
                    session_dir,
                    "--job-url",
                    job_url,
                ]
            )
            with (
                patch("fetch_spread_ci_logs.run_gh", return_value="{}"),
                patch("fetch_spread_ci_logs.run_gh_to_file") as fetch_log,
                patch("fetch_spread_ci_logs.fetch_results", return_value=[]),
            ):
                self.assertEqual(fetch_direct_jobs(args), 0)
            fetch_log.assert_called_once()
            gh_args = fetch_log.call_args.args[0]
            self.assertIn("--job", gh_args)
            self.assertIn("129383722199", gh_args)


if __name__ == "__main__":
    unittest.main()
