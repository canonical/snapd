#!/usr/bin/env python3

import argparse
from collections import defaultdict
import json
from typing import Any, TextIO
import sys

from features import Cmd, Endpoint, Interface, Task, Change, Ensure, CmdLogLine, EndpointLogLine, InterfaceLogLine, EnsureLogLine, ChangeLogLine, TaskLogLine
from state import State, NotInStateError


def _check_msg(json_entry: dict[str, Any], msg: str) -> bool:
    return 'msg' in json_entry and json_entry['msg'] == msg


def _remove_duplicate_features(key: str, dictionary: dict[str, Any]):
    if key in dictionary:
        l = dictionary[key]
        dictionary[key] = [i for n, i in enumerate(l) if i not in l[n + 1:]]


class CmdFeature:
    name = 'cmd'
    parent = 'cmds'
    msg = 'command-execution'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        feature_dict[CmdFeature.parent].append(
            Cmd(cmd=json_entry[CmdLogLine.cmd]))

    @staticmethod
    def cleanup_dict(_):
        pass


class EndpointFeature:
    name = 'endpoint'
    parent = 'endpoints'
    msg = 'endpoint'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        if EndpointLogLine.action in json_entry:
            entry = Endpoint(method=json_entry[EndpointLogLine.method],
                             path=json_entry[EndpointLogLine.path],
                             action=json_entry[EndpointLogLine.action])
        else:
            entry = Endpoint(
                method=json_entry[EndpointLogLine.method], path=json_entry[EndpointLogLine.path])
        feature_dict[EndpointFeature.parent].append(entry)

    @staticmethod
    def cleanup_dict(_):
        pass


class InterfaceFeature:
    name = 'interface'
    parent = 'interfaces'
    msg = 'interface-connection'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        feature_dict[InterfaceFeature.parent].append(Interface(
            name=json_entry[InterfaceLogLine.interface],
            plug_snap_type=json_entry[InterfaceLogLine.plug],
            slot_snap_type=json_entry[InterfaceLogLine.slot]))

    @staticmethod
    def cleanup_dict(_):
        pass


class EnsureFeature:
    name = 'ensure'
    parent = 'ensures'
    msg = 'ensure'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], _):
        if EnsureLogLine.func in json_entry:
            for ensure_list in reversed(feature_dict[EnsureFeature.parent]):
                if ensure_list['manager'] == json_entry[EnsureLogLine.manager]:
                    ensure_list['functions'].append(
                        json_entry[EnsureLogLine.func])
                    break
        else:
            feature_dict[EnsureFeature.parent].append(
                Ensure(manager=json_entry[EnsureLogLine.manager], functions=[]))

    @staticmethod
    def cleanup_dict(_):
        pass


class ChangeFeature:
    name = 'change'
    parent = 'changes'
    msg = 'new-change'

    @staticmethod
    def handle_feature(feature_dict: dict[str, list[Any]], json_entry: dict[str, Any], state: State):
        snap_types = ['NOT FOUND']
        if state:
            try:
                snap_types = list(state.get_snap_types_from_change_id(
                    json_entry[ChangeLogLine.id]))
            except NotInStateError:
                pass
        feature_dict[ChangeFeature.parent].append(
            Change(kind=json_entry[ChangeLogLine.kind], snap_types=snap_types))

    @staticmethod
    def cleanup_dict(_):
        pass


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
        snap_types = ['NOT FOUND']
        if state:
            try:
                snap_types = state.get_snap_types_from_task_id(
                    json_entry[TaskLogLine.id])
            except NotInStateError:
                pass
        feature_dict[TaskFeature.parent].append(
            Task(id=json_entry[TaskLogLine.id],
                 kind=json_entry[TaskLogLine.task_name],
                 last_status=json_entry[TaskLogLine.status],
                 snap_types=snap_types))

    @staticmethod
    def cleanup_dict(feature_dict: dict[str, list[Any]]):
        if TaskFeature.parent in feature_dict:
            for entry in feature_dict[TaskFeature.parent]:
                del entry['id']


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
                    try:
                        feature_class.handle_feature(
                            feature_dict, line_json, state)
                    except Exception as e:
                        raise RuntimeError("Encountered error during {} feature processing for {}: {}".format(
                            feature_class.name, line_json, e))
        except json.JSONDecodeError:
            raise RuntimeError("Could not parse line as json: {}".format(line))

    for feature_class in feature_classes:
        feature_class.cleanup_dict(feature_dict)
        _remove_duplicate_features(feature_class.parent, feature_dict)

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
        '-s', '--state', help='state.json', required=False, type=argparse.FileType('r'))
    args = parser.parse_args()

    try:
        state = None
        if args.state:
            state = State(json.load(args.state))
        feature_dictionary = get_feature_dictionary(
            args.journal, args.feature, state)
        json.dump(feature_dictionary, open(args.output, "w"))
    except json.JSONDecodeError:
        raise RuntimeError("The state.json is not valid json")


if __name__ == "__main__":
    main()
