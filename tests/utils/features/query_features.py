#!/usr/bin/env python3

from abc import abstractmethod, ABC
import argparse
from collections import defaultdict
import concurrent.futures
from contextlib import closing
import datetime
import json
import os
import pymongo
import pymongo.collection
import sys
from typing import Any, Iterable

from features import SystemFeatures


KNOWN_FEATURES = ['cmds', 'endpoints',
                  'ensures', 'tasks', 'changes', 'interfaces']


class TaskId:
    suite: str
    task_name: str

    def __init__(self, suite: str, task_name: str) -> None:
        self.suite = suite
        self.task_name = task_name

    def __eq__(self, value) -> bool:
        if not isinstance(value, TaskId):
            return False
        return self.suite == value.suite and self.task_name == value.task_name

    def __hash__(self) -> int:
        return hash((self.suite, self.task_name))

    def __str__(self) -> str:
        return self.suite + ":" + self.task_name


class TaskIdVariant(TaskId):
    variant: str

    def __init__(self, suite: str, task_name: str, variant: str) -> None:
        super().__init__(suite, task_name)
        self.variant = variant

    def __eq__(self, value) -> bool:
        if isinstance(value, TaskIdVariant):
            return self.suite == value.suite and self.task_name == value.task_name and self.variant == value.variant
        elif isinstance(value, TaskId):
            return self.suite == value.suite and self.task_name == value.task_name
        else:
            return False

    def __hash__(self) -> int:
        return hash((self.suite, self.task_name, self.variant))

    def __str__(self) -> str:
        return self.suite + ":" + self.task_name + ":" + self.variant


class DateTimeEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, datetime.datetime):
            return obj.isoformat()


class Retriever(ABC):
    '''
    Retrieves features tags from a data source.
    '''
    @abstractmethod
    def get_sorted_timestamps_and_systems(self) -> list[dict[str, Any]]:
        '''
        Gets the complete list of all timestamps and the systems run under each timestamp.
        Format: [{"timestamp":<timestamp>,"systems":[<system1>,...,<systemN>]}]
        '''

    @abstractmethod
    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        '''
        Given the timestamp and system, gets a single dictionary entry.

        :raises RuntimeError: when there is not exactly one entry for the system at the timestamp
        '''

    @abstractmethod
    def get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        '''
        Retrieves dictionary entries for all indicated systems at 
        the given timestamp. If systems is None, then it retrieves 
        all dictionary entries for all systems at the given timestamp.
        '''

    @abstractmethod
    def get_all_features(self, timestamp: str) -> dict:
        '''
        Retrives a dictionary of all feature data at the given timestmap.
        It contains only feature keys (e.g. cmds, endpoints) and the list
        of all possible feature data at the indicated moment in time.
        '''

    @abstractmethod
    def close(self) -> None:
        pass


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

    def close(self):
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
    
    def get_all_features(self, timestamp):
        json_result = self.collection.find(
            {'timestamp': datetime.datetime.fromisoformat(timestamp), 'all_features': True}).to_list()
        if len(json_result) != 1:
            raise RuntimeError(f'{len(json_result)} entries of feature coverage information found in collection {timestamp}')
        res = json_result[0]
        del res['timestamp']
        del res['all_features']
        return res


class DirRetriever(Retriever):
    '''
    Retrieves features tagging data from the filesystem.
    It assumes data is saved in the following structure:
     <dir>/<timestamp>/<system>.json.
    To populate a directory with data from mongo, do the following:

    ./query_features.py export -f /mongo/creds.json -o /write/dir -t <timestamp1> .. <timestampN> -s <system1> .. <systemN>

    Then one can use /write/dir as a data source with this retriever
    '''

    def __init__(self, dir: str):
        if not os.path.exists(dir):
            raise RuntimeError(f'directory {dir} does not exist')
        self.dir = dir

    @staticmethod
    def __get_filename_without_last_ext(filename):
        return filename.rsplit('.', 1)[0]

    def close(self):
        pass

    def get_sorted_timestamps_and_systems(self) -> list[dict[str, Any]]:
        dictionary = defaultdict(list)
        for timestamp in os.listdir(self.dir):
            timestamp_path = os.path.join(self.dir, timestamp)
            if not os.path.isdir(timestamp_path):
                continue
            for filename in os.listdir(timestamp_path):
                if filename.endswith('.json'):
                    system = self.__get_filename_without_last_ext(filename)
                    dictionary[timestamp].append(system)
        return [{"timestamp": entry[0], "systems": entry[1]} for entry in sorted(dictionary.items(), reverse=True)]

    def get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        timestamp_dir = os.path.join(self.dir, timestamp)
        if not os.path.isdir(timestamp_dir):
            raise RuntimeError(
                f'timestamp {timestamp} not present in dir {self.dir}')
        for filename in os.listdir(timestamp_dir):
            if filename.endswith('.json') and (not systems or self.__get_filename_without_last_ext(filename) in systems):
                with open(os.path.join(timestamp_dir, filename), 'r', encoding='utf-8') as f:
                    yield json.load(f)

    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        sys_path = os.path.join(self.dir, timestamp, system+'.json')
        if not os.path.exists(sys_path):
            raise RuntimeError(f'system file not found {sys_path}')
        with open(sys_path, 'r', encoding='utf-8') as f:
            return json.load(f)
        
    def get_all_features(self, timestamp):
        sys_path = os.path.join(self.dir, timestamp, 'all-features.json')
        if not os.path.exists(sys_path):
            raise RuntimeError(f'all-features.json not found')
        with open(sys_path, 'r', encoding='utf-8') as f:
            all = json.load(f)
            if 'timestamp' in all:
                del all['timestamp']
            if 'all_features' in all:
                del all['all_features']
            return all


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
            if feature_name not in KNOWN_FEATURES:
                continue
            for feature in test[feature_name]:
                if feature not in features[feature_name]:
                    features[feature_name].append(feature)
    return features


def minus(first: dict[str, list], second: dict[str, list]) -> dict:
    '''
    Creates a new dictionary of first - second calculated on values.

    Ex: 
    >>> first = {'a':['b','c'],'d':['e']}
    >>> second = {'a':['c'], 'q':[]}
    >>> minus(first, second)
    {'a': ['b'], 'd': ['e']}
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
    Calculates set(system1_features) - set(system2_features), each at their
    respective timestamps. 

    :param remove_failed: if true, will remove all instances of tests where success == False
    :param only_same: if true, will only calculate difference between tests run on both systems
    '''
    system_json1 = retriever.get_single_json(timestamp1, system1)
    system_json2 = retriever.get_single_json(timestamp2, system2)

    include_tasks1 = None
    include_tasks2 = None
    if remove_failed or only_same:
        include_tasks1 = list_tasks(system_json1, remove_failed)
        include_tasks2 = list_tasks(system_json2, remove_failed)
        if only_same:
            include_tasks2 = include_tasks1 = include_tasks1.intersection(
                include_tasks2)

    features1 = consolidate_system_features(
        system_json1, include_tasks=include_tasks1)
    features2 = consolidate_system_features(
        system_json2, include_tasks=include_tasks2)
    mns = minus(features1, features2)
    return mns


def diff_all_features(retriever: Retriever, timestamp: str, system: str, remove_failed: bool) -> dict:
    '''
    Calculates set(coverage_features) - set(system_features), at the indicated timestamp. 

    :param remove_failed: if true, will remove all instances of tests where success == False
    '''
    sys_features = feat_sys(retriever, timestamp, system, remove_failed)
    mns = minus(retriever.get_all_features(timestamp), sys_features)
    return mns


def feat_sys(retriever: Retriever, timestamp: str, system: str, remove_failed: bool, suite: str = None, task: str = None, variant: str = None) -> dict:
    '''
    Calculates set(system_features), at the indicated timestamp. 

    :param remove_failed: if true, will remove all instances of tests where success == False
    '''
    system_json = retriever.get_single_json(timestamp, system)

    if suite or task or variant:
        system_json['tests'] = [test for test in system_json['tests'] 
                                if (not suite or test['suite'] == suite) and 
                                (not task or test['task_name'] == task) and 
                                (not variant or test['variant'] == variant)]

    include_tasks = None
    if remove_failed:
        include_tasks = list_tasks(system_json, remove_failed)

    return consolidate_system_features(
        system_json, include_tasks=include_tasks)


def check_duplicate(args):
    task, system_json = args
    # Exclude all variants of the task from consolidation so that
    # it isn't flagged as a duplicate when variants of the same
    # task have identical features.
    task_id = TaskId(suite=task['suite'], task_name=task['task_name'])
    features = consolidate_system_features(
        system_json, exclude_tasks=[task_id])
    to_check = {key: value for key,
                value in task.items() if key in KNOWN_FEATURES}
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
        results = executor.map(
            check_duplicate, [(task, system_json) for task in system_json['tests']])
        for result in results:
            if result is not None:
                duplicates.append(result)

    return duplicates


def export(retriever: Retriever, output: str, timestamps: list[str], systems: list[str] = None):
    '''
    Writes the feature data to the output directory in format <dir>/<timestamp>/<system>.json.
    It creates one directory for each supplied timestamp, and will write one json file for each system
    in that timestamp and present in the systems list. If the systems list is None, then it will write
    all systems at each supplied timestamp.
    '''
    for timestamp in timestamps:
        os.makedirs(os.path.join(output, timestamp), exist_ok=True)
        for system_json in retriever.get_systems(timestamp, systems):
            with open(os.path.join(output, timestamp, system_json['system'] + ".json"), 'w', encoding='utf-8') as f:
                json.dump(system_json, f, cls=DateTimeEncoder)


def add_data_source_args(parser: argparse.ArgumentParser):
    parser.add_argument('-f', '--file', help='json file containing creds for mongodb', type=argparse.FileType('r', encoding='utf-8'))
    parser.add_argument('-d', '--dir', help='folder containing feature data', type=str)


def add_diff_parser(subparsers: argparse._SubParsersAction):
    diff_description = '''
        Calculates feature diff between two systems: set(features_1) - set(features_2).
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
    add_data_source_args(diff)
    diff.add_argument('-t1', '--timestamp1', help='timestamp of first execution', type=str, required=True)
    diff.add_argument('-s1', '--system1', help='system of first execution', type=str, required=True)
    diff.add_argument('-t2', '--timestamp2', help='timestamp of second execution', type=str, required=True)
    diff.add_argument('-s2', '--system2', help='system of second execution', type=str, required=True)
    diff.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    diff.add_argument('--only-same', help='only compare tasks that were executed on both systems', action='store_true')
    return cmd


def add_diff_parsers(subparsers: argparse._SubParsersAction):

    sys_description = '''
        Calculates feature diff between two systems: set(features_1) - set(features_2).
        You can specify either a json file with credentials for mongodb or a directory with features output.
        If using a directory, the directory format must be <dir>/<timestamp1>/<system1>.json and 
        <dir>/<timestamp2>/<system2>.json.

        By default, it will compare all features across both systems. If you wish to restrict the comparison
        to only tasks that were successful, use the --remove-failed flag. If you wish to restrict the
        comparison to only tasks that executed on both systems, use the --only-same flag.

        If you wish to create a directory from mongo data, use the export command instead of diff first.
    '''
    all_description = '''
        Calculates feature diff between all possible features a system's: set(all_features) - set(system_features).
        You can specify either a json file with credentials for mongodb or a directory with features output.
        If using a directory, the directory format must be <dir>/<timestamp1>/<system1>.json and 
        <dir>/<timestamp1>/all-feat.json.

        By default, it will compare all features . If you wish to restrict the comparison
        to only tasks that were successful, use the --remove-failed flag.

        If you wish to create a directory from mongo data, use the export command instead of diff first.
    '''
    cmd = 'diff'
    cmd_sys = 'systems'
    cmd_all = 'all-features'
    diff: argparse.ArgumentParser = subparsers.add_parser(cmd, help='calculate diff between features',
                                 description='Calculates feature diff either between two systems or between a system and all possible features.')
    diff_subparsers = diff.add_subparsers(dest='diff_cmd')
    sys = diff_subparsers.add_parser(cmd_sys, help='calculate diff between two systems\' features',
                               description=sys_description, formatter_class=argparse.RawDescriptionHelpFormatter)
    add_data_source_args(sys)
    sys.add_argument('-t1', '--timestamp1', help='timestamp of first execution', type=str, required=True)
    sys.add_argument('-s1', '--system1', help='system of first execution', type=str, required=True)
    sys.add_argument('-t2', '--timestamp2', help='timestamp of second execution', type=str, required=True)
    sys.add_argument('-s2', '--system2', help='system of second execution', type=str, required=True)
    sys.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    sys.add_argument('--only-same', help='only compare tasks that were executed on both systems', action='store_true')

    all = diff_subparsers.add_parser(cmd_all, help='calculate diff between system features and all features',
                                     description=all_description, formatter_class=argparse.RawDescriptionHelpFormatter)
    add_data_source_args(all)
    all.add_argument('-t', '--timestamp', help='timestamp of instance to search', required=True, type=str)
    all.add_argument('-s', '--system', help='system whose features should be searched', required=True, type=str)
    all.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    return cmd, cmd_sys, cmd_all


def add_dup_parser(subparsers: argparse._SubParsersAction):
    dup_description = '''
        For each task present in the indicated system under the indicated timestamp,
        calculates the difference between that task's features and the system's 
        without the task: set(task_features) - set(system_features without task).
        If the difference is ever empty, then that task is printed to console as
        a duplicate feature.

        To remove all failed tasks from consideration, add the --remove-failed flag.
    '''
    cmd = 'dup'
    duplicate: argparse.ArgumentParser = subparsers.add_parser(cmd,
                                      help='show tasks whose features are completely covered by the rest',
                                      description=dup_description,
                                      formatter_class=argparse.RawDescriptionHelpFormatter)
    add_data_source_args(duplicate)
    duplicate.add_argument('-t', '--timestamp', help='timestamp of instance to search', required=True, type=str)
    duplicate.add_argument('-s', '--system', help='system whose features should be searched', required=True, type=str)
    duplicate.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    return cmd


def add_export_parser(subparsers: argparse._SubParsersAction):
    cmd = 'export'
    export: argparse.ArgumentParser = subparsers.add_parser(cmd, help='export data to output local directory',
                                   description='Grabs system json files by timestamps and systems and saves them to the folder indicated in the output arguement.')
    add_data_source_args(export)
    export.add_argument('-t', '--timestamps', help='space-separated list of identifying timestamps', required=True, nargs='+')
    export.add_argument('-s', '--systems', help='space-separated list of systems', nargs='*')
    export.add_argument('-o', '--output', help='folder to save feature data', required=True, type=str)
    return cmd


def add_list_parser(subparsers: argparse._SubParsersAction):
    cmd = 'list'
    lst: argparse.ArgumentParser = subparsers.add_parser(cmd, help='lists all timestamps with systems present in data source',
                                description='Lists all timestamps with systems present in data source.')
    add_data_source_args(lst)
    return cmd


def add_all_features_parser(subparsers: argparse._SubParsersAction):
    cmd = 'feat'
    cmd_all = 'all'
    cmd_sys = 'sys'
    feat: argparse.ArgumentParser = subparsers.add_parser(cmd, help='', description='')
    feat_subparsers = feat.add_subparsers(dest='features_cmd')
    all = feat_subparsers.add_parser(cmd_all, help='lists all features at a given timestamp',
                                     description='Lists all features for a given timestamp present in data source.')
    add_data_source_args(all)
    all.add_argument('-t', '--timestamp', help='timestamp for feature data', required=True, type=str)

    sys = feat_subparsers.add_parser(cmd_sys, help='lists all features for a given system and timestamp',
                                     description='Lists all features for a given system and timestamp present in data source.')
    add_data_source_args(sys)
    sys.add_argument('-t', '--timestamp', help='timestamp for feature data', required=True, type=str)
    sys.add_argument('-s', '--system', help='system for feature data', required=True, type=str)
    sys.add_argument('--suite', help='if provided, only grab features from this suite', default=None, type=str)
    sys.add_argument('--task', help='if provided, only grab features of this task', default=None, type=str)
    sys.add_argument('--variant', help='if provided, only grab features with this variant', default=None, type=str)
    sys.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    return cmd, cmd_all, cmd_sys


def main():
    parser = argparse.ArgumentParser(
        description='cli to query data source containing feature data')
    subparsers = parser.add_subparsers(dest='command')
    subparsers.required = True
    diff_cmd, diff_sys_cmd, diff_all_cmd = add_diff_parsers(subparsers)
    dup_cmd = add_dup_parser(subparsers)
    export_cmd = add_export_parser(subparsers)
    list_cmd = add_list_parser(subparsers)
    feat_cmd, feat_all_cmd, feat_sys_cmd = add_all_features_parser(subparsers)

    args = parser.parse_args()

    retriever_creator = None
    if args.dir:
        def retriever_creator(): return DirRetriever(args.dir)
    elif args.file:
        def retriever_creator(): return MongoRetriever(args.file)
    else:
        raise RuntimeError(
            'you must specify either a mongodb credential file (-f) or a directory with feature tagging results (-d)')

    with closing(retriever_creator()) as retriever:
        if args.command == diff_cmd:
            if args.diff_cmd == diff_sys_cmd:
                result = diff(retriever, args.timestamp1, args.system1,
                            args.timestamp2, args.system2, args.remove_failed, args.only_same)
            elif args.diff_cmd == diff_all_cmd:
                result = diff_all_features(retriever, args.timestamp, args.system, args.remove_failed)
            else:
                raise RuntimeError(f'diff command not recognized: {args.diff_cmd}')
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
        elif args.command == feat_cmd:
            if args.features_cmd == feat_all_cmd:
                result = retriever.get_all_features(args.timestamp)
            elif args.features_cmd == feat_sys_cmd:
                result = feat_sys(retriever, args.timestamp, args.system, args.remove_failed, args.suite, args.task, args.variant)
            else:
                raise RuntimeError(f'unrecognized feature command {args.features_cmd}')
            json.dump(result, sys.stdout, cls=DateTimeEncoder)
            print()
        else:
            raise RuntimeError(f'command not recognized: {args.command}')


if __name__ == '__main__':
    main()
