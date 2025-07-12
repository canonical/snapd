
import argparse
from copy import deepcopy
from datetime import datetime
from io import StringIO
import json
import os
from pathlib import Path
import pytest
import sys
import tempfile
from typing import Iterable
import unittest
from unittest.mock import Mock, patch
# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import query_features
from query_features import TaskIdVariant
from features import SystemFeatures, TaskFeatures, Cmd, Endpoint, Change, Task, Ensure, Interface


class DictRetriever(query_features.Retriever):
    def __init__(self, data, all_features=None):
        self.data = data
        self.all_features = all_features

    def close(self):
        pass

    def get_sorted_timestamps_and_systems(self) -> list[dict[str, list[str]]]:
        pass

    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        return deepcopy(self.data[timestamp][system])

    def get_systems(self, timestamp: str, systems: list[str] = None) -> Iterable[SystemFeatures]:    
        if systems:
            return [deepcopy(self.data[timestamp][system]) for system in systems]
        else:
            return [deepcopy(data) for _, data in self.data[timestamp].items()]

    def get_all_features(self, timestamp):
        return deepcopy(self.all_features[timestamp])


class FakeClient:
    def close(self):
        pass


class FakeCollectionReturn:
    def __init__(self, l):
        self.l = l

    def __iter__(self):
        return iter(self.l)

    def __len__(self):
        return len(self.l)

    def to_list(self):
        return self.l


class FakeMongoCollection:
    def __init__(self, list_json):
        self.list_json = list_json

    def find(self, dictionary=None):
        l = []
        for doc in self.list_json:
            if not dictionary or all(FakeMongoCollection.check_equals(key, dictionary, doc) for key in dictionary.keys()):
                l.append(doc)
        return FakeCollectionReturn(l)
    
    def check_equals(key, dict1, dict2):
        if key not in dict1 or key not in dict2:
            return False
        ts1 = dict1[key]
        ts2 = dict2[key]
        if key != 'timestamp':
            return ts1 == ts2
        if isinstance(ts1, str):
            ts1 = datetime.fromisoformat(ts1)
        if isinstance(ts2, str):
            ts2 = datetime.fromisoformat(ts2)
        return ts1 == ts2
            


class MongoMocker:
    def __init__(self, collection_data, do_patch_stdout=False):
        self.collection_data = collection_data
        for data in self.collection_data:
            if 'timestamp' in data:
                data['timestamp'] = datetime.fromisoformat(data['timestamp'])
        self.do_patch_stdout = do_patch_stdout
        self.patch_stdout = None
        self.patch_mongo = None

    def get_stdout(self):
        return self.stdout.getvalue()

    def __enter__(self):
        data = self.collection_data

        def my_init(self, *_):
            self.collection = FakeMongoCollection(data)
            self.client = FakeClient()
        self.patch_mongo = patch.object(
            query_features.MongoRetriever, '__init__', my_init)
        self.patch_mongo.start()
        if self.do_patch_stdout:
            self.patch_stdout = patch('sys.stdout', new=StringIO())
            self.stdout = self.patch_stdout.start()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.patch_mongo:
            self.patch_mongo.stop()
        if self.patch_stdout:
            self.patch_stdout.stop()


class DirMocker:
    def __init__(self, collection_data, do_patch_stdout=False):
        self.collection_data = collection_data
        self.do_patch_stdout = do_patch_stdout
        self.patch_stdout = None
        self.tmpdir = None

    def get_stdout(self):
        return self.stdout.getvalue()

    def get_dir(self):
        return self.tmpdir.name

    def __populate_dir(self):
        for doc in self.collection_data:
            dir = os.path.join(self.tmpdir.name, doc['timestamp'])
            os.makedirs(dir, exist_ok=True)
            if 'all_features' in doc:
                with open(os.path.join(dir, "all-features.json"), 'w', encoding='utf-8') as f:
                    json.dump(doc, f)
            else:
                with open(os.path.join(dir, f'{doc["system"]}.json'), 'w', encoding='utf-8') as f:
                    json.dump(doc, f)

    def __enter__(self):
        self.tmpdir = tempfile.TemporaryDirectory()
        self.__populate_dir()
        if self.do_patch_stdout:
            self.patch_stdout = patch('sys.stdout', new=StringIO())
            self.stdout = self.patch_stdout.start()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.tmpdir.cleanup()
        if self.patch_stdout:
            self.patch_stdout.stop()


class TestQueryFeatures:

    def test_dirretriever_get_sorted_timestamps_and_systems(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.mkdir(os.path.join(tmpdir, 'timestamp1'))
            Path(os.path.join(tmpdir, 'randomFile')).touch()
            os.mkdir(os.path.join(tmpdir, 'timestamp2'))
            Path(os.path.join(tmpdir, 'timestamp1', 'system1.json')).touch()
            Path(os.path.join(tmpdir, 'timestamp1', 'system2.json')).touch()
            Path(os.path.join(tmpdir, 'timestamp2', 'system2.json')).touch()
            Path(os.path.join(tmpdir, 'timestamp2', 'randomfile')).touch()
            os.mkdir(os.path.join(tmpdir, 'timestamp3'))

            retriever = query_features.DirRetriever(tmpdir)
            results = retriever.get_sorted_timestamps_and_systems()
            assert 2 == len(results)
            assert {'timestamp':'timestamp2','systems':['system2']} in results
            assert {'timestamp':'timestamp1','systems':['system1','system2']} in results or {'timestamp':'timestamp1','systems':['system2','system1']} in results


    def test_dirretriever_get_systems(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.mkdir(os.path.join(tmpdir, 'timestamp1'))
            with open(os.path.join(tmpdir, 'timestamp1', 'system1.json'), 'w', encoding='utf-8') as f:
                json.dump(SystemFeatures(system='system1'), f)
            with open(os.path.join(tmpdir, 'timestamp1', 'system2.json'), 'w', encoding='utf-8') as f:
                json.dump(SystemFeatures(system='system2'), f)

            retriever = query_features.DirRetriever(tmpdir)
            results = list(retriever.get_systems('timestamp1'))
            assert 2 == len(results)
            assert SystemFeatures(system='system1') in results
            assert SystemFeatures(system='system2') in results
            results = list(retriever.get_systems('timestamp1', ['system1', 'system2']))
            assert 2 == len(results)
            assert SystemFeatures(system='system1') in results
            assert SystemFeatures(system='system2') in results
            results = list(retriever.get_systems('timestamp1', ['system1']))
            assert [SystemFeatures(system='system1')] == results

    def test_dirretriever_get_single_json(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.mkdir(os.path.join(tmpdir, 'timestamp1'))
            with open(os.path.join(tmpdir, 'timestamp1', 'system1.json'), 'w', encoding='utf-8') as f:
                json.dump(SystemFeatures(system='system1'), f)
            retriever = query_features.DirRetriever(tmpdir)
            result = retriever.get_single_json('timestamp1', 'system1')
            assert SystemFeatures(system='system1') == result

    def test_dirretriever_get_all_features(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.mkdir(os.path.join(tmpdir, 'timestamp1'))
            all_features = {
                'timestamp':'timestamp1',
                'cmds':[Cmd(cmd='snap list'),Cmd(cmd='snap pack')],
                'changes':[Change(kind='refresh',snap_types=[])],
                'tasks':[Task(kind='link',snap_types=['snapd'],last_status='Done')]}
            with open(os.path.join(tmpdir, 'timestamp1', 'all-features.json'), 'w', encoding='utf-8') as f:
                json.dump(all_features, f)
            retriever = query_features.DirRetriever(tmpdir)
            result = retriever.get_all_features('timestamp1')
            del all_features['timestamp']
            assert all_features == result

    def test_consolidate_features(self):
        j = {"tests": [
            {"task_name": "task1",
             "cmds": [{"cmd": "snap list --all"}, {"cmd": "snap ack file"},],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc1"}]},
            {"task_name": "task2",
             "cmds": [{"cmd": "snap do things"}, {"cmd": "snap list --all"}],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc2"}]
             }
        ]}
        c = query_features.consolidate_system_features(j)
        assert len(c) == 2
        assert "cmds" in c
        assert len(c["cmds"]) == 3
        assert {"cmd": "snap list --all"} in c["cmds"]
        assert {"cmd": "snap ack file"} in c["cmds"]
        assert {"cmd": "snap do things"} in c["cmds"]
        assert "ensures" in c
        assert len(c["ensures"]) == 2
        assert {"manager":"SnapManager","function":"ensureFunc1"} in c["ensures"]
        assert {"manager":"SnapManager","function":"ensureFunc2"} in c["ensures"]


    def test_consolidate_features_exclude_task(self):
        j = {"tests": [
            {"suite": "suite", "task_name": "task1", "variant": "a",
             "cmds": [{"cmd": "snap list --all"}, {"cmd": "snap ack file"},],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc1"}]},
            {"suite": "suite", "task_name": "task2", "variant": "",
             "cmds": [{"cmd": "snap do things"}, {"cmd": "snap list --all"}],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc2"}]
             }
        ]}
        c = query_features.consolidate_system_features(j, exclude_tasks=[query_features.TaskId(suite='suite',task_name="task1")])
        assert len(c) == 2
        assert "cmds" in c
        assert len(c["cmds"]) == 2
        assert {"cmd": "snap list --all"} in c["cmds"]
        assert {"cmd": "snap do things"} in c["cmds"]
        assert "ensures" in c
        assert len(c["ensures"]) == 1
        assert {"manager": "SnapManager", "function": "ensureFunc2"} in c["ensures"]

    def test_consolidate_features_include_task(self):
        j = {"tests": [
            {"suite": "suite", "task_name": "task1", "variant": "a",
             "cmds": [{"cmd": "snap list --all"}, {"cmd": "snap ack file"},],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc1"}]},
            {"suite": "suite", "task_name": "task2", "variant": "",
             "cmds": [{"cmd": "snap do things"}, {"cmd": "snap list --all"}],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc2"}]
             }
        ]}
        c = query_features.consolidate_system_features(j, include_tasks=[query_features.TaskId(suite='suite',task_name="task2")])
        assert len(c) == 2
        assert "cmds" in c
        assert len(c["cmds"]) == 2
        assert {"cmd": "snap list --all"} in c["cmds"]
        assert {"cmd": "snap do things"} in c["cmds"]
        assert "ensures" in c
        assert len(c["ensures"]) == 1
        assert {"manager": "SnapManager", "function": "ensureFunc2"} in c["ensures"]

    def test_features_minus(self):
        j = {"cmds": [{"cmd": "snap list --all"}, {"cmd": "snap ack file"},],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc1"}],
             }
        k = {"cmds": [{"cmd": "snap list --all"}],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc2"}],
             }
        minus = query_features.minus(j, k)
        assert len(minus) == 2
        assert "cmds" in minus
        assert "ensures" in minus
        assert len(minus["cmds"]), 1
        assert {"cmd": "snap ack file"} in minus["cmds"]
        assert len(minus["ensures"]) == 1
        assert {"manager": "SnapManager", "function": "ensureFunc1"} in minus["ensures"]

    def test_list_tasks(self):
        sys_json = SystemFeatures(tests=[
            TaskFeatures(success=True,task_name='task1',variant='variant1',suite='suite1'),
            TaskFeatures(success=False,task_name='task2',variant='variant2',suite='suite2'),
        ])
        tasks_all = query_features.list_tasks(sys_json, False)
        tasks_success = query_features.list_tasks(sys_json, True)
        assert {query_features.TaskIdVariant('suite1', 'task1', 'variant1'),
                             query_features.TaskIdVariant('suite2', 'task2', 'variant2')} == tasks_all
        assert {query_features.TaskIdVariant('suite1', 'task1', 'variant1')} == tasks_success

    def test_list_tasks_empty(self):
        sys_json = SystemFeatures(tests=[])
        tasks = query_features.list_tasks(sys_json, False)
        assert set() == tasks

    def test_check_dup_none(self):
        system_json = SystemFeatures(tests=[
            TaskFeatures(suite='suite',task_name='task1',variant='a',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task1',variant='b',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task2',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task3',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task4',variant='b',cmds=[Cmd(cmd='cmd1')]),
        ])
        dups = query_features.check_duplicate((system_json['tests'][0], system_json))
        assert dups == None
        dups = query_features.check_duplicate((system_json['tests'][1], system_json))
        assert dups == None

    def test_check_dup_no_variant(self):
        system_json = SystemFeatures(tests=[
            TaskFeatures(suite='suite',task_name='task1',variant='a',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task1',variant='b',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task2',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task3',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task4',variant='b',cmds=[Cmd(cmd='cmd1')]),
        ])
        dups = query_features.check_duplicate((system_json['tests'][2], system_json))
        assert query_features.TaskIdVariant(suite='suite',task_name='task2',variant='') == dups
        dups = query_features.check_duplicate((system_json['tests'][3], system_json))
        assert query_features.TaskIdVariant(suite='suite',task_name='task3',variant='') == dups
        dups = query_features.check_duplicate((system_json['tests'][4], system_json))
        assert query_features.TaskIdVariant(suite='suite',task_name='task4',variant='b') == dups


    def test_dup(self):
        data = {'timestamp1': {'system1': {'tests': [
            TaskFeatures(suite='suite', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list --all"),
                               Cmd(cmd="snap ack file")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack file"),
                               Cmd(cmd="snap debug api")],
                         endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]),
            TaskFeatures(suite='suite', task_name='task3', success=False, variant='v2',
                         cmds=[
                             Cmd(cmd="snap pack file")],
                         endpoints=[Endpoint(method="GET", path="/v2/snaps")]),]}}}
        retriever = DictRetriever(data)
        dup = query_features.dup(retriever, 'timestamp1', 'system1', False)
        assert [query_features.TaskIdVariant(suite='suite', task_name='task3', variant='v2')] == dup
        dup = query_features.dup(retriever, 'timestamp1', 'system1', True)
        assert [] == dup

    def test_dup_variants(self):
        data = {'timestamp1': {'system1': {'tests': [
            TaskFeatures(suite='suite', task_name='task1', success=True, variant='a',
                         cmds=[Cmd(cmd="snap list --all"),
                               Cmd(cmd="snap ack file")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite', task_name='task1', success=True, variant='b',
                         cmds=[Cmd(cmd="snap list --all"),
                               Cmd(cmd="snap ack file")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite', task_name='task3', success=False, variant='v2',
                         endpoints=[Endpoint(method="GET", path="/v2/snaps")]),]}}}
        retriever = DictRetriever(data)
        dup = query_features.dup(retriever, 'timestamp1', 'system1', False)
        # Features from variants of the same test should not influence
        # duplicate calculation. The only duplicate task should be task3
        assert [query_features.TaskIdVariant(
            suite='suite', task_name='task3', variant='v2')] == dup
        dup = query_features.dup(retriever, 'timestamp1', 'system1', True)
        assert [] == dup

    def test_export(self):
        t1s1_dict = SystemFeatures(system='system1', tests=[
            TaskFeatures(suite='suite', task_name='task1',
                         success=True, variant=''),
            TaskFeatures(suite='suite', task_name='task2', success=True, variant='v1')])
        t2s1_dict = SystemFeatures(system='system1', tests=[
            TaskFeatures(suite='suite', task_name='task1', success=False, variant='')])
        s2_dict = SystemFeatures(system='system2')
        data = {'timestamp1': {'system1': t1s1_dict,
                               'system2': s2_dict},
                'timestamp2': {'system1': t2s1_dict,
                               'system2': s2_dict, }}
        retriever = DictRetriever(data)

        def check_equal(file, ref_dict):
            assert os.path.isfile(file)
            with open(file, 'r', encoding='utf-8') as f:
                assert ref_dict == json.load(f)

        with tempfile.TemporaryDirectory() as tmpdir:
            query_features.export(retriever, tmpdir, ['timestamp1', 'timestamp2'], None)
            timestamp1 = os.path.join(tmpdir, 'timestamp1')
            timestamp2 = os.path.join(tmpdir, 'timestamp2')
            assert os.path.isdir(timestamp1)
            assert os.path.isdir(timestamp2)
            check_equal(os.path.join(timestamp1, 'system1.json'), t1s1_dict)
            check_equal(os.path.join(timestamp1, 'system2.json'), s2_dict)
            check_equal(os.path.join(timestamp2, 'system1.json'), t2s1_dict)
            check_equal(os.path.join(timestamp2, 'system2.json'), s2_dict)

    def test_diff(self):
        data = {'timestamp1': {'system1': {'tests': [
            TaskFeatures(suite='suite', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list --all"),
                               Cmd(cmd="snap ack file")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack file"),
                               Cmd(cmd="snap debug api")],
                         endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")])]},
            'system2': {'tests': []}},
            'timestamp2': {'system1': {'tests': [
                TaskFeatures(suite='suite', task_name='task1', success=False, variant='',
                             cmds=[
                                 Cmd(cmd="snap list --all")],
                             endpoints=[Endpoint(
                                 method="GET", path="/v2/changes/{id}"), Endpoint(method="GET", path="/v2/snaps")],
                             changes=[Change(kind="install-snap", snap_types=["app"])])]},
                           'system2': {'tests': []}}}
        retriever = DictRetriever(data)

        # When getting difference only between the same tasks in both systems,
        # the only difference is in suite:task1
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp2', 'system1', False, True)
        assert {"cmds":[Cmd(cmd="snap ack file")]} == diff
        diff = query_features.diff(retriever, 'timestamp2', 'system1', 'timestamp1', 'system1', False, True)
        assert {"endpoints":[Endpoint(method="GET", path="/v2/changes/{id}")]} == diff

        # When getting difference only between the same tasks in both systems,
        # and also removing failed tasks, then there are no tasks features to compare
        # since suite:task1 failed on timestamp2
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp2', 'system1', True, True)
        assert {} == diff
        diff = query_features.diff(retriever, 'timestamp2', 'system1', 'timestamp1', 'system1', True, True)
        assert {} == diff

        # When getting all differences, suite:task2:v1, that isn't present
        # on the timestamp2 run, gets counted.
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp2', 'system1', False, False)
        assert {"cmds":[Cmd(cmd="snap ack file"),Cmd(cmd="snap pack file"),Cmd(cmd="snap debug api")],
                              "endpoints":[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]} == diff
        diff = query_features.diff(retriever, 'timestamp2', 'system1', 'timestamp1', 'system1', False, False)
        assert {"endpoints":[Endpoint(method="GET", path="/v2/changes/{id}")]} == diff

        # When removing all failed executions, the difference becomes
        # all features of suite:task1 and suite:task2:v1 in timestamp1
        # because suite:task1 failed in timestamp2 and so is removed.
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp2', 'system1', True, False)
        assert {
            "cmds":[Cmd(cmd="snap list --all"),Cmd(cmd="snap ack file"),Cmd(cmd="snap pack file"),Cmd(cmd="snap debug api")],
            "endpoints":[Endpoint(method="GET", path="/v2/snaps"),Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")],
            "changes":[Change(kind="install-snap", snap_types=["app"])]
        } == diff
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp1', 'system1', True, False)
        assert {} == diff

        # Empty dictionaries of tests have no feature difference
        diff = query_features.diff(retriever, 'timestamp1', 'system2', 'timestamp2', 'system2', False, False)
        assert {} == diff
        diff = query_features.diff(retriever, 'timestamp2', 'system2', 'timestamp1', 'system2', False, False)
        assert {} == diff

    def test_feat_sys(self):
        data = {'timestamp1': {'system': {'tests': [
            TaskFeatures(suite='suite1', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite2', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack"),
                               Cmd(cmd="snap debug api")],
                         endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]),
            TaskFeatures(suite='suite2', task_name='task1', success=False, variant='v1',
                         cmds=[Cmd(cmd="snap routine file-access")],
                         endpoints=[Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")]),
                         ]}}}
        retriever = DictRetriever(data)

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', False)
        expected = {'cmds':[cmd for entry in data['timestamp1']['system']['tests'] if 'cmds' in entry for cmd in entry['cmds']],
                    'endpoints':[endpt for entry in data['timestamp1']['system']['tests'] if 'endpoints' in entry for endpt in entry['endpoints']],
                    'changes':[change for entry in data['timestamp1']['system']['tests'] if 'changes' in entry for change in entry['changes']]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', True)
        expected = {'cmds':[Cmd(cmd="snap list"),Cmd(cmd="snap pack"),Cmd(cmd="snap debug api")],
                    'endpoints':[Endpoint(method="GET", path="/v2/snaps"),Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")],
                    'changes':[Change(kind="install-snap", snap_types=["app"])]}
        assert expected == cov

    def test_feat_sys_suite(self):
        data = {'timestamp1': {'system': {'tests': [
            TaskFeatures(suite='suite1', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite2', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack"),
                               Cmd(cmd="snap debug api")],
                         endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]),
            TaskFeatures(suite='suite2', task_name='task1', success=False, variant='v1',
                         cmds=[Cmd(cmd="snap routine file-access")],
                         endpoints=[Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")]),
                         ]}}}
        retriever = DictRetriever(data)

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', True, suite='suite1')
        expected = {'cmds':[Cmd(cmd="snap list")],
                    'endpoints':[Endpoint(method="GET", path="/v2/snaps")],
                    'changes':[Change(kind="install-snap", snap_types=["app"])]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', True, suite='suite2')
        expected = {'cmds':[Cmd(cmd="snap pack"), Cmd(cmd="snap debug api")],
                    'endpoints':[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', False, suite='suite2')
        expected = {'cmds':[Cmd(cmd="snap pack"), Cmd(cmd="snap debug api"), Cmd(cmd="snap routine file-access")],
                    'endpoints':[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove"),Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', False, suite='doesnotexist')
        assert {} == cov

    def test_feat_sys_task(self):
        data = {'timestamp1': {'system': {'tests': [
            TaskFeatures(suite='suite1', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite2', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack"),
                               Cmd(cmd="snap debug api")],
                         endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]),
            TaskFeatures(suite='suite2', task_name='task1', success=False, variant='v1',
                         cmds=[Cmd(cmd="snap routine file-access")],
                         endpoints=[Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")]),
                         ]}}}
        retriever = DictRetriever(data)

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', True, task='task1')
        expected = {'cmds':[Cmd(cmd="snap list")],
                    'endpoints':[Endpoint(method="GET", path="/v2/snaps")],
                    'changes':[Change(kind="install-snap", snap_types=["app"])]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', False, task='task1')
        expected = {'cmds':[Cmd(cmd="snap list"),Cmd(cmd="snap routine file-access")],
                    'endpoints':[Endpoint(method="GET", path="/v2/snaps"), Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")],
                    'changes':[Change(kind="install-snap", snap_types=["app"])]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', False, task='doesnotexist')
        assert {} == cov

    def test_feat_sys_variant(self):
        data = {'timestamp1': {'system': {'tests': [
            TaskFeatures(suite='suite1', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite2', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack"),
                               Cmd(cmd="snap debug api")],
                         endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]),
            TaskFeatures(suite='suite2', task_name='task1', success=False, variant='v1',
                         cmds=[Cmd(cmd="snap routine file-access")],
                         endpoints=[Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")]),
                         ]}}}
        retriever = DictRetriever(data)

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', True, variant='v1')
        expected = {'cmds':[Cmd(cmd="snap pack"),Cmd(cmd="snap debug api")],
                    'endpoints':[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', False, variant='v1')
        expected = {'cmds':[Cmd(cmd="snap pack"),Cmd(cmd="snap debug api"),Cmd(cmd="snap routine file-access")],
                    'endpoints':[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove"),Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")]}
        assert expected == cov

        cov = query_features.feat_sys(retriever, 'timestamp1', 'system', False, variant='doesnotexist')
        assert {} == cov

    def test_diff_feat_all(self):
        data = {'timestamp1': {'system': {'tests': [
            TaskFeatures(suite='suite1', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list")],
                         endpoints=[
                             Endpoint(method="GET", path="/v2/snaps")],
                         changes=[Change(kind="install-snap", snap_types=["app"])]),
            TaskFeatures(suite='suite2', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack"),
                               Cmd(cmd="snap debug api")],
                         endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]),
            TaskFeatures(suite='suite2', task_name='task1', success=False, variant='v1',
                         cmds=[Cmd(cmd="snap routine file-access")],
                         endpoints=[Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")]),
                         ]}}}
        all_features = {'timestamp1': {
            'cmds':[cmd for entry in data['timestamp1']['system']['tests'] if 'cmds' in entry for cmd in entry['cmds']],
            'endpoints':[endpt for entry in data['timestamp1']['system']['tests'] if 'endpoints' in entry for endpt in entry['endpoints']],
            'changes':[Change(kind="install-snap"),Change(kind="create-recovery-system")],
            'ensures':[Ensure(manager="SnapManager",function="myFunction")],
            'interfaces':[Interface(name="iface")],
            'tasks':[Task(kind="refresh", last_status="Done")]}}
        
        retriever = DictRetriever(data, all_features)

        diff = query_features.diff_all_features(retriever, 'timestamp1', 'system', False)
        expected = {
            'changes':[Change(kind="create-recovery-system")], 
            'ensures':all_features['timestamp1']['ensures'], 
            'interfaces': all_features['timestamp1']['interfaces'],
            'tasks': all_features['timestamp1']['tasks']}
        assert expected == diff

        diff = query_features.diff_all_features(retriever, 'timestamp1', 'system', True)
        expected = {
            'cmds':[Cmd(cmd="snap routine file-access")],
            'endpoints':[Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")],
            'changes':[Change(kind="create-recovery-system")],
            'ensures':all_features['timestamp1']['ensures'], 
            'interfaces': all_features['timestamp1']['interfaces'],
            'tasks': all_features['timestamp1']['tasks']}
        assert expected == diff

    def test_feat_find(self):
        data = {'timestamp1': {'system': {'system':'system', 'tests': [
            TaskFeatures(suite='suite1', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list")],
                         tasks=[Task(kind="a",last_status="Done",snap_types=["app"])],
                         ensures=[Ensure(manager="a",function="c")]),
            TaskFeatures(suite='suite2', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack"),Cmd(cmd="snap debug api")],
                         interfaces=[Interface(name="i",plug_snap_type="snapd",slot_snap_type="app")],
                         endpoints=[Endpoint(method="POST", path="/v", action="b")],
                         changes=[Change(kind="a",snap_types=["app","snapd"])]),
            TaskFeatures(suite='suite2', task_name='task1', success=False, variant='v1',
                         cmds=[Cmd(cmd="snap list")],
                         endpoints=[Endpoint(method="POST", path="/v", action="a")],
                         ensures=[Ensure(manager="a",function="b")]),
                         ]}}}
        
        retriever = DictRetriever(data)
        tests = query_features.find_feat(retriever, 'timestamp1', Cmd(cmd="snap list"), False)
        expected = {'system': [TaskIdVariant(suite='suite1',task_name='task1',variant=''),TaskIdVariant(suite='suite2',task_name='task1',variant='v1')]}
        assert json.dumps(expected, default=lambda x: str(x)) == json.dumps(tests, default=lambda x: str(x))

        tests = query_features.find_feat(retriever, 'timestamp1', Cmd(cmd="snap list"), True)
        expected = {'system': [TaskIdVariant(suite='suite1',task_name='task1',variant='')]}
        assert json.dumps(expected, default=lambda x: str(x)) == json.dumps(tests, default=lambda x: str(x))

        tests = query_features.find_feat(retriever, 'timestamp1', Task(kind='a',last_status='Done'), False)
        expected = {'system': [TaskIdVariant(suite='suite1',task_name='task1',variant='')]}
        assert json.dumps(expected, default=lambda x: str(x)) == json.dumps(tests, default=lambda x: str(x))

        tests = query_features.find_feat(retriever, 'timestamp1', Interface(name='i'), False)
        expected = {'system': [TaskIdVariant(suite='suite2',task_name='task2',variant='v1')]}
        assert json.dumps(expected, default=lambda x: str(x)) == json.dumps(tests, default=lambda x: str(x))

        tests = query_features.find_feat(retriever, 'timestamp1',Endpoint(method="POST", path="/v", action="a"), False)
        expected = {'system': [TaskIdVariant(suite='suite2',task_name='task1',variant='v1')]}
        assert json.dumps(expected, default=lambda x: str(x)) == json.dumps(tests, default=lambda x: str(x))

        tests = query_features.find_feat(retriever, 'timestamp1',Ensure(manager="a",function="b"), False)
        expected = {'system': [TaskIdVariant(suite='suite2',task_name='task1',variant='v1')]}
        assert json.dumps(expected, default=lambda x: str(x)) == json.dumps(tests, default=lambda x: str(x))

        tests = query_features.find_feat(retriever, 'timestamp1',Change(kind="a"), False)
        expected = {'system': [TaskIdVariant(suite='suite2',task_name='task2',variant='v1')]}
        assert json.dumps(expected, default=lambda x: str(x)) == json.dumps(tests, default=lambda x: str(x))


    def test_task_list(self):
        data = {'timestamp1': {'system': {'system':'system', 'tests': [
            TaskFeatures(suite='suite1', task_name='task1', success=True, variant='',
                         cmds=[Cmd(cmd="snap list")]),
            TaskFeatures(suite='suite2', task_name='task2', success=True, variant='v1',
                         cmds=[Cmd(cmd="snap pack"),Cmd(cmd="snap debug api")]),
            TaskFeatures(suite='suite2', task_name='task1', success=False, variant='v1',
                         cmds=[Cmd(cmd="snap list")]),
                         ]}}}
        
        retriever = DictRetriever(data)
        tasks = query_features.task_list(retriever, 'timestamp1')
        assert len(tasks) == 3
        assert TaskIdVariant(suite='suite1',task_name='task1',variant='') in tasks
        assert TaskIdVariant(suite='suite2',task_name='task2',variant='v1') in tasks
        assert TaskIdVariant(suite='suite2',task_name='task1',variant='v1') in tasks



    @patch('argparse.ArgumentParser.parse_args')
    def test_dirretriever_list(self, parse_args_mock: Mock):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1'},
            {'timestamp': '2025-05-04', 'system': 'system2'},
            {'timestamp': '2025-05-05', 'system': 'system2'}
        ]
        with DirMocker(data, do_patch_stdout=True) as dm:
            parse_args_mock.return_value = argparse.Namespace(
                command='list',
                file=None,
                dir=dm.get_dir()
            )
            query_features.main()
            actual = json.loads(dm.get_stdout())
            assert 2 == len(actual)
            in_actual = {'timestamp': '2025-05-04', 'systems': ['system1', 'system2']} in actual \
                or {'timestamp': '2025-05-04', 'systems': ['system2', 'system1']} in actual
            assert in_actual
            assert {'timestamp': '2025-05-05','systems': ['system2']} in actual


    @patch('argparse.ArgumentParser.parse_args')
    def test_mongoretriever_list(self, parse_args_mock: Mock):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1'},
            {'timestamp': '2025-05-04', 'system': 'system2'},
            {'timestamp': '2025-05-05', 'system': 'system2'}
        ]
        with MongoMocker(data, do_patch_stdout=True) as mm:
            parse_args_mock.return_value = argparse.Namespace(
                command='list',
                file=StringIO(''),
                dir=None
            )
            query_features.main()
            actual = json.loads(mm.get_stdout())
            assert 2, len(actual)
            in_actual = {'timestamp': '2025-05-04T00:00:00', 'systems': ['system1', 'system2']} in actual \
                or {'timestamp': '2025-05-04T00:00:00', 'systems': ['system2', 'system1']} in actual
            assert in_actual
            assert {'timestamp': '2025-05-05T00:00:00', 'systems': ['system2']} in actual


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_diff_systems(self, parse_args_mock: Mock, mocker_class: str):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'b'}], 'endpoints': [{'1': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'endpoints': [{'5': 'd'}]},
            ]},
            {'timestamp': '2025-05-05', 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'c'}], 'endpoints': [{'1': 'b'}, {'2': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'tasks': [{'task': 'a'}]},
                {'cmds': [{'cmd': 'e'}], 'endpoints': [{'5': 'd'}]}
            ]}
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data, do_patch_stdout=True) as mocker:
            parse_args_mock.return_value = argparse.Namespace(
                command='diff',
                diff_cmd='systems',
                file=StringIO('') if mocker_class == "MongoMocker" else None,
                dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                timestamp1='2025-05-04',
                system1='system',
                timestamp2='2025-05-05',
                system2='system',
                remove_failed=False,
                only_same=False
            )
            query_features.main()
            expected = {'cmds': [{'cmd': 'b'}], 'endpoints': [{'1': 'a'}]}
            actual = json.loads(mocker.get_stdout())
            assert expected == actual


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_diff_all(self, parse_args_mock: Mock, mocker_class: str):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system', 'tests': [
                {'cmds': [Cmd(cmd='a'), Cmd(cmd='b')], 'endpoints': [Endpoint(method='a',path='/a')]},
                {'cmds': [Cmd(cmd='d')], 'endpoints': [Endpoint(method='b', path='/b', action='b')]},
                {'tasks': [Task(kind='a',last_status='Done',snap_types=['a'])], 'changes': [Change(kind='change')],},
                {'interfaces': [Interface(name='iface')],'ensures': [Ensure(manager='mgr',function='func')]}
            ]},
            {'timestamp': '2025-05-05', 'system': 'system', 'tests': [
                {'cmds': [Cmd(cmd='a'), Cmd(cmd='c')], 'endpoints': [Endpoint(method='c',path='/c'),Endpoint(method='d',path='/d',action='d')]},
                {'cmds': [Cmd(cmd='d')], 'tasks': [Task(kind='a',last_status='Done',snap_types=['a'])]},
                {'cmds': [Cmd(cmd='e')], 'endpoints': [Endpoint(method='c',path='/c')]}
            ]},
            {'timestamp': '2025-05-04', 'all_features': True,
                'cmds': [Cmd(cmd='a'), Cmd(cmd='b'), Cmd(cmd='c'), Cmd(cmd='d'), Cmd(cmd='e'), Cmd(cmd='f')], 
                'endpoints': [Endpoint(method='a',path='/a'),
                              Endpoint(method='b', path='/b', action='b'),
                              Endpoint(method='c',path='/c'),
                              Endpoint(method='d',path='/d',action='d')],
                'tasks': [Task(kind='a',last_status='Done'),Task(kind='a',last_status='Error')],
                'interfaces': [Interface(name='iface')],
                'changes': [Change(kind='change')],
                'ensures': [Ensure(manager='mgr',function='func')]
            }
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data, do_patch_stdout=True) as mocker:
            parse_args_mock.return_value = argparse.Namespace(
                command='diff',
                diff_cmd='all-features',
                file=StringIO('') if mocker_class == "MongoMocker" else None,
                dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                timestamp='2025-05-04',
                system='system',
                remove_failed=False
            )
            query_features.main()
            expected = {'cmds': [Cmd(cmd='c'),Cmd(cmd='e'),Cmd(cmd='f')],
                        'endpoints': [Endpoint(method='c',path='/c'),Endpoint(method='d',path='/d',action='d')],
                        'tasks': [Task(kind='a',last_status='Error')]}
            actual = json.loads(mocker.get_stdout())
            assert expected == actual


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_dup(self, parse_args_mock: Mock, mocker_class: str):
        data = [{'timestamp': '2025-05-04', 'system': 'system', 'tests': [
            TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}]),
            TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}]),
            TaskFeatures(task_name='task3', suite='suite2', variant='', cmds=[{'cmd': 'd'}]),
            TaskFeatures(task_name='task4', suite='suite1', variant='v1', endpoints=[{'1': 'a'}])
        ]}
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data, do_patch_stdout=True) as mocker:
            parse_args_mock.return_value = argparse.Namespace(
                command='dup',
                file=StringIO('') if mocker_class == "MongoMocker" else None,
                dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                timestamp='2025-05-04',
                system='system',
                remove_failed=False,
            )
            query_features.main()
            actual = json.loads(mocker.get_stdout())
            assert 2 == len(actual)
            assert 'suite2:task3' in actual
            assert 'suite1:task4:v1' in actual


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_export(self, parse_args_mock: Mock, mocker_class: str):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}])
            ]},
            {'timestamp': '2025-05-05', 'system': 'system2', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'c'}, {'cmd': 'd'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'2': 'q'}])
            ]},
            {'timestamp': '2025-05-06', 'system': 'system3', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}])
            ]},
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data) as mocker:
            with tempfile.TemporaryDirectory() as tmpdir:
                parse_args_mock.return_value = argparse.Namespace(
                    command='export',
                    file=StringIO('') if mocker_class == "MongoMocker" else None,
                    dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                    timestamps=['2025-05-04', '2025-05-05'],
                    systems=None,
                    output=tmpdir,
                )
                with patch('sys.stderr', new=StringIO()) as stderr_patch:
                    query_features.main()
                    assert stderr_patch.getvalue().startswith('could not find all features at timestamp 2025-05-04')

                assert os.path.isdir(os.path.join(tmpdir, '2025-05-04'))
                assert os.path.isdir(os.path.join(tmpdir, '2025-05-05'))
                assert os.path.isfile(os.path.join(tmpdir, '2025-05-04', 'system1.json'))
                assert os.path.isfile(os.path.join(tmpdir, '2025-05-05', 'system2.json'))


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_export_with_all(self, parse_args_mock: Mock, mocker_class: str):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}])
            ]},
            {'timestamp': '2025-05-05', 'system': 'system2', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'c'}, {'cmd': 'd'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'2': 'q'}])
            ]},
            {'timestamp': '2025-05-06', 'system': 'system3', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}])
            ]},
            {'timestamp': '2025-05-04', 'all_features': True, 'cmds': [{'cmd': 'a'},{'cmd': 'b'},{'cmd': 'c'},{'cmd': 'd'}]},
            {'timestamp': '2025-05-05', 'all_features': True, 'cmds': [{'cmd': 'a'},{'cmd': 'b'},{'cmd': 'c'},{'cmd': 'd'}]},
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data) as mocker:
            with tempfile.TemporaryDirectory() as tmpdir:
                parse_args_mock.return_value = argparse.Namespace(
                    command='export',
                    file=StringIO('') if mocker_class == "MongoMocker" else None,
                    dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                    timestamps=['2025-05-04', '2025-05-05'],
                    systems=None,
                    output=tmpdir,
                )
                query_features.main()

                assert os.path.isdir(os.path.join(tmpdir, '2025-05-04'))
                assert os.path.isdir(os.path.join(tmpdir, '2025-05-05'))
                assert os.path.isfile(os.path.join(tmpdir, '2025-05-04', 'system1.json'))
                assert os.path.isfile(os.path.join(tmpdir, '2025-05-05', 'system2.json'))
                assert os.path.isfile(os.path.join(tmpdir, '2025-05-04', 'all-features.json'))
                assert os.path.isfile(os.path.join(tmpdir, '2025-05-05', 'all-features.json'))


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_feat_sys(self, parse_args_mock: Mock, mocker_class: str):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}], success=True),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}], success=True),
                TaskFeatures(task_name='task3', suite='suite2', variant='', cmds=[{'cmd': 'c'}], success=False)
            ]}
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data, do_patch_stdout=True) as mocker:
            parse_args_mock.return_value = argparse.Namespace(
                command='feat',
                features_cmd='sys',
                file=StringIO('') if mocker_class == "MongoMocker" else None,
                dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                timestamp='2025-05-04',
                system='system1',
                suite=None,
                task=None,
                variant=None,
                remove_failed=True
            )
            query_features.main()

            output = json.loads(mocker.get_stdout())
            expected = {'cmds':[{'cmd':'a'},{'cmd':'b'},{'cmd':'d'}],
                        'endpoints':[{'1':'a'},{'5':'d'}]}
            assert expected == output


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_feat_all(self, parse_args_mock: Mock, mocker_class: str):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'b'}], 'endpoints': [{'1': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'endpoints': [{'5': 'd'}]},
            ]},
            {'timestamp': '2025-05-04', 'all_features': True,
                'cmds': [{'cmd': 'a'}, {'cmd': 'b'}, {'cmd': 'c'}, {'cmd': 'd'}, {'cmd': 'e'}, {'cmd': 'f'}], 
                'endpoints': [{'1': 'a'},{'1': 'b'},{'2': 'a'},{'5': 'd'}]
            }
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data, do_patch_stdout=True) as mocker:
            parse_args_mock.return_value = argparse.Namespace(
                command='feat',
                features_cmd='all',
                file=StringIO('') if mocker_class == "MongoMocker" else None,
                dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                timestamp='2025-05-04',
                system='system1',
                remove_failed=True
            )
            query_features.main()

            output = json.loads(mocker.get_stdout())
            expected = {'cmds':data[1]['cmds'],
                        'endpoints':data[1]['endpoints']}
            assert expected == output


    @pytest.mark.parametrize("mocker_class", ["MongoMocker","DirMocker"])
    @patch('argparse.ArgumentParser.parse_args')
    def test_retriever_feat_find(self, parse_args_mock: Mock, mocker_class: str):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1', 'tests': [
                TaskFeatures(success=True, task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(success=True, task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'2': 'q'}])
            ]},
            {'timestamp': '2025-05-04', 'system': 'system2', 'tests': [
                TaskFeatures(success=True, task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'c'}, {'cmd': 'd'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(success=False, task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'2': 'q'}])
            ]},
            {'timestamp': '2025-05-04', 'all_features': True,
                'cmds': [{'cmd': 'a'}, {'cmd': 'b'}, {'cmd': 'c'}, {'cmd': 'd'}, {'cmd': 'e'}, {'cmd': 'f'}], 
                'endpoints': [{'1': 'a'},{'1': 'b'},{'2': 'a'},{'5': 'd'}]
            }
        ]
        Mocker = globals()[mocker_class]
        with Mocker(data, do_patch_stdout=True) as mocker:
            parse_args_mock.return_value = argparse.Namespace(
                command='feat',
                features_cmd='find',
                file=StringIO('') if mocker_class == "MongoMocker" else None,
                dir=mocker.get_dir() if mocker_class == "DirMocker" else None,
                timestamp='2025-05-04',
                feat='{"cmd":"d"}',
                system=None,
                remove_failed=True
            )
            query_features.main()

            output = json.loads(mocker.get_stdout())
            expected = {'system1':[str(TaskIdVariant(suite='suite1',task_name='task2',variant=''))],
                        'system2':[str(TaskIdVariant(suite='suite1',task_name='task1',variant=''))]}
            assert expected == output
