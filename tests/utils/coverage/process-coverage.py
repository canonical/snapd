#!/usr/bin/env python3

import argparse
import json
import os
import subprocess
import sys
import tempfile


def test_passed(results_json: dict, backend: str, system: str, test_name: str) -> bool:
    results = [result for result in results_json["items"] if result["backend"] == backend and result["system"] == system and result["name"] == test_name and result["verb"] != "checking"]
    return all(result["success"] == True for result in results)


def main():
    parser = argparse.ArgumentParser(description="Process spread coverage data")
    parser.add_argument("--coverage-dir", required=True, help="Directory containing per-test coverage.json files")
    args = parser.parse_args()

    dirs = os.listdir(args.coverage_dir)

    execution_dict = {}

    for dir in dirs:
        file = os.path.join(args.coverage_dir, dir, "coverage.json")
        if not os.path.isfile(file):
            coverage_data_dir = os.path.join(args.coverage_dir, dir)
            fd, tmp_file = tempfile.mkstemp(prefix="coverage-", suffix=".json", dir=coverage_data_dir)
            os.close(fd)
            with open(tmp_file, 'w', encoding='utf-8') as out_f:
                result = subprocess.run(
                    ["./tests/utils/coverage/main.py", "-results-dir", coverage_data_dir, "-output", "functions"],
                    stdout=out_f,
                    stderr=subprocess.PIPE,
                    text=True
                )
            if result.returncode != 0:
                print(f"ERROR: could not generate {file}, skipping due to {result.stderr}", file=sys.stderr)
                try:
                    os.unlink(tmp_file)
                except OSError:
                    pass
                continue
            os.replace(tmp_file, file)
        with open(file) as f:
            try:
                data = json.load(f)
                for entry in data["files"]:
                    if entry["path"] not in execution_dict:
                        execution_dict[entry["path"]] = []
                    execution_dict[entry["path"]].append({"test": dir.replace("--", "/"), "functions": entry["covered_functions"]})
                    
            except:
                print(f"ERROR: could not load {file}, skipping", file=sys.stderr)
                continue

    json.dump(execution_dict, sys.stdout)


if __name__ == "__main__":
    main()


