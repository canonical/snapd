#!/usr/bin/env python3

import argparse
import json
import os
import sys

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
    json.dump(list(failed), sys.stdout)
    
if __name__ == '__main__':
    main()