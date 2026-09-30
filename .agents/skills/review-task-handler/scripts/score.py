import argparse
import json
import sys
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path
from typing import Any, Dict, List, Mapping, NamedTuple, Optional, TextIO, Tuple


class Category(NamedTuple):
    name: str
    heading: str
    weight: float
    fixed_weight: bool = False


CATEGORIES = (
    Category("General", "General", 0.09),
    Category("Coordination", "Concurrency And Coordination", 0.14),
    Category("State and locking", "State And Locking", 0.195),
    Category("Slow operation locking", "Slow Operation Locking", 0.04, True),
    Category("Do handler", "Do Handler", 0.195),
    Category("Undo handler", "Undo Handler", 0.15),
    Category("Tests", "Tests Expected", 0.19),
)
# unrated checklist sections
OTHER_CHECKLIST_HEADINGS = frozenset({"Sources"})
PAYLOAD_KEYS = ("ratings", "confirmed_severity")
RATINGS = ("pass", "partial", "fail", "na")
SEVERITY_CAPS = {
    "critical": 3.9,
    "high": 5.9,
    "medium": 7.9,
    "low": 9.4,
}
GRADE_THRESHOLDS = (
    (9.7, "A+"),
    (9.3, "A"),
    (9.0, "A-"),
    (8.7, "B+"),
    (8.3, "B"),
    (8.0, "B-"),
    (7.7, "C+"),
    (7.3, "C"),
    (7.0, "C-"),
    (6.7, "D+"),
    (6.3, "D"),
    (6.0, "D-"),
    (5.0, "E"),
    (0.0, "F"),
)


class InputError(Exception):
    pass


def repository_root() -> Path:
    return Path(__file__).resolve().parents[4]


def checklist_bullet_counts(checklist_path: Path) -> Dict[str, int]:
    headings = {category.heading for category in CATEGORIES}
    counts = {heading: 0 for heading in headings}
    current_heading: Optional[str] = None

    try:
        lines = checklist_path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise InputError(f"cannot read checklist {checklist_path}: {error}") from error

    in_fence = False
    for line in lines:
        if line.startswith(("```", "~~~")):
            in_fence = not in_fence
        elif in_fence:
            continue
        elif line.startswith("## "):
            heading = line[3:].strip()
            if heading not in headings and heading not in OTHER_CHECKLIST_HEADINGS:
                raise InputError(f"unknown checklist section: {heading!r}")
            current_heading = heading if heading in headings else None
        elif current_heading is not None and line.startswith("###"):
            raise InputError(
                f"unsupported subheading in checklist section {current_heading!r}: "
                f"{line.strip()!r}"
            )
        elif current_heading is not None and line.startswith("- "):
            counts[current_heading] += 1
    if in_fence:
        raise InputError("unterminated code fence in checklist")

    missing = sorted(heading for heading, count in counts.items() if count == 0)
    if missing:
        raise InputError(
            "cannot find checklist bullets for sections: " + ", ".join(missing)
        )
    return counts


def load_input(stream: TextIO) -> Tuple[Mapping[str, Any], Optional[str]]:
    try:
        payload = json.load(stream)
    except (json.JSONDecodeError, OSError) as error:
        raise InputError(f"cannot read ratings JSON: {error}") from error

    if not isinstance(payload, dict):
        raise InputError("ratings input must be a JSON object")

    unknown_keys = sorted(set(payload) - set(PAYLOAD_KEYS))
    missing_keys = [key for key in PAYLOAD_KEYS if key not in payload]
    if unknown_keys:
        raise InputError("unknown ratings input keys: " + ", ".join(unknown_keys))
    if missing_keys:
        raise InputError("missing ratings input keys: " + ", ".join(missing_keys))

    ratings = payload["ratings"]
    if not isinstance(ratings, dict):
        raise InputError("ratings must be a JSON object")

    severity = payload["confirmed_severity"]
    if severity is not None and (
        not isinstance(severity, str) or severity not in SEVERITY_CAPS
    ):
        choices = ", ".join(sorted(SEVERITY_CAPS))
        raise InputError(f"confirmed_severity must be null or one of: {choices}")
    return ratings, severity


def validate_ratings(
    ratings: Mapping[str, Any], checklist_counts: Mapping[str, int]
) -> Dict[str, Dict[str, int]]:
    expected_categories = {category.name for category in CATEGORIES}
    unknown_categories = sorted(set(ratings) - expected_categories)
    missing_categories = sorted(expected_categories - set(ratings))
    if unknown_categories:
        raise InputError("unknown rating categories: " + ", ".join(unknown_categories))
    if missing_categories:
        raise InputError("missing rating categories: " + ", ".join(missing_categories))

    validated: Dict[str, Dict[str, int]] = {}
    for category in CATEGORIES:
        values = ratings[category.name]
        if not isinstance(values, dict):
            raise InputError(f"ratings for {category.name} must be a JSON object")

        unknown_ratings = sorted(set(values) - set(RATINGS))
        missing_ratings = sorted(set(RATINGS) - set(values))
        if unknown_ratings:
            raise InputError(
                f"unknown ratings for {category.name}: " + ", ".join(unknown_ratings)
            )
        if missing_ratings:
            raise InputError(
                f"missing ratings for {category.name}: " + ", ".join(missing_ratings)
            )

        category_values: Dict[str, int] = {}
        for rating in RATINGS:
            value = values[rating]
            if isinstance(value, bool) or not isinstance(value, int) or value < 0:
                raise InputError(
                    f"{category.name}.{rating} must be a non-negative integer"
                )
            category_values[rating] = value

        expected_count = checklist_counts[category.heading]
        actual_count = sum(category_values.values())
        if actual_count != expected_count:
            raise InputError(
                f"{category.name} ratings total {actual_count}, expected {expected_count} "
                f"from checklist section {category.heading!r}"
            )
        validated[category.name] = category_values

    return validated


def letter_grade(score: float) -> str:
    for threshold, grade in GRADE_THRESHOLDS:
        if score >= threshold:
            return grade
    raise AssertionError("grade thresholds do not cover score")


def calculate_rows(
    ratings: Mapping[str, Mapping[str, int]]
) -> Tuple[List[Dict[str, Any]], float]:
    applicability: Dict[str, int] = {}
    fixed_weight = 0.0
    scalable_weight = 0.0
    for category in CATEGORIES:
        values = ratings[category.name]
        applicable = values["pass"] + values["partial"] + values["fail"]
        applicability[category.name] = applicable
        if applicable > 0:
            if category.fixed_weight:
                fixed_weight += category.weight
            else:
                scalable_weight += category.weight

    if scalable_weight == 0.0:
        raise InputError("at least one non-fixed checklist category must be applicable")

    rows: List[Dict[str, Any]] = []
    raw_score = 0.0
    for category in CATEGORIES:
        values = ratings[category.name]
        applicable = applicability[category.name]
        if applicable == 0:
            category_score = None
            normalized_weight = 0.0
            contribution = 0.0
            maximum = 0.0
        else:
            category_score = (values["pass"] + 0.5 * values["partial"]) / applicable
            if category.fixed_weight:
                normalized_weight = category.weight
            else:
                normalized_weight = (
                    category.weight / scalable_weight * (1.0 - fixed_weight)
                )
            maximum = 10.0 * normalized_weight
            contribution = maximum * category_score
            raw_score += contribution

        rows.append(
            {
                "category": category.name,
                "weight": normalized_weight,
                "values": values,
                "category_score": category_score,
                "contribution": contribution,
                "maximum": maximum,
            }
        )
    return rows, raw_score


class ScoreSummary(NamedTuple):
    raw_score: float
    final_score: float
    binding_cap: Optional[float]
    grade: str


def round_score(score: float) -> float:
    # absorb float summation noise so that x.x5 ties round half up
    exact = Decimal(f"{score:.9f}")
    return float(exact.quantize(Decimal("0.1"), rounding=ROUND_HALF_UP))


def score_summary(raw_score: float, severity: Optional[str]) -> ScoreSummary:
    cap = SEVERITY_CAPS[severity] if severity is not None else 10.0
    # round once so the reported raw score, cap, final score and letter agree
    raw_score = round_score(raw_score)
    final_score = min(raw_score, cap)
    binding_cap = cap if cap < raw_score else None
    return ScoreSummary(raw_score, final_score, binding_cap, letter_grade(final_score))


def print_report(
    rows: List[Dict[str, Any]], raw_score: float, severity: Optional[str]
) -> None:
    summary = score_summary(raw_score, severity)
    print("| Category | Weight | Pass | Partial | Fail | N/A | Raw contribution |")
    print("|---|---:|---:|---:|---:|---:|---:|")
    for row in rows:
        values = row["values"]
        if row["category_score"] is None:
            weight = "N/A"
            contribution = "N/A"
        else:
            # rows are rounded independently and need not sum to the raw score or 100%
            weight = f"{100.0 * row['weight']:.1f}%"
            contribution = f"{row['contribution']:.1f}/{row['maximum']:.1f}"
        print(
            f"| {row['category']} | {weight} | {values['pass']} | "
            f"{values['partial']} | {values['fail']} | {values['na']} | "
            f"{contribution} |"
        )
    print(
        f"| **Raw score** | **100.0%** | | | | | **{summary.raw_score:.1f}/10.0** |"
    )

    print()
    print(f"Raw score: {summary.raw_score:.1f}")
    if summary.binding_cap is not None:
        print(f"Binding cap: {summary.binding_cap:.1f} ({severity})")
    else:
        print("Binding cap: none")
    print(f"Final score: {summary.final_score:.1f}")
    print(f"Grade: {summary.grade}")


def open_ratings(path: str) -> TextIO:
    if path == "-":
        return sys.stdin
    try:
        return open(path, encoding="utf-8")
    except OSError as error:
        raise InputError(f"cannot open ratings file {path}: {error}") from error


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate and score a snapd task-handler quality review"
    )
    parser.add_argument(
        "ratings",
        nargs="?",
        default="-",
        help="ratings JSON file (default: standard input)",
    )
    parser.add_argument(
        "--checklist",
        type=Path,
        default=repository_root() / "overlord" / "handlers-quality.md",
        help="path to handlers-quality.md",
    )
    arguments = parser.parse_args()

    stream: Optional[TextIO] = None
    try:
        stream = open_ratings(arguments.ratings)
        ratings, severity = load_input(stream)
        checklist_counts = checklist_bullet_counts(arguments.checklist)
        validated = validate_ratings(ratings, checklist_counts)
        rows, raw_score = calculate_rows(validated)
        print_report(rows, raw_score, severity)
    except InputError as error:
        print(f"error: {error}", file=sys.stderr)
        return 2
    finally:
        if stream is not None and stream is not sys.stdin:
            stream.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())