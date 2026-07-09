#!/usr/bin/env python3

import argparse
import glob
import json
import sys
from collections import defaultdict


def load_items(report_path):
	with open(report_path, encoding="utf-8") as report_file:
		report = json.load(report_file)
	return report.get("items", [])


def full_name(item):
	name = item.get("name", "")
	variant = item.get("variant") or ""
	if variant:
		return f"{name}:{variant}"
	return name


def display_name(item):
	backend = item.get("backend") or ""
	system = item.get("system") or ""
	test_name = full_name(item)
	if backend:
		return f"{backend}:{system}:{test_name}"
	return f"{system}:{test_name}"


def is_failed(item, verb):
	return item.get("verb") == verb and item.get("success") is False


def is_predictor_candidate(item):
	return (
		item.get("success") is False
		and item.get("skipped") is not True
		and bool(item.get("name") or "")
		and bool(item.get("verb") or "")
		and item.get("verb") != "checking"
		and bool(item.get("system") or "")
		and item.get("start") is not None
		and item.get("end") is not None
	)


def consolidate(args):
	items = []
	for pattern in args.patterns:
		for path in sorted(glob.glob(pattern)):
			with open(path, encoding="utf-8") as report_file:
				report = json.load(report_file)
			items.extend(report.get("items", []))

	with open(args.output, "w", encoding="utf-8") as output_file:
		json.dump({"items": items}, output_file)
		output_file.write("\n")


def failures(args):
	for item in load_items(args.report):
		if is_failed(item, args.verb):
			print(display_name(item))


def has_predictor_rows(args):
	for item in load_items(args.report):
		if is_predictor_candidate(item):
			return 0
	return 1


def predictor_rows(args):
	grouped_items = defaultdict(list)
	for item in load_items(args.report):
		if not is_predictor_candidate(item) or item.get("verb") != args.verb:
			continue

		key = (
			item.get("backend") or "",
			item.get("system") or "",
			full_name(item),
			item.get("scenario") or "generic",
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


def success_probability(_args):
	response = json.load(sys.stdin)
	probability = response.get("success_probability")
	if probability is not None:
		print(probability)


def parse_args():
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


def main():
	args = parse_args()
	return args.func(args)


if __name__ == "__main__":
	sys.exit(main())
