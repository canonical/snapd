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

from features import SystemFeatures, Cmd, Endpoint, Status, Task, Change, Interface


KNOWN_FEATURES = ['cmds', 'endpoints',
                  'ensures', 'tasks', 'changes', 'interfaces']

# file name for the complete list of all features
ALL_FEATURES_FILE = 'all-features.json'


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
        if self.variant:
            return self.suite + ":" + self.task_name + ":" + self.variant
        return self.suite + ":" + self.task_name


class DateTimeEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, datetime.datetime):
            return obj.isoformat()


class Retriever(ABC):
    '''
    Retrieves features tags from a data source.
    '''

    def __init__(self):
        self.cache = {}

    @abstractmethod
    def get_sorted_timestamps_and_systems(self) -> list[dict[str, Any]]:
        '''
        Gets the complete list of all timestamps and the systems run under each timestamp.
        Format: [{"timestamp":<timestamp>,"systems":[<system1>,...,<systemN>]}]
        '''

    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        '''
        Given the timestamp and system, gets a single dictionary entry.

        :raises RuntimeError: when there is not exactly one entry for the system at the timestamp
        '''
        if timestamp in self.cache and 'systems' in self.cache[timestamp]:
            for s in self.cache[timestamp]['systems']:
                if s['system'] == system:
                    return s
        return self._get_single_json(timestamp, system)

    @abstractmethod
    def _get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        pass

    def get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        '''
        Retrieves dictionary entries for all indicated systems at 
        the given timestamp. If systems is None, then it retrieves 
        all dictionary entries for all systems at the given timestamp.
        '''
        if timestamp in self.cache and 'systems' in self.cache[timestamp]:
            if not systems:
                return self.cache[timestamp]['systems']
            else:
                return [sys for sys in self.cache[timestamp]['systems'] if sys['system'] in systems]
            
        if systems:
            # Do not cache if only a subset of systems is specified
            # otherwise the cached list won't be complete
            return self._get_systems(timestamp, systems)
        else:
            if timestamp not in self.cache:
                self.cache[timestamp] = {}
            self.cache[timestamp]['systems'] = list(self._get_systems(timestamp, systems))
            return self.cache[timestamp]['systems']
        

    @abstractmethod
    def _get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        pass

    def get_all_features(self, timestamp: str) -> dict[str, list[Any]]:
        '''
        Retrives a dictionary of all feature data at the given timestmap.
        It contains only feature keys (e.g. cmds, endpoints) and the list
        of all possible feature data at the indicated moment in time.
        '''
        if timestamp in self.cache and 'all-features' in self.cache[timestamp]:
            return self.cache[timestamp]['all-features']
        if timestamp not in self.cache:
            self.cache[timestamp] = {}
        all_feat = self._get_all_features(timestamp)
        self.cache[timestamp]['all-features'] = all_feat
        return all_feat

    @abstractmethod
    def _get_all_features(self, timestamp: str) -> dict[str, list[Any]]:
        pass

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
        super().__init__()
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
            if 'all_features' in result and result['all_features'] == True:
                continue
            dictionary[result['timestamp'].isoformat()].append(
                result['system'])
        return [{"timestamp": entry[0], "systems": entry[1]} for entry in sorted(dictionary.items(), reverse=True)]

    def _get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        if systems:
            for system in systems:
                system_jsons = self.collection.find(
                    {'timestamp': datetime.datetime.fromisoformat(timestamp), 'system': system})
                for system_json in system_jsons:
                    yield system_json
        else:
            for result in self.collection.find({'timestamp': datetime.datetime.fromisoformat(timestamp)}):
                if 'all_features' not in result or not result['all_features']:
                    yield result

    def _get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        json_result = self.collection.find(
            {'timestamp': datetime.datetime.fromisoformat(timestamp), 'system': system}).to_list()
        if len(json_result) != 1:
            raise RuntimeError(f'{len(json_result)} entries of system {system} found in collection {timestamp}')
        return json_result[0]
    
    def _get_all_features(self, timestamp) -> dict[str, list[Any]]:
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
        super().__init__()
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
                if filename.endswith('.json') and not filename == ALL_FEATURES_FILE:
                    system = self.__get_filename_without_last_ext(filename)
                    dictionary[timestamp].append(system)
        return [{"timestamp": entry[0], "systems": entry[1]} for entry in sorted(dictionary.items(), reverse=True)]

    def _get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:
        timestamp_dir = os.path.join(self.dir, timestamp)
        if not os.path.isdir(timestamp_dir):
            raise RuntimeError(
                f'timestamp {timestamp} not present in dir {self.dir}')
        for filename in os.listdir(timestamp_dir):
            if filename == ALL_FEATURES_FILE:
                continue
            if filename.endswith('.json') and (not systems or self.__get_filename_without_last_ext(filename) in systems):
                with open(os.path.join(timestamp_dir, filename), 'r', encoding='utf-8') as f:
                    yield json.load(f)

    def _get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        sys_path = os.path.join(self.dir, timestamp, system+'.json')
        if not os.path.exists(sys_path):
            raise RuntimeError(f'system file not found {sys_path}')
        with open(sys_path, 'r', encoding='utf-8') as f:
            return json.load(f)
        
    def _get_all_features(self, timestamp) -> dict[str, list[Any]]:
        sys_path = os.path.join(self.dir, timestamp, ALL_FEATURES_FILE)
        if not os.path.exists(sys_path):
            raise RuntimeError(f'{ALL_FEATURES_FILE} not found')
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
    Calculates set(coverage_features) - set(system_features), at the indicated timestamp. The difference
    in features for cmds, ensures, and endpoints is calculated on the complete feature. The difference
    for tasks is computed on kind and last_status. The difference for changes are computed on kind.
    The difference for interfaces is computed on name.

    :param remove_failed: if true, will remove all instances of tests where success == False
    '''
    sys_features = feat_sys(retriever, timestamp, system, remove_failed)
    all_features = retriever.get_all_features(timestamp)
    diff = minus({'cmds': all_features['cmds'], 'ensures': all_features['ensures'], 'endpoints': all_features['endpoints']}, sys_features)
    tasks = [af for af in all_features['tasks'] 
             if not any(af['kind'] == sf['kind'] and af['last_status'] == sf['last_status'] for sf in sys_features['tasks'])]
    changes = [af for af in all_features['changes'] 
               if not any(af['kind'] == sf['kind'] for sf in sys_features['changes'])]
    interfaces = [af for af in all_features['interfaces'] 
                  if not any(af['name'] == sf['name'] for sf in sys_features['interfaces'])]
    if tasks:
        diff['tasks'] = tasks
    if changes:
        diff['changes'] = changes
    if interfaces:
        diff['interfaces'] = interfaces
    return diff


def feat_sys(retriever: Retriever, timestamp: str, system: str, remove_failed: bool, suite: str = None, task: str = None, variant: str = None) -> dict:
    '''
    Calculates set(system_features), at the indicated timestamp. 

    :param remove_failed: if true, will remove all instances of tests where success == False
    '''
    system_json = retriever.get_single_json(timestamp, system)

    if suite or task or variant:
        tests = system_json['tests']
        system_json = SystemFeatures(schema_version=system_json['schema_version'] if 'schema_version' in system_json else '', 
                                     system=system_json['system'] if 'system' in system_json else '', 
                                     scenarios=system_json['scenarios'] if 'scenarios' in system_json else '',
                                     env_variables=system_json['env_variables'] if 'env_variables' in system_json else '')
        system_json['tests'] = [test for test in tests 
                                if (not suite or test['suite'] == suite) and 
                                (not task or test['task_name'] == task) and 
                                (not variant or test['variant'] == variant)]

    include_tasks = None
    if remove_failed:
        include_tasks = list_tasks(system_json, remove_failed)

    return consolidate_system_features(
        system_json, include_tasks=include_tasks)


def get_feature_name_from_feature(feat: dict) -> str:
    if 'cmd' in feat and len(feat) == 1:
        return 'cmds'
    elif 'method' in feat and 'path' in feat and (len(feat) == 2 or len(feat) == 3):
        return 'endpoints'
    elif 'manager' in feat and 'function' in feat and len(feat) == 2:
        return 'ensures'
    elif 'kind' in feat and 'last_status' in feat:
        return 'tasks'
    elif 'name' in feat:
        return 'interfaces'
    elif 'kind' in feat:
        return 'changes'
    return ''


def find_feat(retriever: Retriever, timestamp: str, feat: dict, remove_failed: bool, system: str = None) -> dict[str, TaskIdVariant]:
    '''
    Given a timestamp, a feature, and optionally a system, finds
    all tests that contain the indicated feature. If no system
    is specified, then returns all systems that contain the 
    feature combined with tests.

    A feature match happens for cmds, endpoints, and ensures
    when all of their fields match exactly.

    A feature match happens for tasks when the kind and last_status
    are identical.

    A feature match happens for changes when the kinds match.

    A feature match happens for interfaces when the names match.

    :param remove_failed: if true, will remove all instances of tests where success == False
    :returns: dictionary where each key is a system and each value is a list of tests that contain the feature
    '''

    feat_name = get_feature_name_from_feature(feat)
    if not feat_name:
        raise RuntimeError(f'feature {feat} not a recognized feature')

    feat_in_test=lambda _: False
    if feat_name == 'cmds' or feat_name == 'ensures' or feat_name == 'endpoints':
        feat_in_test = lambda test: feat_name in test and feat in test[feat_name]
    elif feat_name == 'tasks':
        feat_in_test = lambda test: feat_name in test and any(t['kind'] == feat['kind'] and t['last_status'] == feat['last_status'] for t in test['tasks'])
    elif feat_name == 'changes':
        feat_in_test = lambda test: feat_name in test and any(c['kind'] == feat['kind'] for c in test['changes'])
    elif feat_name == 'interfaces':
        feat_in_test = lambda test: feat_name in test and any(i['name'] == feat['name'] for i in test['interfaces'])

    system_list = None
    if system:
        system_list = [system]
    system_jsons = retriever.get_systems(timestamp, systems=system_list)

    ret = {}
    for system_json in system_jsons:
        tests = [TaskIdVariant(suite=test['suite'], task_name=test['task_name'], variant=test['variant']) 
                    for test in system_json['tests'] if feat_in_test(test) and (not remove_failed or test['success'])]
        if tests:
            ret[system_json['system']] = tests
    return ret


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
        tests = system_json['tests']
        system_json = SystemFeatures(schema_version=system_json['schema_version'] if 'schema_version' in system_json else '', 
                                     system=system_json['system'] if 'system' in system_json else '', 
                                     scenarios=system_json['scenarios'] if 'scenarios' in system_json else '',
                                     env_variables=system_json['env_variables'] if 'env_variables' in system_json else '')
        system_json['tests'] = [
            task for task in tests if task['success']]

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
        try:
            all_features = retriever.get_all_features(timestamp)
            with open(os.path.join(output, timestamp, ALL_FEATURES_FILE), 'w', encoding='utf-8') as f:
                json.dump(all_features, f, cls=DateTimeEncoder)
        except Exception as e:
            print(f'could not find all features at timestamp {timestamp}', file=sys.stderr)


def task_list(retriever: Retriever, timestamp: str) -> set[TaskIdVariant]:
    '''
    Given a timestamp, gets the complete set of tasks (suite, task, and variant names)
    '''
    all_data = retriever.get_systems(timestamp)
    tasks = set()
    for data in all_data:
        if 'tests' not in data:
            continue
        tasks.update(
            {TaskIdVariant(suite=d['suite'], task_name=d['task_name'], variant=d['variant']) for d in data['tests']}
        )
    return tasks


def add_data_source_args(parser: argparse.ArgumentParser) -> None:
    parser.add_argument('-f', '--file', help='json file containing creds for mongodb', type=argparse.FileType('r', encoding='utf-8'))
    parser.add_argument('-d', '--dir', help='folder containing feature data', type=str)


def add_diff_parser(subparsers: argparse._SubParsersAction) -> str:
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


def add_diff_parsers(subparsers: argparse._SubParsersAction) -> tuple[str, str, str]:

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


def add_dup_parser(subparsers: argparse._SubParsersAction) -> str:
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


def add_export_parser(subparsers: argparse._SubParsersAction) -> str:
    cmd = 'export'
    export: argparse.ArgumentParser = subparsers.add_parser(cmd, help='export data to output local directory',
                                   description='Grabs system json files by timestamps and systems and saves them to the folder indicated in the output arguement.')
    add_data_source_args(export)
    export.add_argument('-t', '--timestamps', help='space-separated list of identifying timestamps', required=True, nargs='+')
    export.add_argument('-s', '--systems', help='space-separated list of systems', nargs='*')
    export.add_argument('-o', '--output', help='folder to save feature data', required=True, type=str)
    return cmd


def add_list_parser(subparsers: argparse._SubParsersAction) -> str:
    cmd = 'list'
    lst: argparse.ArgumentParser = subparsers.add_parser(cmd, help='lists all timestamps with systems present in data source',
                                description='Lists all timestamps with systems present in data source.')
    add_data_source_args(lst)
    return cmd


def add_all_features_parser(subparsers: argparse._SubParsersAction) -> tuple[str, str, str, str]:
    cmd = 'feat'
    cmd_all = 'all'
    cmd_sys = 'sys'
    cmd_find = 'find'
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


    find = feat_subparsers.add_parser(cmd_find, help='given a feature, finds which tests contain it',
                                     description='Lists tests that contain the feature')
    add_data_source_args(find)
    find.add_argument('-t', '--timestamp', help='timestamp for feature data', required=True, type=str)
    find.add_argument('-s', '--system', help='(optional) system to search for feature in', default=None, type=str)
    find.add_argument('--feat', help='feature to search for (json format)', required=True, type=str)
    find.add_argument('--remove-failed', help='remove all tasks that failed', action='store_true')
    return cmd, cmd_all, cmd_sys, cmd_find


def main():
    parser = argparse.ArgumentParser(
        description='cli to query data source containing feature data')
    subparsers = parser.add_subparsers(dest='command')
    subparsers.required = True
    diff_cmd, diff_sys_cmd, diff_all_cmd = add_diff_parsers(subparsers)
    dup_cmd = add_dup_parser(subparsers)
    export_cmd = add_export_parser(subparsers)
    list_cmd = add_list_parser(subparsers)
    feat_cmd, feat_all_cmd, feat_sys_cmd, feat_find_cmd = add_all_features_parser(subparsers)

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
                json.dump(result, sys.stdout, cls=DateTimeEncoder)
            elif args.features_cmd == feat_sys_cmd:
                result = feat_sys(retriever, args.timestamp, args.system, args.remove_failed, args.suite, args.task, args.variant)
                json.dump(result, sys.stdout, cls=DateTimeEncoder)
            elif args.features_cmd == feat_find_cmd:
                try:
                    feat = json.loads(args.feat)
                    result = find_feat(retriever, args.timestamp, feat, args.remove_failed, args.system)
                    json.dump(result, sys.stdout, default=lambda x: str(x))
                except Exception as e:
                    raise RuntimeError(f'Error parsing feature {args.feat}: {e}')
            else:
                raise RuntimeError(f'unrecognized feature command {args.features_cmd}')
            print()
        else:
            raise RuntimeError(f'command not recognized: {args.command}')


if __name__ == '__main__':
    main()
