#!/usr/bin/env python3

import argparse
import json
import os
import sys

def test_passed(results_json: dict, backend: str, system: str, test_name: str) -> bool:
    results = [result for result in results_json["items"] if result["backend"] == backend and result["system"] == system and result["name"] == test_name and result["verb"] != "checking"]
    return all(result["success"] == True for result in results)

def main():
    parser = argparse.ArgumentParser(description="Process spread coverage data")
    parser.add_argument("--results-dir", required=True, help="Path to the spread results.json dirs")
    args = parser.parse_args()
    dirs = os.listdir(args.results_dir)
    failed = set()
    for dir in dirs:
        with open(f"{args.results_dir}/{dir}/results.json") as f:
            results = json.load(f)
        failed.update({f"{result['backend']}:{result['system']}:{result['name']}" for result in results['items'] if not result['success'] and result['verb'] != 'checking' and result['level'] == 'task' and not result['skipped']})
        # failed.update({f"{result['backend']}:{result['system']}:{result['name']}" for result in results['items'] if not result['success'] and result['verb'] != 'checking' and result['level'] == 'task' and not result['skipped'] and not result['aborted']})
    json.dump(list(failed), sys.stdout)
    
if __name__ == '__main__':
    main()