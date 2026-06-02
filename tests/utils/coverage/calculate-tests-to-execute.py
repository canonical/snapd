#!/usr/bin/env python3

import argparse
import json
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
	parser = argparse.ArgumentParser(
		description=(
			"Calculate tests to execute for selected systems by combining core-set tests "
			"and tests relevant to the provided files."
		)
	)
	parser.add_argument(
		"--files",
		nargs="+",
		help="Repository-relative file paths to evaluate (e.g. arch/arch.go)",
	)
	parser.add_argument(
		"--files-list",
		help="Path to a newline-separated file list (alternative to --files)",
	)
	parser.add_argument(
		"--systems",
		nargs="+",
		required=True,
		help="Subset of systems (e.g. openstack:ubuntu-core-20-64)",
	)
	parser.add_argument(
		"--coverage-dir",
		default="~/Desktop/coverage/full-run",
		help="Coverage directory containing core-set.json and per-system JSON files",
	)
	parser.add_argument(
		"--core-set-file",
		default="core-set.json",
		help="Core-set JSON filename inside coverage-dir",
	)
	parser.add_argument(
		"--output",
		help="Optional output file path. If omitted, prints JSON to stdout.",
	)
	return parser.parse_args()


def read_files_input(files: list[str] | None, files_list: str | None) -> list[str]:
	selected_files: set[str] = set()

	for file_path in files or []:
		if file_path.strip():
			selected_files.add(file_path.strip())

	if files_list:
		for line in Path(files_list).expanduser().read_text().splitlines():
			stripped = line.strip()
			if stripped:
				selected_files.add(stripped)

	if not selected_files:
		raise ValueError("at least one input file is required via --files or --files-list")

	return sorted(selected_files)


def normalize_test_name(test_name: str, system: str) -> str:
	prefix = f"{system}:"
	if test_name.startswith(prefix):
		return test_name[len(prefix):]
	return test_name


def resolve_system_coverage_file(coverage_dir: Path, system: str) -> Path:
	candidates = [coverage_dir / f"{system}.json"]
	if ":" in system:
		short_name = system.split(":", 1)[1]
		candidates.append(coverage_dir / f"{short_name}.json")

	for candidate in candidates:
		if candidate.exists():
			return candidate

	formatted_candidates = ", ".join(str(path) for path in candidates)
	raise FileNotFoundError(
		f"cannot find coverage file for system {system}; checked: {formatted_candidates}"
	)


def get_relevant_tests_for_files(
	system_coverage: dict[str, list[dict]], files: list[str], system: str
) -> set[str]:
	relevant_tests: set[str] = set()

	for file_path in files:
		for test_data in system_coverage.get(file_path, []):
			if isinstance(test_data, dict) and isinstance(test_data.get("test"), str):
				relevant_tests.add(normalize_test_name(test_data["test"], system))

	return relevant_tests


def main() -> None:
	args = parse_args()
	selected_files = read_files_input(args.files, args.files_list)

	coverage_dir = Path(args.coverage_dir).expanduser()
	core_set_path = coverage_dir / args.core_set_file

	if not core_set_path.exists():
		raise FileNotFoundError(f"cannot find core set file: {core_set_path}")

	core_set = json.loads(core_set_path.read_text())
	result: dict[str, list[str]] = {}

	for system in args.systems:
		if system not in core_set:
			raise KeyError(f"system not present in core set: {system}")

		system_coverage_path = resolve_system_coverage_file(coverage_dir, system)
		system_coverage = json.loads(system_coverage_path.read_text())

		if not isinstance(system_coverage, dict):
			raise RuntimeError(
				f"unexpected coverage shape in {system_coverage_path}; expected object"
			)

		core_tests = set(core_set[system])
		relevant_tests = get_relevant_tests_for_files(system_coverage, selected_files, system)
		result[system] = sorted(core_tests | relevant_tests)

	output = json.dumps(result, indent=2, sort_keys=True)
	if args.output:
		Path(args.output).expanduser().write_text(output + "\n")
	else:
		print(output)


if __name__ == "__main__":
	try:
		main()
	except Exception as exc:
		print(str(exc), file=sys.stderr)
		sys.exit(1)

