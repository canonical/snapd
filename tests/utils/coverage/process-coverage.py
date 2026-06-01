#!/usr/bin/env python3

import argparse
import json
import os
import subprocess
import sys


def test_passed(results_json: dict, backend: str, system: str, test_name: str) -> bool:
    results = [result for result in results_json["items"] if result["backend"] == backend and result["system"] == system and result["name"] == test_name and result["verb"] != "checking"]
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

    for dir in dirs:
        file = os.path.join(args.coverage_dir, dir, "coverage.json")
        if not os.path.isfile(file):
            coverage_data_dir = os.path.join(args.coverage_dir, dir)
            result = subprocess.run(
                ["./tests/utils/coverage/main.py", "-results-dir", coverage_data_dir, "-output", "functions"],
                # ["go", "run", "./tests/utils/coverage", "-results-dir", coverage_data_dir, "-output", "functions"],
                stdout=open(file, 'w'),
                stderr=subprocess.PIPE,
                text=True
            )
            if result.returncode != 0:
                print(f"ERROR: could not generate {file}, skipping due to {result.stderr}", file=sys.stderr)
                continue
        split = dir.split(":")
        passed = test_passed(results, split[0], split[1], split[2].replace("--", "/"))
        with open(file) as f:
            try:
                data = json.load(f)
                for entry in data["files"]:
                    if entry["path"] not in execution_dict:
                        execution_dict[entry["path"]] = []
                    execution_dict[entry["path"]].append({"test": dir.replace("--", "/"), "passed": passed, "functions": entry["covered_functions"]})
                    
            except:
                print(f"ERROR: could not load {file}, skipping", file=sys.stderr)
                continue

    os.makedirs(args.output_dir, exist_ok=True)

    with open(os.path.join(args.output_dir, "execution.json"), mode="w") as f:
        json.dump(execution_dict, f)


if __name__ == "__main__":
    main()


