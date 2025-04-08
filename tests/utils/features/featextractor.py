#!/usr/bin/env python3

import argparse
from collections import defaultdict
import json
from typing import Any, TextIO
import sys

from features import *
from state import State


def _check_msg(json_entry: dict[str, Any], msg: str) -> bool:
    return 'msg' in json_entry and json_entry['msg'] == msg


class CmdFeature:
    name = 'cmd'
    parent = 'cmds'
    msg = 'command-execution'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        try:
            feature_dict[CmdFeature.parent].append(
                Cmd(cmd=json_entry[CmdLogLine.cmd]))
        except KeyError as e:
            print('cmd entry not found in entry {}: {}'.format(json_entry, e), file=sys.stderr)
        

    @staticmethod
    def cleanup_dict(feature_dict: dict[str, list[Any]]):
        if CmdFeature.parent in feature_dict:
            l = feature_dict[CmdFeature.parent]
            feature_dict[CmdFeature.parent] = [i for n, i in enumerate(l) if i not in l[n + 1:]]


class EndpointFeature:
    name = 'endpoint'
    parent = 'endpoints'
    msg = 'endpoint'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        try:
            if EndpointLogLine.action in json_entry:
                entry = Endpoint(method=json_entry[EndpointLogLine.method],
                                 path=json_entry[EndpointLogLine.path], 
                                 action=json_entry[EndpointLogLine.action])
            else:
                entry = Endpoint(
                    method=json_entry[EndpointLogLine.method], path=json_entry[EndpointLogLine.path])
            feature_dict[EndpointFeature.parent].append(entry)
        except KeyError as e:
            print('endpoint entries not found in entry {}: {}'.format(json_entry, e), file=sys.stderr)
        

    @staticmethod
    def cleanup_dict(feature_dict: dict[str, list[Any]]):
        if EndpointFeature.parent in feature_dict:
            l = feature_dict[EndpointFeature.parent]
            feature_dict[EndpointFeature.parent] = [i for n, i in enumerate(l) if i not in l[n + 1:]]


class InterfaceFeature:
    name = 'interface'
    parent = 'interfaces'
    msg = 'interface-connection'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        try:
            feature_dict[InterfaceFeature.parent].append(Interface(
                name=json_entry[InterfaceLogLine.interface], 
                plug_snap_type=json_entry[InterfaceLogLine.plug], 
                slot_snap_type=json_entry[InterfaceLogLine.slot]))
        except KeyError as e:
            print('interface entries not found in entry {}: {}'.format(json_entry, e), file=sys.stderr)
        

    @staticmethod
    def cleanup_dict(feature_dict: dict[str, list[Any]]):
        if InterfaceFeature.parent in feature_dict:
            l = feature_dict[InterfaceFeature.parent]
            feature_dict[InterfaceFeature.parent] = [i for n, i in enumerate(l) if i not in l[n + 1:]]


class EnsureFeature:
    name = 'ensure'
    parent = 'ensures'
    msg = 'ensure'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        try:
            if EnsureLogLine.func in json_entry:
                for ensure_list in reversed(feature_dict[EnsureFeature.parent]):
                    if ensure_list['manager'] == json_entry[EnsureLogLine.manager]:
                        ensure_list['functions'].append(json_entry[EnsureLogLine.func])
                        break
            else:
                feature_dict[EnsureFeature.parent].append(Ensure(manager=json_entry[EnsureLogLine.manager], functions=[]))

        except KeyError as e:
            print('ensure entries not found in entry {}: {}'.format(json_entry, e), file=sys.stderr)


    @staticmethod
    def cleanup_dict(feature_dict: dict[str, list[Any]]):
        l = feature_dict[EnsureFeature.parent]
        feature_dict[EnsureFeature.parent] = [i for n, i in enumerate(l) if i not in l[n + 1:]]


class ChangeFeature:
    name = 'change'
    parent = 'changes'
    msg = 'new-change'
    

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], state: State):
        try:
            snap_types = list(state.get_snap_types_from_change_id(json_entry[ChangeLogLine.id]))
            feature_dict[ChangeFeature.parent].append(Change(kind=json_entry[ChangeLogLine.kind], snap_types=snap_types))
        except KeyError as e:
            print("Encountered error during change feature processing for change {}: {}".format(json_entry, e), file=sys.stderr)
        
    @staticmethod
    def cleanup_dict(feature_dict: dict[str, list[Any]]):
        l = feature_dict[ChangeFeature.parent]
        feature_dict[ChangeFeature.parent] = [i for n, i in enumerate(l) if i not in l[n + 1:]]


class TaskFeature:
    name = 'task'
    parent = 'tasks'
    msg = 'task-status-change'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], state: State):
        for entry in feature_dict[TaskFeature.parent]:
            if json_entry[TaskLogLine.id] == entry["id"]:
                entry['last_status'] = json_entry[TaskLogLine.status]
                return
        try:
            snap_types = list(state.get_snap_types_from_task_id(json_entry[TaskLogLine.id]))
            feature_dict[TaskFeature.parent].append(
                Task(id=json_entry[TaskLogLine.id], 
                     kind=json_entry[TaskLogLine.task_name], 
                     last_status=json_entry[TaskLogLine.status], 
                     snap_types=snap_types))
        except KeyError as e:
            print("Encountered error during task feature processing for task {}: {}".format(json_entry, e), file=sys.stderr)
        
    @staticmethod
    def cleanup_dict(feature_dict: dict[str, list[Any]]):
        if TaskFeature.parent in feature_dict:
            for entry in feature_dict[TaskFeature.parent]:
                del entry['id']
            l = feature_dict[TaskFeature.parent]
            feature_dict[TaskFeature.parent] = [i for n, i in enumerate(l) if i not in l[n + 1:]]


FEATURE_LIST = [CmdFeature, EndpointFeature, InterfaceFeature,
                EnsureFeature, ChangeFeature, TaskFeature]


def get_feature_dictionary(log_file: TextIO, feature_list: list[str], state: State):
    '''
    Extracts features from the journal entries and places them in a dictionary.

    :param log_file: iterator of journal entries
    :param feature_list: list of feature names to extract
    :param state: dictionary of a state.json
    :return: dictionary of features
    :raises: ValueError if an invalid feature name is provided
    :raises: RuntimeError if a line could not be parsed as json
    '''

    feature_dict = defaultdict(list)
    feature_classes = [cls for cls in FEATURE_LIST
                       if cls.name in feature_list]
    if len(feature_classes) != len(feature_list):
        raise ValueError(
            "Error: Invalid feature name in feature list {}".format(feature_list))

    for line in log_file:
        try:
            line_json = json.loads(line)
            for feature_class in feature_classes:
                if _check_msg(line_json, feature_class.msg):
                    feature_class.handle_feature(
                        feature_dict, line_json, state)
        except json.JSONDecodeError:
            raise RuntimeError("Could not parse line as json: {}".format(line))
        
    for feature_class in feature_classes:
        feature_class.cleanup_dict(feature_dict)

    return feature_dict


def main():
    parser = argparse.ArgumentParser(
        description="""Given a set of features with journal entries, each in json format, and a 
        state.json, this script will search the text file and extract the features. Those 
        features will be saved in a dictionary and written to the indicated file in output.""")
    parser.add_argument('-o', '--output', help='Output file', required=True)
    parser.add_argument(
        '-f', '--feature', help='Features to extract from journal {cmd, task, change, ensure, endpoint, interface}; '
        'can be repeated multiple times', nargs='+')
    parser.add_argument(
        '-j', '--journal', help='Text file containing journal entries', required=True, type=argparse.FileType('r'))
    parser.add_argument(
        '-s', '--state', help='state.json', required=True, type=argparse.FileType('r'))
    args = parser.parse_args()

    try:
        state = State(json.load(args.state))
        feature_dictionary = get_feature_dictionary(
            args.journal, args.feature, state)
        json.dump(feature_dictionary, open(args.output, "w"))
    except json.JSONDecodeError:
        raise RuntimeError("The state.json is not valid json")


if __name__ == "__main__":
    main()
