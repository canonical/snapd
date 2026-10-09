import contextlib
import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any, Dict, List, Optional

import score


CHECKLIST = """\
# Title
## General
- general
## Concurrency And Coordination
- coordination
## State And Locking
- state
## Slow Operation Locking
- slow operation locking
## Do Handler
- do
## Undo Handler
- undo
## Tests Expected
- tests
"""


def checklist_counts(checklist: str) -> Dict[str, int]:
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "handlers-quality.md"
        path.write_text(checklist, encoding="utf-8")
        return score.checklist_bullet_counts(path)


def single_bullet_counts() -> Dict[str, int]:
    return {category.heading: 1 for category in score.CATEGORIES}


def rating_counts(
    passed: int = 0, partial: int = 0, failed: int = 0, na: int = 0
) -> Dict[str, int]:
    return {"pass": passed, "partial": partial, "fail": failed, "na": na}


def all_pass() -> Dict[str, Dict[str, int]]:
    return {category.name: rating_counts(passed=1) for category in score.CATEGORIES}


def row_for(rows: List[Dict[str, Any]], category: str) -> Dict[str, Any]:
    return next(row for row in rows if row["category"] == category)


def report_output(raw_score: float, severity: Optional[str]) -> str:
    output = io.StringIO()
    with contextlib.redirect_stdout(output):
        score.print_report([], raw_score, severity)
    return output.getvalue()


class ScoreTest(unittest.TestCase):
    def test_live_checklist_bullet_counts(self) -> None:
        # update when overlord/handlers-quality.md criteria change
        counts = score.checklist_bullet_counts(
            score.repository_root() / "overlord" / "handlers-quality.md"
        )

        self.assertEqual(
            counts,
            {
                "General": 7,
                "Concurrency And Coordination": 6,
                "State And Locking": 8,
                "Slow Operation Locking": 2,
                "Do Handler": 6,
                "Undo Handler": 7,
                "Tests Expected": 10,
            },
        )

    def test_checklist_ignores_sources_bullets(self) -> None:
        counts = checklist_counts(CHECKLIST + "## Sources\n- `CODING.md`\n")

        self.assertEqual(counts["Tests Expected"], 1)

    def test_checklist_rejects_unknown_section(self) -> None:
        with self.assertRaisesRegex(
            score.InputError, "unknown checklist section: 'Extra'"
        ):
            checklist_counts(CHECKLIST + "## Extra\n- extra\n")

    def test_checklist_skips_fenced_code(self) -> None:
        counts = checklist_counts(
            CHECKLIST + "```\n- not a criterion\n## Not A Heading\n```\n"
        )

        self.assertEqual(counts["Tests Expected"], 1)

    def test_checklist_rejects_unterminated_fence(self) -> None:
        with self.assertRaisesRegex(score.InputError, "unterminated code fence"):
            checklist_counts(CHECKLIST + "```\n- not a criterion\n")

    def test_checklist_rejects_subheading_in_section(self) -> None:
        with self.assertRaisesRegex(
            score.InputError,
            "unsupported subheading in checklist section 'Tests Expected'",
        ):
            checklist_counts(CHECKLIST + "### Nested\n- nested\n")

    def test_load_input_requires_confirmed_severity(self) -> None:
        with self.assertRaisesRegex(
            score.InputError, "missing ratings input keys: confirmed_severity"
        ):
            score.load_input(io.StringIO('{"ratings": {}}'))

    def test_load_input_rejects_unknown_keys(self) -> None:
        payload = '{"ratings": {}, "confirmed_severity": null, "confirmed_severty": "high"}'

        with self.assertRaisesRegex(
            score.InputError, "unknown ratings input keys: confirmed_severty"
        ):
            score.load_input(io.StringIO(payload))

    def test_load_input_rejects_invalid_severity(self) -> None:
        for severity in ('"severe"', "1", "[]", "{}"):
            with self.subTest(severity=severity):
                payload = f'{{"ratings": {{}}, "confirmed_severity": {severity}}}'

                with self.assertRaisesRegex(
                    score.InputError, "confirmed_severity must be null or one of"
                ):
                    score.load_input(io.StringIO(payload))

    def test_load_input_accepts_null_and_known_severity(self) -> None:
        for severity, expected in (("null", None), ('"high"', "high")):
            with self.subTest(severity=severity):
                payload = f'{{"ratings": {{}}, "confirmed_severity": {severity}}}'

                ratings, loaded_severity = score.load_input(io.StringIO(payload))

                self.assertEqual(ratings, {})
                self.assertEqual(loaded_severity, expected)

    def test_main_reads_ratings_from_stdin_by_default(self) -> None:
        ratings = {"ratings": all_pass(), "confirmed_severity": None}
        with tempfile.TemporaryDirectory() as directory:
            checklist_path = Path(directory) / "handlers-quality.md"
            checklist_path.write_text(CHECKLIST, encoding="utf-8")
            result = subprocess.run(
                [
                    sys.executable,
                    score.__file__,
                    "--checklist",
                    str(checklist_path),
                ],
                input=json.dumps(ratings),
                capture_output=True,
                text=True,
                check=False,
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Final score: 10.0", result.stdout)

    def test_checklist_bullet_counts_uses_top_level_bullets(self) -> None:
        counts = checklist_counts(
            CHECKLIST.replace("- general\n", "- first\n  - nested\n- second\n")
        )

        self.assertEqual(counts["General"], 2)
        self.assertEqual(counts["Concurrency And Coordination"], 1)

    def test_validate_ratings_rejects_wrong_category_total(self) -> None:
        ratings = all_pass()
        ratings["General"]["pass"] = 0

        with self.assertRaisesRegex(score.InputError, "General ratings total 0"):
            score.validate_ratings(ratings, single_bullet_counts())

    def test_validate_ratings_rejects_missing_rating_key(self) -> None:
        ratings = all_pass()
        del ratings["General"]["na"]

        with self.assertRaisesRegex(
            score.InputError, r"^missing ratings for General: na$"
        ):
            score.validate_ratings(ratings, single_bullet_counts())

    def test_validate_ratings_rejects_non_count_values(self) -> None:
        # totals reconcile, e.g. -1 would otherwise hide an inflated pass count
        for value in (-1, True, 1.0):
            with self.subTest(value=value):
                ratings = all_pass()
                ratings["General"] = rating_counts(passed=int(1 - value), na=value)

                with self.assertRaisesRegex(
                    score.InputError, r"^General\.na must be a non-negative integer$"
                ):
                    score.validate_ratings(ratings, single_bullet_counts())

    def test_main_reports_input_errors(self) -> None:
        result = subprocess.run(
            [sys.executable, score.__file__],
            input="[]",
            capture_output=True,
            text=True,
            check=False,
        )

        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        self.assertEqual(
            result.stderr, "error: ratings input must be a JSON object\n"
        )

    def test_all_na_category_redistributes_weight(self) -> None:
        ratings = all_pass()
        ratings["Undo handler"] = rating_counts(na=1)

        rows, raw_score = score.calculate_rows(ratings)

        self.assertAlmostEqual(raw_score, 10.0)
        self.assertEqual(row_for(rows, "Undo handler")["weight"], 0.0)
        self.assertAlmostEqual(row_for(rows, "Slow operation locking")["weight"], 0.04)
        self.assertAlmostEqual(sum(row["weight"] for row in rows), 1.0)

    def test_category_weights_total_one(self) -> None:
        self.assertAlmostEqual(sum(category.weight for category in score.CATEGORIES), 1.0)
        slow_category = next(
            category
            for category in score.CATEGORIES
            if category.name == "Slow operation locking"
        )
        self.assertEqual(slow_category.weight, 0.04)
        self.assertTrue(slow_category.fixed_weight)

    def test_slow_operation_locking_weight_stays_fixed(self) -> None:
        ratings = all_pass()
        ratings["Slow operation locking"] = rating_counts(passed=1, na=1)
        ratings["Undo handler"] = rating_counts(na=1)

        rows, raw_score = score.calculate_rows(ratings)

        slow_row = row_for(rows, "Slow operation locking")
        self.assertAlmostEqual(slow_row["weight"], 0.04)
        self.assertAlmostEqual(slow_row["maximum"], 0.4)
        self.assertAlmostEqual(raw_score, 10.0)
        self.assertAlmostEqual(sum(row["weight"] for row in rows), 1.0)

    def test_slow_operation_locking_na_redistributes_weight(self) -> None:
        ratings = all_pass()
        ratings["Slow operation locking"] = rating_counts(na=2)

        rows, raw_score = score.calculate_rows(ratings)

        self.assertEqual(row_for(rows, "Slow operation locking")["weight"], 0.0)
        self.assertAlmostEqual(raw_score, 10.0)
        self.assertAlmostEqual(sum(row["weight"] for row in rows), 1.0)

    def test_failed_slow_operation_locking_costs_four_tenths(self) -> None:
        for undo_is_na in (False, True):
            with self.subTest(undo_is_na=undo_is_na):
                ratings = all_pass()
                if undo_is_na:
                    ratings["Undo handler"] = rating_counts(na=1)

                _, passing_raw_score = score.calculate_rows(ratings)
                passing_final_score = score.score_summary(
                    passing_raw_score, None
                ).final_score

                ratings["Slow operation locking"] = rating_counts(failed=2)
                _, failing_raw_score = score.calculate_rows(ratings)
                failing_final_score = score.score_summary(
                    failing_raw_score, None
                ).final_score

                self.assertAlmostEqual(passing_raw_score - failing_raw_score, 0.4)
                self.assertAlmostEqual(
                    passing_final_score - failing_final_score, 0.4
                )

    def test_only_fixed_category_applicable_is_rejected(self) -> None:
        ratings = {
            category.name: rating_counts(na=1) for category in score.CATEGORIES
        }
        ratings["Slow operation locking"] = rating_counts(passed=1, na=1)

        with self.assertRaisesRegex(score.InputError, "non-fixed"):
            score.calculate_rows(ratings)

    def test_severity_cap_never_raises_raw_score(self) -> None:
        summary = score.score_summary(5.0, "medium")

        self.assertEqual(summary.final_score, 5.0)
        self.assertIsNone(summary.binding_cap)
        self.assertEqual(summary.grade, "E")

    def test_severity_cap_can_lower_raw_score(self) -> None:
        summary = score.score_summary(9.0, "high")

        self.assertEqual(summary.raw_score, 9.0)
        self.assertEqual(summary.final_score, 5.9)
        self.assertEqual(summary.binding_cap, 5.9)
        self.assertEqual(summary.grade, "E")

    def test_critical_and_high_caps_differ(self) -> None:
        critical_grade = score.score_summary(9.0, "critical").grade
        high_grade = score.score_summary(9.0, "high").grade

        self.assertEqual(critical_grade, "F")
        self.assertEqual(high_grade, "E")

    def test_severity_cap_binds_only_below_rounded_raw_score(self) -> None:
        for raw, severity, expected_raw, expected_cap in (
            (9.44, "low", 9.4, None),
            (9.4, "low", 9.4, None),
            (9.46, "low", 9.5, 9.4),
            # float repr of 9.45 is below the tie
            (9.45, "low", 9.5, 9.4),
            (7.94, "medium", 7.9, None),
            (7.95, "medium", 8.0, 7.9),
        ):
            with self.subTest(raw=raw, severity=severity):
                summary = score.score_summary(raw, severity)

                self.assertEqual(summary.raw_score, expected_raw)
                self.assertEqual(summary.binding_cap, expected_cap)
                self.assertEqual(
                    summary.final_score, score.SEVERITY_CAPS[severity]
                )

    def test_scores_round_half_up(self) -> None:
        for raw, expected_score, expected_grade in (
            (8.25, 8.3, "B"),
            (9.25, 9.3, "A"),
            (8.75, 8.8, "B+"),
            (7.25, 7.3, "C"),
            # summation noise just below a tie
            (8.25 - 1e-14, 8.3, "B"),
            (8.249999, 8.2, "B-"),
            (7.95, 8.0, "B-"),
            (9.65, 9.7, "A+"),
        ):
            with self.subTest(raw=raw):
                summary = score.score_summary(raw, None)

                self.assertEqual(summary.raw_score, expected_score)
                self.assertEqual(summary.final_score, expected_score)
                self.assertEqual(summary.grade, expected_grade)

    def test_report_shows_rounded_raw_score(self) -> None:
        output = report_output(8.25, None)

        self.assertIn("**8.3/10.0**", output)
        self.assertIn("Raw score: 8.3\n", output)
        self.assertIn("Binding cap: none\n", output)
        self.assertIn("Final score: 8.3\n", output)
        self.assertIn("Grade: B\n", output)

    def test_report_shows_binding_cap(self) -> None:
        output = report_output(9.46, "low")

        self.assertIn("Raw score: 9.5\n", output)
        self.assertIn("Binding cap: 9.4 (low)\n", output)
        self.assertIn("Final score: 9.4\n", output)
        self.assertIn("Grade: A\n", output)


if __name__ == "__main__":
    unittest.main()