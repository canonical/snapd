#!/usr/bin/env python3

import argparse
import json
import os
import sys

sys.path.append(os.path.dirname(os.path.abspath(__file__)))

from features import Change, Cmd, Endpoint, Interface, Status, Task


def translate_all_features(all_features_json: str, output_json: str) -> dict:
    all_features=None
    with open(all_features_json, 'r', encoding='utf-8') as f:
        all_features = json.load(f)

    if any(key for key in ["endpoints", "tasks", "commands", "ensures", "interfaces", "changes"] if key not in all_features):
        raise RuntimeError(f"missing key in all features file {all_features_json}")

    all_endpoints = []
    for endpoint in all_features['endpoints']:
        if 'actions' in endpoint and endpoint['actions']:
            for action in endpoint['actions']:
                all_endpoints.append(Endpoint(method=endpoint['method'], path=endpoint['path'], action=action))
        else:
            all_endpoints.append(Endpoint(method=endpoint['method'], path=endpoint['path']))
    all_cmds = [Cmd(cmd=cmd) for cmd in all_features['commands']]
    all_tasks = []
    for task in all_features['tasks']:
        status_list = [Status.done, Status.undone, Status.error] if 'has-undo' in task and task['has-undo'] else [Status.done, Status.error]
        for status in status_list:
            all_tasks.append(Task(kind=task['kind'], last_status=status))
    all_changes = [Change(kind=change) for change in all_features['changes']]
    all_interfaces = [Interface(name=iface) for iface in all_features['interfaces']]

    with open(output_json, 'w', encoding='utf-8') as f:
        json.dump({
            'cmds': all_cmds,
            'ensures': all_features['ensures'],
            'tasks': all_tasks,
            'changes': all_changes,
            'endpoints': all_endpoints,
            'interfaces': all_interfaces,
        }, f)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Translates feature tagging output from 'snap debug features' to the same format as feature tagging reports")
    parser.add_argument(
        '-f', '--file', help='snap debug features output file', required=True, type=str)
    parser.add_argument(
        '-o', '--output', help='where to write translated output', required=True, type=str)
    args = parser.parse_args()
    translate_all_features(args.file, args.output)