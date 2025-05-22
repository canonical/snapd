#!/usr/bin/env python3

from abc import abstractmethod, ABC
import argparse
from collections import defaultdict
import concurrent.futures
import datetime
import json
import os
import pymongo
import pymongo.collection
import sys
from typing import Any, Iterable

from features import SystemFeatures


FEATURES = ['cmds', 'endpoints', 'ensures', 'tasks', 'changes', 'interfaces']


class TaskId:
    def __init__(self, suite, task_name):
        self.suite = suite
        self.task_name = task_name

    def __eq__(self, value):
        if not isinstance(value, TaskId):
            return False
        return self.suite == value.suite and self.task_name == value.task_name

    def __hash__(self):
        return hash((self.suite, self.task_name))

    def __repr__(self):
        return self.suite + ":" + self.task_name

    def __str__(self):
        return self.suite + ":" + self.task_name


class TaskIdVariant(TaskId):
    def __init__(self, suite, task_name, variant):
        super().__init__(suite, task_name)
        self.variant = variant

    def __eq__(self, value):
        if not isinstance(value, TaskId):
            return False
        if isinstance(value, TaskIdVariant):
            return self.suite == value.suite and self.task_name == value.task_name and self.variant == value.variant
        else:
            return self.suite == value.suite and self.task_name == value.task_name

    def __hash__(self):
        return hash((self.suite, self.task_name, self.variant))

    def __repr__(self):
        return self.suite + ":" + self.task_name + ":" + self.variant

    def __str__(self):
        return self.suite + ":" + self.task_name + ":" + self.variant


class DateTimeEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, datetime.datetime):
            return obj.isoformat()


class Retriever(ABC):
    '''
    Retrieves features tags from a data source.
    '''
    @classmethod
    @abstractmethod
    def get_sorted_timestamps_and_systems(self) -> list[dict[str, Any]]:
        '''
        Gets the complete list of all timestamps and the systems run under each timestamp.
        Format: [{"timestamp":\<timestamp\>,"systems":[\<system1\>,...,\<systemN\>]}]
        '''

    @classmethod
    @abstractmethod
    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        '''
        Given the timestamp and system, gets a single dictionary entry.

        :raises RuntimeError: when there is not exactly one entry for the system at the timestamp
        '''

    @classmethod
    @abstractmethod
    def get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        '''
        Retrieves dictionary entries for all indicated systems at 
        the given timestamp. If systems is None, then it retrieves 
        all dictionary entries for all systems at the given timestamp.
        '''


class MongoRetriever(Retriever):
    '''
    Retrieves feature data from a mongodb instance at snapd.features. 
    The mongodb entries should all be SystemFeatures documents with a 
    timestamp added. Use of this retriever, requires a credentials 
    json file with host, port, user, and password defined.
    '''

    def __init__(self, creds_file):
        config = json.load(creds_file)
        self.client = pymongo.MongoClient(
            host=config['host'], port=config['port'], username=config['user'], password=config['password'])
        self.collection = self.client.snapd.features

    def __del__(self):
        self.client.close()

    def get_sorted_timestamps_and_systems(self) -> list[dict[str, Any]]:
        results = self.collection.find()
        dictionary = defaultdict(list)
        for result in results:
            dictionary[result['timestamp'].isoformat()].append(
                result['system'])
        return [{"timestamp": entry[0], "systems": entry[1]} for entry in sorted(dictionary.items(), reverse=True)]

    def get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        if systems:
            for system in systems:
                system_jsons = self.collection.find(
                    {'timestamp': datetime.datetime.fromisoformat(timestamp), 'system': system})
                for system_json in system_jsons:
                    yield system_json
        else:
            for result in self.collection.find({'timestamp': datetime.datetime.fromisoformat(timestamp)}):
                yield result

    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        json_result = self.collection.find(
            {'timestamp': datetime.datetime.fromisoformat(timestamp), 'system': system}).to_list()
        if len(json_result) != 1:
            raise RuntimeError(f'{len(json_result)} entries of system {system} found in collection {timestamp}')
        return json_result[0]


class DirRetriever(Retriever):
    '''
    Retrieves features tagging data from the filesystem.
    It assumes data is saved in the following structure:
     \<dir\>/\<timestamp\>/\<system\>.json.
    To populate a directory with data from mongo, do the following:

    ./query_features.py export -f /mongo/creds.json -o /write/dir -t \<timestamp1\> .. \<timestampN\> -s \<system1\> .. \<systemN\>

    Then one can use /write/dir as a data source with this retriever
    '''

    def __init__(self, dir: str):
        if not os.path.exists(dir):
            raise RuntimeError(f'directory {dir} does not exist')
        self.dir = dir

    def get_sorted_timestamps_and_systems(self) -> list[dict[str, Any]]:
        dictionary = defaultdict(list)
        for timestamp in os.listdir(self.dir):
            timestamp_path = os.path.join(self.dir, timestamp)
            if not os.path.isdir(timestamp_path):
                continue
            for filename in os.listdir(timestamp_path):
                if filename.endswith('.json'):
                    system = filename.rsplit('.', 1)[0]
                    dictionary[timestamp].append(system)
        return [{"timestamp": entry[0], "systems": entry[1]} for entry in sorted(dictionary.items(), reverse=True)]

    def get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        timestamp_dir = os.path.join(self.dir, timestamp)
        if not os.path.isdir(timestamp_dir):
            raise RuntimeError(
                f'timestamp {timestamp} not present in dir {self.dir}')
        for filename in os.listdir(timestamp_dir):
            if filename.endswith('.json') and (not systems or filename.rsplit('.', 1)[0] in systems):
                with open(os.path.join(timestamp_dir, filename), 'r', encoding='utf-8') as f:
                    yield json.load(f)

    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        sys_path = os.path.join(self.dir, timestamp, system+'.json')
        if not os.path.exists(sys_path):
            raise RuntimeError(f'system file not found {sys_path}')
        with open(sys_path, 'r', encoding='utf-8') as f:
            return json.load(f)


def consolidate_system_features(system_json: SystemFeatures, include_tasks: Iterable[TaskId] = None, exclude_tasks: Iterable[TaskId] = None) -> dict[str, list[dict[str, Any]]]:
    '''
    Given a dictionary of system feature data, consolidates features across
    all tests into one feature data dictionary.

    :param system_json: a SystemFeatures dictionary
    :param include_tasks: if not None, will only consolidate tasks present in this list
    :param exclude_tasks: if not None, will not consolidate tasks present in this list
    :returns: a dictionary with only feature data
    '''
    features = defaultdict(list)
    for test in system_json['tests']:
        if include_tasks is not None and TaskIdVariant(test['suite'], test['task_name'], test['variant']) not in include_tasks:
            continue
        if exclude_tasks is not None and TaskIdVariant(test['suite'], test['task_name'], test['variant']) in exclude_tasks:
            continue

        for feature_name in test.keys():
            if feature_name not in FEATURES:
                continue
            for feature in test[feature_name]:
                if feature not in features[feature_name]:
                    features[feature_name].append(feature)
    return features


def minus(first: dict[str, list], second: dict[str, list]) -> dict:
    '''
    Creates a new dictionary of first \ second calculated on values.

    Ex: 
    first = {"a":["b","c"],"d":["e"]}; second = {"a":["c"], "q":[]}

    first \ second == {"a":["b"],"d":["e"]}
    '''
    minus = {}
    for feature, feature_list in first.items():
        if feature not in second:
            minus[feature] = feature_list
        else:
            m = [item for item in feature_list if item not in second[feature]]
            if m:
                minus[feature] = m
    return minus


def list_tasks(system_json: SystemFeatures, remove_failed: bool) -> set[TaskIdVariant]:
    '''
    Lists all tasks present in the SystemFeatures dictionary

    :param remove_failed: if true, will only include those tasks where success == True
    '''
    tasks = set()
    for test in system_json['tests']:
        if not remove_failed or test['success']:
            tasks.add(
                TaskIdVariant(test['suite'], test['task_name'], test['variant']))
    return tasks


def diff(retriever: Retriever, timestamp1: str, system1: str, timestamp2: str, system2: str, remove_failed: bool, only_same: bool) -> dict:
    '''
    Calculates set(system1_features) \ set(system2_features), each at their
    respective timestamps. 

    :param remove_failed: if true, will remove all instances of tests where success == False
    :param only_same: if true, will only calculate difference between tests run on both systems
    '''
    system1 = retriever.get_single_json(timestamp1, system1)
    system2 = retriever.get_single_json(timestamp2, system2)

    include_tasks1 = None
    include_tasks2 = None
    if remove_failed or only_same:
        include_tasks1 = list_tasks(system1, remove_failed)
        include_tasks2 = list_tasks(system2, remove_failed)
        if only_same:
            include_tasks2 = include_tasks1 = include_tasks1.intersection(
                include_tasks2)

    features1 = consolidate_system_features(
        system1, include_tasks=include_tasks1)
    features2 = consolidate_system_features(
        system2, include_tasks=include_tasks2)
    mns = minus(features1, features2)
    return mns


def check_duplicate(args):
    task, system_json = args
    # Exclude all variants of the task from consolidation so that
    # it isn't flagged as a duplicate when variants of the same
    # task have identical features.
    task_id = TaskId(suite=task['suite'], task_name=task['task_name'])
    features = consolidate_system_features(system_json, exclude_tasks=[task_id])
    to_check = {key: value for key, value in task.items() if key in FEATURES}
    mns = minus(to_check, features)
    if to_check and not mns:
        return TaskIdVariant(suite=task['suite'], task_name=task['task_name'], variant=task['variant'])
    return None


def dup(retriever: Retriever, timestamp: str, system: str, remove_failed: bool) -> list[TaskIdVariant]:
    '''
    Returns tests whose features are completely covered by other tests in that system.

    :param remove_failed: if true, will remove all instances of tests whose success == False
    '''
    system_json = retriever.get_single_json(timestamp, system)

    if remove_failed:
        system_json['tests'] = [
            task for task in system_json['tests'] if task['success']]

    duplicates = []
    with concurrent.futures.ProcessPoolExecutor() as executor:
        results = executor.map(check_duplicate, [(task, system_json) for task in system_json['tests']])
        for result in results:
            if result is not None:
                duplicates.append(result)

    return duplicates


def export(retriever: Retriever, output: str, timestamps: list[str], systems: list[str] = None):
    '''
    Writes the feature data to the output directory in format \<dir\>/\<timestamp\>/\<system\>.json.
    It creates one directory for each supplied timestamp, and will write one json file for each system
    in that timestamp and present in the systems list. If the systems list is None, then it will write
    all systems at each supplied timestamp.
    '''
    for timestamp in timestamps:
        os.makedirs(os.path.join(output, timestamp), exist_ok=True)
        for system_json in retriever.get_systems(timestamp, systems):
            with open(os.path.join(output, timestamp, system_json['system'] + ".json"), 'w', encoding='utf-8') as f:
                json.dump(system_json, f, cls=DateTimeEncoder)


def add_diff_parser(subparsers):
    diff_description = '''
        Calculates feature diff between two systems: set(features_1) \ set(features_2).
        You can specify either a json file with credentials for mongodb or a directory with features output.
        If using a directory, the directory format must be <dir>/<timestamp1>/<system1>.json and 
        <dir>/<timestamp2>/<system2>.json.

        By default, it will compare all features across both systems. If you wish to restrict the comparison
        to only tasks that were successful, use the --remove-failed flag. If you wish to restrict the
        comparison to only tasks that executed on both systems, use the --only-same flag.

        If you wish to create a directory from mongo data, use the export command instead of diff first.
    '''
    cmd = 'diff'
    diff: argparse.ArgumentParser = subparsers.add_parser(cmd, help='calculate diff between system features',
                                 description=diff_description, formatter_class=argparse.RawDescriptionHelpFormatter)
    diff.add_argument('-f', '--file', help='json file containing creds for mongodb', type=argparse.FileType('r', encoding='utf-8'))
    diff.add_argument('-d', '--dir', help='folder containing feature data', type=str)
    diff.add_argument('-t1', '--timestamp1', help='timestamp of first execution', type=str, required=True)
    diff.add_argument('-s1', '--system1', help='system of first execution', type=str, required=True)
    diff.add_argument('-t2', '--timestamp2', help='timestamp of second execution', type=str, required=True)
    diff.add_argument('-s2', '--system2', help='system of second execution', type=str, required=True)
    diff.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    diff.add_argument('--only-same', help='only compare tasks that were executed on both systems', action='store_true')
    return cmd


def add_dup_parser(subparsers):
    dup_description = '''
        For each task present in the indicated system under the indicated timestamp,
        calculates the difference between that task's features and the system's 
        without the task: set(task_features) \ set(system_features without task).
        If the difference is ever empty, then that task is printed to console as
        a duplicate feature.

        To remove all failed tasks from consideration, add the --remove-failed flag.
    '''
    cmd = 'dup'
    duplicate: argparse.ArgumentParser = subparsers.add_parser(cmd,
                                      help='show tasks whose features are completely covered by the rest',
                                      description=dup_description,
                                      formatter_class=argparse.RawDescriptionHelpFormatter)
    duplicate.add_argument('-f', '--file', help='json file containing creds for mongodb', type=argparse.FileType('r', encoding='utf-8'))
    duplicate.add_argument('-d', '--dir', help='folder containing feature data', type=str)
    duplicate.add_argument('-t', '--timestamp', help='timestamp of instance to search', required=True, type=str)
    duplicate.add_argument('-s', '--system', help='system whose features should be searched', required=True, type=str)
    duplicate.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    return cmd


def add_export_parser(subparsers):
    cmd = 'export'
    export: argparse.ArgumentParser = subparsers.add_parser(cmd, help='export data to output local directory',
                                   description='Grabs system json files by timestamps and systems and saves them to the folder indicated in the output arguement.')
    export.add_argument('-f', '--file', help='json file containing creds for mongodb', type=argparse.FileType('r', encoding='utf-8'))
    export.add_argument('-d', '--dir', help='folder containing feature data', type=str)
    export.add_argument('-t', '--timestamps', help='space-separated list of identifying timestamps', required=True, nargs='+')
    export.add_argument('-s', '--systems', help='space-separated list of systems', nargs='*')
    export.add_argument('-o', '--output', help='folder to save feature data', required=True, type=str)
    return cmd


def add_list_parser(subparsers):
    cmd = 'list'
    lst: argparse.ArgumentParser = subparsers.add_parser(cmd, help='lists all timestamps with systems present in data source',
                                description='Lists all timestamps with systems present in data source.')
    lst.add_argument('-f', '--file', help='json file containing creds for mongodb', type=argparse.FileType('r', encoding='utf-8'))
    lst.add_argument('-d', '--dir', help='folder containing feature data', type=str)
    return cmd


def main():
    parser = argparse.ArgumentParser(
        description='cli to query data source containing feature data')
    subparsers = parser.add_subparsers(dest='command')
    subparsers.required = True
    diff_cmd = add_diff_parser(subparsers)
    dup_cmd = add_dup_parser(subparsers)
    export_cmd = add_export_parser(subparsers)
    list_cmd = add_list_parser(subparsers)

    args = parser.parse_args()

    retriever = None
    if args.dir:
        retriever = DirRetriever(args.dir)
    elif args.file:
        retriever = MongoRetriever(args.file)
    else:
        raise RuntimeError(
            'you must specify either a mongodb credential file (-f) or a directory with feature tagging results (-d)')

    if args.command == diff_cmd:
        result = diff(retriever, args.timestamp1, args.system1,
                              args.timestamp2, args.system2, args.remove_failed, args.only_same)
        json.dump(result, sys.stdout, cls=DateTimeEncoder)
        print()
    elif args.command == dup_cmd:
        results = dup(retriever, args.timestamp,
                              args.system, args.remove_failed)
        if results:
            json.dump(results, sys.stdout, default=lambda x: str(x))
            print()
    elif args.command == export_cmd:
        export(retriever, args.output, args.timestamps, args.systems)
    elif args.command == list_cmd:
        result = retriever.get_sorted_timestamps_and_systems()
        json.dump(result, sys.stdout, cls=DateTimeEncoder)
        print()
    else:
        raise RuntimeError(f'command not recognized: {args.command}')


if __name__ == '__main__':
    main()
