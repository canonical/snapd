#!/usr/bin/env python3
'''
Adds runtime data from spread-results artifacts to composed feature files.

The spread-results artifacts are named:
  spread-results-<run_id>-<attempt>-<group>
and each contains a results.json with items having start/end timestamps
for each executed task.

For each test in the final composed feature files, this script finds the
runtime from the most recent attempt that ran that test and adds it as a
"runtime" field (in seconds, or null if not found). Runtime is the sum of
task-level preparing + executing + restoring durations.
'''

import argparse
import json
import os
import re
from datetime import datetime
from typing import Optional


def _parse_runtime_seconds(start: str, end: str) -> float:
    '''
    Calculate runtime in seconds from ISO8601 start/end strings.

    :param start: ISO8601 start timestamp string
    :param end: ISO8601 end timestamp string
    :returns: Duration in seconds as a float
    '''
    fmt_start = datetime.fromisoformat(start.replace('Z', '+00:00'))
    fmt_end = datetime.fromisoformat(end.replace('Z', '+00:00'))
    return (fmt_end - fmt_start).total_seconds()


def _parse_attempt_from_dirname(dirname: str) -> Optional[int]:
    '''
    Parse the attempt number from a directory name of the form:
      spread-results-<run_id>-<attempt>-<group>
    where run_id and attempt are a sequence of digits

    :param dirname: the directory name to parse
    :returns: attempt number as an int, or None if the name does not match
    '''
    m = re.match(r'^spread-results-\d+-(\d+)-', dirname)
    if m:
        return int(m.group(1))
    return None


def build_runtime_lookup(results_dir: str) -> dict[tuple, float]:
    '''
    Walks results_dir, reads each results.json in subdirectories named like
    spread-results-<run_id>-<attempt>-<group>, and builds a lookup:
      (system, full_test_name, variant) -> runtime_seconds

    When the same test appears in multiple attempts, only the latest attempt
    is kept.

    :param results_dir: directory containing spread-results artifact subdirectories
    :returns: dict mapping (system, test_name, variant) to runtime in seconds
    '''
    # key -> (attempt, runtime_seconds)
    best: dict[tuple, tuple[int, float]] = {}
    phases = {'preparing', 'executing', 'restoring'}

    for entry in os.scandir(results_dir):
        if not entry.is_dir():
            continue
        attempt = _parse_attempt_from_dirname(entry.name)
        if attempt is None:
            continue
        results_file = os.path.join(entry.path, 'results.json')
        if not os.path.isfile(results_file):
            continue

        with open(results_file, 'r', encoding='utf-8') as f:
            data = json.load(f)

        per_attempt_totals: dict[tuple, float] = {}
        for item in data.get('items', []):
            if item.get('level') != 'task' or item.get('verb') not in phases:
                continue
            try:
                system = '{}:{}'.format(item['backend'], item['system'])
                name = item['name']
                variant = item.get('variant') or ''
                runtime = _parse_runtime_seconds(item['start'], item['end'])
            except (KeyError, ValueError):
                continue

            key = (system, name, variant)
            per_attempt_totals[key] = per_attempt_totals.get(key, 0.0) + runtime

        for key, runtime in per_attempt_totals.items():
            if key not in best or attempt > best[key][0]:
                best[key] = (attempt, runtime)

    return {k: v[1] for k, v in best.items()}


def add_runtime_to_features(features_dir: str, runtime_lookup: dict[tuple, float]) -> None:
    '''
    Reads each system feature JSON file in features_dir, adds a "runtime"
    field (in seconds) to each test from the lookup, and writes the file back.
    Tests with no matching entry in the lookup get runtime set to null.

    :param features_dir: directory containing composed feature JSON files
    :param runtime_lookup: dict from build_runtime_lookup
    '''
    for filename in os.listdir(features_dir):
        if not filename.endswith('.json'):
            continue
        filepath = os.path.join(features_dir, filename)
        with open(filepath, 'r', encoding='utf-8') as f:
            system_data = json.load(f)

        system = system_data.get('system', '')
        for test in system_data.get('tests', []):
            suite = test.get('suite', '')
            task_name = test.get('task_name', '')
            variant = test.get('variant') or ''
            full_name = '{}/{}'.format(suite, task_name) if suite else task_name
            key = (system, full_name, variant)
            test['runtime'] = runtime_lookup.get(key)

        with open(filepath, 'w', encoding='utf-8') as f:
            json.dump(system_data, f)


def main():
    parser = argparse.ArgumentParser(
        description='''Adds runtime data from spread-results artifacts to composed feature files.
        
        Reads spread-results artifact directories (named spread-results-<run_id>-<attempt>-<group>)
        from results-dir and uses them to populate the "runtime" field on each test entry in the
        composed feature JSON files in features-dir. The most recent attempt wins when a test
        appears in multiple attempts.''')
    parser.add_argument('--results-dir', required=True,
                        help='Directory containing spread-results artifact subdirectories')
    parser.add_argument('--features-dir', required=True,
                        help='Directory containing composed feature JSON files to update')
    args = parser.parse_args()

    runtime_lookup = build_runtime_lookup(args.results_dir)
    add_runtime_to_features(args.features_dir, runtime_lookup)


if __name__ == '__main__':
    main()
