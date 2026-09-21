import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import score


class ScoreTest(unittest.TestCase):
    def test_main_reads_ratings_from_stdin_by_default(self) -> None:
        checklist = """\
## General
- general
## Concurrency And Coordination
- coordination
## State And Locking
- state
## Do Handler
- do
## Undo Handler
- undo
## Tests Expected
- tests
"""
        ratings = {
            "ratings": {
                category: {"pass": 1, "partial": 0, "fail": 0, "na": 0}
                for category, _, _ in score.CATEGORIES
            },
            "confirmed_severity": None,
        }
        with tempfile.TemporaryDirectory() as directory:
            checklist_path = Path(directory) / "handlers-quality.md"
            checklist_path.write_text(checklist, encoding="utf-8")
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
        checklist = """\
## General
- first
  - nested
- second
## Concurrency And Coordination
- coordination
## State And Locking
- state
## Do Handler
- do
## Undo Handler
- undo
## Tests Expected
- tests
"""
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "handlers-quality.md"
            path.write_text(checklist, encoding="utf-8")
            counts = score.checklist_bullet_counts(path)

        self.assertEqual(counts["General"], 2)
        self.assertEqual(counts["Concurrency And Coordination"], 1)

    def test_validate_ratings_rejects_wrong_category_total(self) -> None:
        checklist_counts = {
            heading: 1 for _, heading, _ in score.CATEGORIES
        }
        ratings = {
            category: {"pass": 1, "partial": 0, "fail": 0, "na": 0}
            for category, _, _ in score.CATEGORIES
        }
        ratings["General"]["pass"] = 0

        with self.assertRaisesRegex(score.InputError, "General ratings total 0"):
            score.validate_ratings(ratings, checklist_counts)

    def test_all_na_category_redistributes_weight(self) -> None:
        ratings = {
            category: {"pass": 1, "partial": 0, "fail": 0, "na": 0}
            for category, _, _ in score.CATEGORIES
        }
        ratings["Undo handler"] = {"pass": 0, "partial": 0, "fail": 0, "na": 1}

        rows, raw_score = score.calculate_rows(ratings)

        self.assertAlmostEqual(raw_score, 10.0)
        undo_row = next(row for row in rows if row["category"] == "Undo handler")
        self.assertEqual(undo_row["weight"], 0.0)
        self.assertAlmostEqual(sum(row["weight"] for row in rows), 1.0)

    def test_severity_cap_never_raises_raw_score(self) -> None:
        final_score, binding_cap, grade = score.score_summary(5.0, "medium")

        self.assertEqual(final_score, 5.0)
        self.assertIsNone(binding_cap)
        self.assertEqual(grade, "E")

    def test_severity_cap_can_lower_raw_score(self) -> None:
        final_score, binding_cap, grade = score.score_summary(9.0, "high")

        self.assertEqual(final_score, 5.9)
        self.assertEqual(binding_cap, 5.9)
        self.assertEqual(grade, "E")

    def test_critical_and_high_caps_differ(self) -> None:
        _, _, critical_grade = score.score_summary(9.0, "critical")
        _, _, high_grade = score.score_summary(9.0, "high")

        self.assertEqual(critical_grade, "F")
        self.assertEqual(high_grade, "E")

    def test_reported_score_matches_letter_grade(self) -> None:
        for raw, expected_score, expected_grade in (
            (7.95, 8.0, "B-"),
            (9.65, 9.7, "A+"),
        ):
            final_score, _, grade = score.score_summary(raw, None)

            self.assertEqual(final_score, expected_score)
            self.assertEqual(grade, expected_grade)


if __name__ == "__main__":
    unittest.main()