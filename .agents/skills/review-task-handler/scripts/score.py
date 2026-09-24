import argparse
import json
import sys
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

    for line in lines:
        if line.startswith("## "):
            heading = line[3:].strip()
            current_heading = heading if heading in headings else None
        elif current_heading is not None and line.startswith("- "):
            counts[current_heading] += 1

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

    ratings = payload.get("ratings")
    if not isinstance(ratings, dict):
        raise InputError("ratings must be a JSON object")

    severity = payload.get("confirmed_severity")
    if severity is not None and severity not in SEVERITY_CAPS:
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
                f"unknown ratings for {category}: " + ", ".join(unknown_ratings)
            )
        if missing_ratings:
            raise InputError(
                f"missing ratings for {category}: " + ", ".join(missing_ratings)
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

    if fixed_weight == 0.0 and scalable_weight == 0.0:
        raise InputError("at least one checklist category must be applicable")
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


def score_summary(
    raw_score: float, severity: Optional[str]
) -> Tuple[float, Optional[float], str]:
    cap = SEVERITY_CAPS[severity] if severity is not None else 10.0
    # round before grading so the reported score and letter always agree
    final_score = round(min(raw_score, cap), 1)
    binding_cap = cap if cap < raw_score else None
    return final_score, binding_cap, letter_grade(final_score)


def print_report(
    rows: List[Dict[str, Any]], raw_score: float, severity: Optional[str]
) -> None:
    raw_score = round(raw_score, 1)
    print("| Category | Weight | Pass | Partial | Fail | N/A | Raw contribution |")
    print("|---|---:|---:|---:|---:|---:|---:|")
    for row in rows:
        values = row["values"]
        if row["category_score"] is None:
            weight = "N/A"
            contribution = "N/A"
        else:
            weight = f"{100.0 * row['weight']:.1f}%"
            contribution = f"{row['contribution']:.1f}/{row['maximum']:.1f}"
        print(
            f"| {row['category']} | {weight} | {values['pass']} | "
            f"{values['partial']} | {values['fail']} | {values['na']} | "
            f"{contribution} |"
        )
    print(f"| **Raw score** | **100.0%** | | | | | **{raw_score:.1f}/10.0** |")

    final_score, binding_cap, grade = score_summary(raw_score, severity)
    print()
    print(f"Raw score: {raw_score:.1f}")
    if binding_cap is not None:
        print(f"Binding cap: {binding_cap:.1f} ({severity})")
    else:
        print("Binding cap: none")
    print(f"Final score: {final_score:.1f}")
    print(f"Grade: {grade}")


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