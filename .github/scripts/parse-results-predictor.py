#!/usr/bin/env python3

from __future__ import annotations

import argparse
import glob
import json
import sys
from collections import defaultdict
from typing import Callable, TypeAlias, cast


JsonValue: TypeAlias = None | bool | int | float | str | list["JsonValue"] | dict[str, "JsonValue"]
JsonObject: TypeAlias = dict[str, JsonValue]
PredictorKey: TypeAlias = tuple[str, str, str, str]


def load_json_object(path: str) -> JsonObject:
	with open(path, encoding="utf-8") as json_file:
		data = json.load(json_file)
	if not isinstance(data, dict):
		raise ValueError(f"Expected {path} to contain a JSON object")
	return cast(JsonObject, data)


def string_value(value: JsonValue, default: str = "") -> str:
	if isinstance(value, str):
		return value
	return default


def load_items(report_path: str) -> list[JsonObject]:
	report = load_json_object(report_path)
	items = report.get("items", [])
	if not isinstance(items, list):
		return []
	return [item for item in items if isinstance(item, dict)]


def full_name(item: JsonObject) -> str:
	name = string_value(item.get("name", ""))
	variant = string_value(item.get("variant", ""))
	if variant:
		return f"{name}:{variant}"
	return name


def display_name(item: JsonObject) -> str:
	backend = string_value(item.get("backend", ""))
	system = string_value(item.get("system", ""))
	test_name = full_name(item)
	if backend:
		return f"{backend}:{system}:{test_name}"
	return f"{system}:{test_name}"


def is_failed(item: JsonObject, verb: str) -> bool:
	return item.get("verb") == verb and item.get("success") is False


def is_predictor_candidate(item: JsonObject) -> bool:
	return (
		item.get("success") is False
		and item.get("skipped") is not True
		and bool(string_value(item.get("name", "")))
		and bool(string_value(item.get("verb", "")))
		and item.get("verb") != "checking"
		and bool(string_value(item.get("system", "")))
		and item.get("start") is not None
		and item.get("end") is not None
	)


def consolidate(args: argparse.Namespace) -> int:
	items: list[JsonObject] = []
	for pattern in args.patterns:
		for path in sorted(glob.glob(pattern)):
			report = load_json_object(path)
			report_items = report.get("items", [])
			if isinstance(report_items, list):
				items.extend(item for item in report_items if isinstance(item, dict))

	with open(args.output, "w", encoding="utf-8") as output_file:
		json.dump({"items": items}, output_file)
		output_file.write("\n")
	return 0


def failures(args: argparse.Namespace) -> int:
	for item in load_items(args.report):
		if is_failed(item, args.verb):
			print(display_name(item))
	return 0


def has_predictor_rows(args: argparse.Namespace) -> int:
	for item in load_items(args.report):
		if is_predictor_candidate(item):
			return 0
	return 1


def predictor_rows(args: argparse.Namespace) -> int:
	grouped_items: defaultdict[PredictorKey, list[JsonObject]] = defaultdict(list)
	for item in load_items(args.report):
		if not is_predictor_candidate(item) or item.get("verb") != args.verb:
			continue

		key = (
			string_value(item.get("backend", "")),
			string_value(item.get("system", "")),
			full_name(item),
			string_value(item.get("scenario", "generic"), "generic"),
		)
		grouped_items[key].append(item)

	for key in sorted(grouped_items):
		backend, system, test_name, scenario = key
		retries = len(grouped_items[key]) - 1
		if backend:
			test_display_name = f"{backend}:{system}:{test_name}"
		else:
			test_display_name = f"{system}:{test_name}"
		print("\t".join([test_display_name, str(retries), test_name, system, scenario]))
	return 0


def success_probability(_args: argparse.Namespace) -> int:
	response = json.load(sys.stdin)
	if not isinstance(response, dict):
		return 0
	probability = response.get("success_probability")
	if probability is not None:
		print(probability)
	return 0


def parse_args() -> argparse.Namespace:
	parser = argparse.ArgumentParser(description="Parse spread result reports.")
	subparsers = parser.add_subparsers(required=True)

	consolidate_parser = subparsers.add_parser("consolidate")
	consolidate_parser.add_argument("output")
	consolidate_parser.add_argument("patterns", nargs="+")
	consolidate_parser.set_defaults(func=consolidate)

	failures_parser = subparsers.add_parser("failures")
	failures_parser.add_argument("report")
	failures_parser.add_argument("verb")
	failures_parser.set_defaults(func=failures)

	has_predictor_rows_parser = subparsers.add_parser("has-predictor-rows")
	has_predictor_rows_parser.add_argument("report")
	has_predictor_rows_parser.set_defaults(func=has_predictor_rows)

	predictor_rows_parser = subparsers.add_parser("predictor-rows")
	predictor_rows_parser.add_argument("report")
	predictor_rows_parser.add_argument("verb")
	predictor_rows_parser.set_defaults(func=predictor_rows)

	success_probability_parser = subparsers.add_parser("success-probability")
	success_probability_parser.set_defaults(func=success_probability)

	return parser.parse_args()


def main() -> int:
	args = parse_args()
	func = cast(Callable[[argparse.Namespace], int], args.func)
	return func(args)


if __name__ == "__main__":
	sys.exit(main())
