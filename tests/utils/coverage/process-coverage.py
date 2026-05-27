#!/usr/bin/env python3

import argparse
import json
import os
import sys


def test_passed(results_json: dict, backend: str, system: str, test_name: str) -> bool:
    results = [result for result in results_json["items"] if result["backend"] == backend and result["system"] == system and result["name"] == test_name]
    return all(result["success"] == True for result in results)


def main():
    parser = argparse.ArgumentParser(description="Process spread coverage data")
    parser.add_argument("--coverage-dir", required=True, help="Directory containing per-test coverage.json files")
    parser.add_argument("--results-path", required=True, help="Path to the spread results.json file")
    parser.add_argument("--output-dir", required=True, help="Directory to write execution.json and execution-w-init.json")
    args = parser.parse_args()

    with open(args.results_path) as f:
        results = json.load(f)

    dirs = os.listdir(args.coverage_dir)

    execution_dict = {}
    execution_dict_with_init = {}

    for dir in dirs:
        file = os.path.join(args.coverage_dir, dir, "coverage.json")
        if os.path.isfile(file):
            split = dir.split(":")
            passed = test_passed(results, split[0], split[1], split[2].replace("--", "/"))
            if not passed:
                print(f"WARNING: test {dir} did not pass, skipping", file=sys.stderr)
                continue
            with open(file) as f:
                try:
                    data = json.load(f)
                    for entry in data["files"]:
                        functions = entry["covered_functions"]
                        if len(functions) == 1 and functions[0] == "init":
                            if entry["path"] not in execution_dict_with_init:
                                execution_dict_with_init[entry["path"]] = []
                            execution_dict_with_init[entry["path"]].append(dir.replace("--", "/"))
                            continue
                        if entry["path"] not in execution_dict:
                            execution_dict[entry["path"]] = []
                        execution_dict[entry["path"]].append(dir.replace("--", "/"))
                        if entry["path"] not in execution_dict_with_init:
                            execution_dict_with_init[entry["path"]] = []
                        execution_dict_with_init[entry["path"]].append(dir.replace("--", "/"))
                except:
                    print(f"ERROR: could not load {file}, skipping", file=sys.stderr)
                    continue

    os.makedirs(args.output_dir, exist_ok=True)

    with open(os.path.join(args.output_dir, "execution.json"), mode="w") as f:
        json.dump(execution_dict, f)

    with open(os.path.join(args.output_dir, "execution-w-init.json"), mode="w") as f:
        json.dump(execution_dict_with_init, f)


if __name__ == "__main__":
    main()


