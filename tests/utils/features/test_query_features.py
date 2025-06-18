
import argparse
from datetime import datetime
from io import StringIO
import json
import os
from pathlib import Path
import sys
import tempfile
from typing import Iterable
import unittest
from unittest.mock import Mock, patch
# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import query_features
from features import SystemFeatures, TaskFeatures, Cmd, Endpoint, Change


class DictRetriever(query_features.Retriever):
    def __init__(self, data, all_features=None):
        self.data = data
        self.all_features = all_features

    def close(self):
        pass

    def get_sorted_timestamps_and_systems(self) -> list[dict[str, list[str]]]:
        pass

    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        return self.data[timestamp][system]

    def get_systems(self, timestamp: str, systems: list[str]) -> Iterable[SystemFeatures]:
        if systems:
            return [self.data[timestamp][system] for system in systems]
        else:
            return [data for _, data in self.data[timestamp].items()]

    def get_all_features(self, timestamp):
        return self.all_features[timestamp]


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


class TestQueryFeatures(unittest.TestCase):

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
            self.assertEqual(2, len(results))
            self.assertTrue({'timestamp':'timestamp2','systems':['system2']} in results)
            self.assertTrue({'timestamp':'timestamp1','systems':['system1','system2']} in results or
                            {'timestamp':'timestamp1','systems':['system2','system1']} in results)


    def test_dirretriever_get_systems(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.mkdir(os.path.join(tmpdir, 'timestamp1'))
            with open(os.path.join(tmpdir, 'timestamp1', 'system1.json'), 'w', encoding='utf-8') as f:
                json.dump(SystemFeatures(system='system1'), f)
            with open(os.path.join(tmpdir, 'timestamp1', 'system2.json'), 'w', encoding='utf-8') as f:
                json.dump(SystemFeatures(system='system2'), f)

            retriever = query_features.DirRetriever(tmpdir)
            results = list(retriever.get_systems('timestamp1'))
            self.assertEqual(2, len(results))
            self.assertIn(SystemFeatures(system='system1'), results)
            self.assertIn(SystemFeatures(system='system2'), results)
            results = list(retriever.get_systems('timestamp1', ['system1', 'system2']))
            self.assertEqual(2, len(results))
            self.assertIn(SystemFeatures(system='system1'), results)
            self.assertIn(SystemFeatures(system='system2'), results)
            results = list(retriever.get_systems('timestamp1', ['system1']))
            self.assertListEqual([SystemFeatures(system='system1')], results)

    def test_dirretriever_get_single_json(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.mkdir(os.path.join(tmpdir, 'timestamp1'))
            with open(os.path.join(tmpdir, 'timestamp1', 'system1.json'), 'w', encoding='utf-8') as f:
                json.dump(SystemFeatures(system='system1'), f)
            retriever = query_features.DirRetriever(tmpdir)
            result = retriever.get_single_json('timestamp1', 'system1')
            self.assertDictEqual(SystemFeatures(system='system1'), result)

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
        self.assertEqual(len(c), 2)
        self.assertTrue("cmds" in c)
        self.assertEqual(len(c["cmds"]), 3)
        self.assertTrue({"cmd": "snap list --all"} in c["cmds"])
        self.assertTrue({"cmd": "snap ack file"} in c["cmds"])
        self.assertTrue({"cmd": "snap do things"} in c["cmds"])
        self.assertTrue("ensures" in c)
        self.assertEqual(len(c["ensures"]), 2)
        self.assertTrue({"manager":"SnapManager","function":"ensureFunc1"} in c["ensures"])
        self.assertTrue({"manager":"SnapManager","function":"ensureFunc2"} in c["ensures"])


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
        self.assertEqual(len(c), 2)
        self.assertTrue("cmds" in c)
        self.assertEqual(len(c["cmds"]), 2)
        self.assertTrue({"cmd": "snap list --all"} in c["cmds"])
        self.assertTrue({"cmd": "snap do things"} in c["cmds"])
        self.assertTrue("ensures" in c)
        self.assertEqual(len(c["ensures"]), 1)
        self.assertTrue({"manager": "SnapManager", "function": "ensureFunc2"} in c["ensures"])

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
        self.assertEqual(len(c), 2)
        self.assertTrue("cmds" in c)
        self.assertEqual(len(c["cmds"]), 2)
        self.assertTrue({"cmd": "snap list --all"} in c["cmds"])
        self.assertTrue({"cmd": "snap do things"} in c["cmds"])
        self.assertTrue("ensures" in c)
        self.assertEqual(len(c["ensures"]), 1)
        self.assertTrue({"manager": "SnapManager", "function": "ensureFunc2"} in c["ensures"])

    def test_features_minus(self):
        j = {"cmds": [{"cmd": "snap list --all"}, {"cmd": "snap ack file"},],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc1"}],
             }
        k = {"cmds": [{"cmd": "snap list --all"}],
             "ensures": [{"manager": "SnapManager", "function": "ensureFunc2"}],
             }
        minus = query_features.minus(j, k)
        self.assertEqual(len(minus), 2)
        self.assertTrue("cmds" in minus)
        self.assertTrue("ensures" in minus)
        self.assertEqual(len(minus["cmds"]), 1)
        self.assertTrue({"cmd": "snap ack file"} in minus["cmds"])
        self.assertEqual(len(minus["ensures"]), 1)
        self.assertTrue({"manager": "SnapManager",
                        "function": "ensureFunc1"} in minus["ensures"])

    def test_list_tasks(self):
        sys_json = SystemFeatures(tests=[
            TaskFeatures(success=True,task_name='task1',variant='variant1',suite='suite1'),
            TaskFeatures(success=False,task_name='task2',variant='variant2',suite='suite2'),
        ])
        tasks_all = query_features.list_tasks(sys_json, False)
        tasks_success = query_features.list_tasks(sys_json, True)
        self.assertSetEqual({query_features.TaskIdVariant('suite1', 'task1', 'variant1'),
                             query_features.TaskIdVariant('suite2', 'task2', 'variant2')}, tasks_all)
        self.assertSetEqual({query_features.TaskIdVariant('suite1', 'task1', 'variant1')}, tasks_success)

    def test_list_tasks_empty(self):
        sys_json = SystemFeatures(tests=[])
        tasks = query_features.list_tasks(sys_json, False)
        self.assertSetEqual(set(), tasks)

    def test_check_dup_none(self):
        system_json = SystemFeatures(tests=[
            TaskFeatures(suite='suite',task_name='task1',variant='a',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task1',variant='b',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task2',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task3',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task4',variant='b',cmds=[Cmd(cmd='cmd1')]),
        ])
        dups = query_features.check_duplicate((system_json['tests'][0], system_json))
        self.assertIsNone(dups)
        dups = query_features.check_duplicate((system_json['tests'][1], system_json))
        self.assertIsNone(dups)

    def test_check_dup_no_variant(self):
        system_json = SystemFeatures(tests=[
            TaskFeatures(suite='suite',task_name='task1',variant='a',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task1',variant='b',cmds=[Cmd(cmd='cmd1'),Cmd(cmd='cmd2')]),
            TaskFeatures(suite='suite',task_name='task2',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task3',variant='',endpoints=[Endpoint(method='GET', path='/v2/snaps')]),
            TaskFeatures(suite='suite',task_name='task4',variant='b',cmds=[Cmd(cmd='cmd1')]),
        ])
        dups = query_features.check_duplicate((system_json['tests'][2], system_json))
        self.assertEqual(query_features.TaskIdVariant(suite='suite',task_name='task2',variant=''), dups)
        dups = query_features.check_duplicate((system_json['tests'][3], system_json))
        self.assertEqual(query_features.TaskIdVariant(suite='suite',task_name='task3',variant=''), dups)
        dups = query_features.check_duplicate((system_json['tests'][4], system_json))
        self.assertEqual(query_features.TaskIdVariant(suite='suite',task_name='task4',variant='b'), dups)


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
        self.assertListEqual([query_features.TaskIdVariant(suite='suite', task_name='task3', variant='v2')], dup)
        dup = query_features.dup(retriever, 'timestamp1', 'system1', True)
        self.assertListEqual([], dup)

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
        self.assertListEqual([query_features.TaskIdVariant(
            suite='suite', task_name='task3', variant='v2')], dup)
        dup = query_features.dup(retriever, 'timestamp1', 'system1', True)
        self.assertListEqual([], dup)

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
            self.assertTrue(os.path.isfile(file))
            with open(file, 'r', encoding='utf-8') as f:
                self.assertDictEqual(ref_dict, json.load(f))

        with tempfile.TemporaryDirectory() as tmpdir:
            query_features.export(retriever, tmpdir, ['timestamp1', 'timestamp2'], None)
            timestamp1 = os.path.join(tmpdir, 'timestamp1')
            timestamp2 = os.path.join(tmpdir, 'timestamp2')
            self.assertTrue(os.path.isdir(timestamp1))
            self.assertTrue(os.path.isdir(timestamp2))
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
        self.assertDictEqual({"cmds":[Cmd(cmd="snap ack file")]}, diff)
        diff = query_features.diff(retriever, 'timestamp2', 'system1', 'timestamp1', 'system1', False, True)
        self.assertDictEqual({"endpoints":[Endpoint(method="GET", path="/v2/changes/{id}")]}, diff)

        # When getting difference only between the same tasks in both systems,
        # and also removing failed tasks, then there are no tasks features to compare
        # since suite:task1 failed on timestamp2
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp2', 'system1', True, True)
        self.assertDictEqual({}, diff)
        diff = query_features.diff(retriever, 'timestamp2', 'system1', 'timestamp1', 'system1', True, True)
        self.assertDictEqual({}, diff)

        # When getting all differences, suite:task2:v1, that isn't present
        # on the timestamp2 run, gets counted.
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp2', 'system1', False, False)
        self.assertDictEqual({"cmds":[Cmd(cmd="snap ack file"),Cmd(cmd="snap pack file"),Cmd(cmd="snap debug api")],
                              "endpoints":[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]}, diff)
        diff = query_features.diff(retriever, 'timestamp2', 'system1', 'timestamp1', 'system1', False, False)
        self.assertDictEqual({"endpoints":[Endpoint(method="GET", path="/v2/changes/{id}")]}, diff)

        # When removing all failed executions, the difference becomes
        # all features of suite:task1 and suite:task2:v1 in timestamp1
        # because suite:task1 failed in timestamp2 and so is removed.
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp2', 'system1', True, False)
        self.assertDictEqual({
            "cmds":[Cmd(cmd="snap list --all"),Cmd(cmd="snap ack file"),Cmd(cmd="snap pack file"),Cmd(cmd="snap debug api")],
            "endpoints":[Endpoint(method="GET", path="/v2/snaps"),Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")],
            "changes":[Change(kind="install-snap", snap_types=["app"])]
        }, diff)
        diff = query_features.diff(retriever, 'timestamp1', 'system1', 'timestamp1', 'system1', True, False)
        self.assertDictEqual({}, diff)

        # Empty dictionaries of tests have no feature difference
        diff = query_features.diff(retriever, 'timestamp1', 'system2', 'timestamp2', 'system2', False, False)
        self.assertDictEqual({}, diff)
        diff = query_features.diff(retriever, 'timestamp2', 'system2', 'timestamp1', 'system2', False, False)
        self.assertDictEqual({}, diff)

    def test_cov(self):
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

        cov = query_features.cov(retriever, 'timestamp1', 'system', False)
        expected = {'cmds':[cmd for entry in data['timestamp1']['system']['tests'] if 'cmds' in entry for cmd in entry['cmds']],
                    'endpoints':[endpt for entry in data['timestamp1']['system']['tests'] if 'endpoints' in entry for endpt in entry['endpoints']],
                    'changes':[change for entry in data['timestamp1']['system']['tests'] if 'changes' in entry for change in entry['changes']]}
        self.assertDictEqual(expected, cov)

        cov = query_features.cov(retriever, 'timestamp1', 'system', True)
        expected = {'cmds':[Cmd(cmd="snap list"),Cmd(cmd="snap pack"),Cmd(cmd="snap debug api")],
                    'endpoints':[Endpoint(method="GET", path="/v2/snaps"),Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")],
                    'changes':[Change(kind="install-snap", snap_types=["app"])]}
        self.assertDictEqual(expected, cov)

    def test_diff_all_features(self):
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
            'changes':[Change(kind="install-snap", snap_types=["app"]),Change(kind="create-recovery-system", snap_types=[])]}}
        
        retriever = DictRetriever(data, all_features)

        diff = query_features.diff_all_features(retriever, 'timestamp1', 'system', False)
        expected = {'changes':[Change(kind="create-recovery-system", snap_types=[])]}
        self.assertDictEqual(expected, diff)

        diff = query_features.diff_all_features(retriever, 'timestamp1', 'system', True)
        expected = {
            'cmds':[Cmd(cmd="snap routine file-access")],
            'endpoints':[Endpoint(method="POST", path="/v2/system-info", action="advise-system-key-mismatch")],
            'changes':[Change(kind="create-recovery-system", snap_types=[])]}
        self.assertDictEqual(expected, diff)
        

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
            self.assertEqual(2, len(actual))
            in_actual = {'timestamp': '2025-05-04', 'systems': ['system1', 'system2']} in actual \
                or {'timestamp': '2025-05-04', 'systems': ['system2', 'system1']} in actual
            self.assertTrue(in_actual)
            self.assertIn({'timestamp': '2025-05-05','systems': ['system2']}, actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_mongoretriever_list(self, parse_args_mock: Mock):
        data = [
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'system': 'system1'},
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'system': 'system2'},
            {'timestamp': datetime.fromisoformat('2025-05-05'), 'system': 'system2'}
        ]
        with MongoMocker(data, do_patch_stdout=True) as mm:
            parse_args_mock.return_value = argparse.Namespace(
                command='list',
                file=StringIO(''),
                dir=None
            )
            query_features.main()
            actual = json.loads(mm.get_stdout())
            self.assertEqual(2, len(actual))
            in_actual = {'timestamp': '2025-05-04T00:00:00', 'systems': ['system1', 'system2']} in actual \
                or {'timestamp': '2025-05-04T00:00:00', 'systems': ['system2', 'system1']} in actual
            self.assertTrue(in_actual)
            self.assertIn({'timestamp': '2025-05-05T00:00:00', 'systems': ['system2']}, actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_mongoretriever_diff_systems(self, parse_args_mock: Mock):
        data = [
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'b'}], 'endpoints': [{'1': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'endpoints': [{'5': 'd'}]},
            ]},
            {'timestamp': datetime.fromisoformat('2025-05-05'), 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'c'}], 'endpoints': [{'1': 'b'}, {'2': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'tasks': [{'task': 'a'}]},
                {'cmds': [{'cmd': 'e'}], 'endpoints': [{'5': 'd'}]}
            ]}
        ]
        with MongoMocker(data, do_patch_stdout=True) as mm:
            parse_args_mock.return_value = argparse.Namespace(
                command='diff',
                diff_cmd='systems',
                file=StringIO(''),
                dir=None,
                timestamp1='2025-05-04',
                system1='system',
                timestamp2='2025-05-05',
                system2='system',
                remove_failed=False,
                only_same=False
            )
            query_features.main()
            expected = {'cmds': [{'cmd': 'b'}], 'endpoints': [{'1': 'a'}]}
            actual = json.loads(mm.get_stdout())
            self.assertDictEqual(expected, actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_dirretriever_diff_systems(self, parse_args_mock: Mock):
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
        with DirMocker(data, do_patch_stdout=True) as dm:
            parse_args_mock.return_value = argparse.Namespace(
                command='diff',
                diff_cmd='systems',
                file=None,
                dir=dm.get_dir(),
                timestamp1='2025-05-04',
                system1='system',
                timestamp2='2025-05-05',
                system2='system',
                remove_failed=False,
                only_same=False
            )
            query_features.main()
            expected = {'cmds': [{'cmd': 'b'}], 'endpoints': [{'1': 'a'}]}
            actual = json.loads(dm.get_stdout())
            self.assertDictEqual(expected, actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_mongoretriever_diff_all(self, parse_args_mock: Mock):
        data = [
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'b'}], 'endpoints': [{'1': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'endpoints': [{'5': 'd'}]},
            ]},
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'system2': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'c'}], 'endpoints': [{'1': 'b'}, {'2': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'tasks': [{'task': 'a'}]},
                {'cmds': [{'cmd': 'e'}], 'endpoints': [{'5': 'd'}]}
            ]},
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'all_features': True,
                'cmds': [{'cmd': 'a'}, {'cmd': 'b'}, {'cmd': 'c'}, {'cmd': 'd'}, {'cmd': 'e'}, {'cmd': 'f'}], 
                'endpoints': [{'1': 'a'},{'1': 'b'},{'2': 'a'},{'5': 'd'}]
            }
        ]
        with MongoMocker(data, do_patch_stdout=True) as mm:
            parse_args_mock.return_value = argparse.Namespace(
                command='diff',
                diff_cmd='all-features',
                file=StringIO(''),
                dir=None,
                timestamp='2025-05-04',
                system='system',
                remove_failed=False
            )
            query_features.main()
            expected = {'cmds': [{'cmd': 'c'},{'cmd': 'e'},{'cmd': 'f'}],
                        'endpoints': [{'1': 'b'},{'2': 'a'}]}
            actual = json.loads(mm.get_stdout())
            self.assertDictEqual(expected, actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_dirretriever_diff_all(self, parse_args_mock: Mock):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'b'}], 'endpoints': [{'1': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'endpoints': [{'5': 'd'}]},
            ]},
            {'timestamp': '2025-05-05', 'system': 'system', 'tests': [
                {'cmds': [{'cmd': 'a'}, {'cmd': 'c'}], 'endpoints': [{'1': 'b'}, {'2': 'a'}]},
                {'cmds': [{'cmd': 'd'}], 'tasks': [{'task': 'a'}]},
                {'cmds': [{'cmd': 'e'}], 'endpoints': [{'5': 'd'}]}
            ]},
            {'timestamp': '2025-05-04', 'all_features': True,
                'cmds': [{'cmd': 'a'}, {'cmd': 'b'}, {'cmd': 'c'}, {'cmd': 'd'}, {'cmd': 'e'}, {'cmd': 'f'}], 
                'endpoints': [{'1': 'a'},{'1': 'b'},{'2': 'a'},{'5': 'd'}]
            }
        ]
        with DirMocker(data, do_patch_stdout=True) as dm:
            parse_args_mock.return_value = argparse.Namespace(
                command='diff',
                diff_cmd='all-features',
                file=None,
                dir=dm.get_dir(),
                timestamp='2025-05-04',
                system='system',
                remove_failed=False
            )
            query_features.main()
            expected = {'cmds': [{'cmd': 'c'},{'cmd': 'e'},{'cmd': 'f'}],
                        'endpoints': [{'1': 'b'},{'2': 'a'}]}
            actual = json.loads(dm.get_stdout())
            self.assertDictEqual(expected, actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_mongoretriever_dup(self, parse_args_mock: Mock):
        data = [
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'system': 'system', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}]),
                TaskFeatures(task_name='task3', suite='suite2', variant='', cmds=[{'cmd': 'd'}]),
                TaskFeatures(task_name='task4', suite='suite1', variant='v1', endpoints=[{'1': 'a'}])
            ]}
        ]
        with MongoMocker(data, do_patch_stdout=True) as mm:
            parse_args_mock.return_value = argparse.Namespace(
                command='dup',
                file=StringIO(''),
                dir=None,
                timestamp='2025-05-04',
                system='system',
                remove_failed=False,
            )
            query_features.main()
            actual = json.loads(mm.get_stdout())
            self.assertEqual(2, len(actual))
            self.assertIn('suite2:task3:', actual)
            self.assertIn('suite1:task4:v1', actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_dirretriever_dup(self, parse_args_mock: Mock):
        data = [{'timestamp': '2025-05-04', 'system': 'system', 'tests': [
            TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}]),
            TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}]),
            TaskFeatures(task_name='task3', suite='suite2', variant='', cmds=[{'cmd': 'd'}]),
            TaskFeatures(task_name='task4', suite='suite1', variant='v1', endpoints=[{'1': 'a'}])
        ]}
        ]
        with DirMocker(data, do_patch_stdout=True) as dm:
            parse_args_mock.return_value = argparse.Namespace(
                command='dup',
                file=None,
                dir=dm.get_dir(),
                timestamp='2025-05-04',
                system='system',
                remove_failed=False,
            )
            query_features.main()
            actual = json.loads(dm.get_stdout())
            self.assertEqual(2, len(actual))
            self.assertIn('suite2:task3:', actual)
            self.assertIn('suite1:task4:v1', actual)

    @patch('argparse.ArgumentParser.parse_args')
    def test_mongoretriever_export(self, parse_args_mock: Mock):
        data = [
            {'timestamp': datetime.fromisoformat('2025-05-04'), 'system': 'system1', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}])
            ]},
            {'timestamp': datetime.fromisoformat('2025-05-05'), 'system': 'system2', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'c'}, {'cmd': 'd'}], endpoints=[{'1': 'a'}]),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'2': 'q'}])
            ]},
            {'timestamp': datetime.fromisoformat('2025-05-06'), 'system': 'system3', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}])
            ]},
        ]
        with MongoMocker(data):
            with tempfile.TemporaryDirectory() as tmpdir:
                parse_args_mock.return_value = argparse.Namespace(
                    command='export',
                    file=StringIO(''),
                    dir=None,
                    timestamps=['2025-05-04', '2025-05-05'],
                    systems=None,
                    output=tmpdir,
                )
                query_features.main()

                self.assertTrue(os.path.isdir(os.path.join(tmpdir, '2025-05-04')))
                self.assertTrue(os.path.isdir(os.path.join(tmpdir, '2025-05-05')))
                self.assertTrue(os.path.isfile(os.path.join(tmpdir, '2025-05-04', 'system1.json')))
                self.assertTrue(os.path.isfile(os.path.join(tmpdir, '2025-05-05', 'system2.json')))

    @patch('argparse.ArgumentParser.parse_args')
    def test_dirretriever_export(self, parse_args_mock: Mock):
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
        with DirMocker(data) as dm:
            with tempfile.TemporaryDirectory() as tmpdir:
                parse_args_mock.return_value = argparse.Namespace(
                    command='export',
                    file=None,
                    dir=dm.get_dir(),
                    timestamps=['2025-05-04', '2025-05-05'],
                    systems=None,
                    output=tmpdir,
                )
                query_features.main()

                self.assertTrue(os.path.isdir(os.path.join(tmpdir, '2025-05-04')))
                self.assertTrue(os.path.isdir(os.path.join(tmpdir, '2025-05-05')))
                self.assertTrue(os.path.isfile(os.path.join(tmpdir, '2025-05-04', 'system1.json')))
                self.assertTrue(os.path.isfile(os.path.join(tmpdir, '2025-05-05', 'system2.json')))

    @patch('argparse.ArgumentParser.parse_args')
    def test_mongoretriever_cov(self, parse_args_mock: Mock):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}], success=True),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}], success=True),
                TaskFeatures(task_name='task3', suite='suite2', variant='', cmds=[{'cmd': 'c'}], success=False)
            ]}
        ]
        with MongoMocker(data, do_patch_stdout=True) as dm:
            parse_args_mock.return_value = argparse.Namespace(
                command='cov',
                file=StringIO(''),
                dir=None,
                timestamp='2025-05-04',
                system='system1',
                remove_failed=True
            )
            query_features.main()

            output = json.loads(dm.get_stdout())
            expected = {'cmds':[{'cmd':'a'},{'cmd':'b'},{'cmd':'d'}],
                        'endpoints':[{'1':'a'},{'5':'d'}]}
            self.assertDictEqual(expected, output)

    @patch('argparse.ArgumentParser.parse_args')
    def test_dirretriever_cov(self, parse_args_mock: Mock):
        data = [
            {'timestamp': '2025-05-04', 'system': 'system1', 'tests': [
                TaskFeatures(task_name='task1', suite='suite1', variant='', cmds=[{'cmd': 'a'}, {'cmd': 'b'}], endpoints=[{'1': 'a'}], success=True),
                TaskFeatures(task_name='task2', suite='suite1', variant='', cmds=[{'cmd': 'd'}], endpoints=[{'5': 'd'}], success=True),
                TaskFeatures(task_name='task3', suite='suite2', variant='', cmds=[{'cmd': 'c'}], success=False)
            ]}
        ]
        with DirMocker(data, do_patch_stdout=True) as dm:
            with tempfile.TemporaryDirectory() as tmpdir:
                parse_args_mock.return_value = argparse.Namespace(
                    command='cov',
                    file=None,
                    dir=dm.get_dir(),
                    timestamp='2025-05-04',
                    system='system1',
                    remove_failed=True
                )
                query_features.main()

            output = json.loads(dm.get_stdout())
            expected = {'cmds':[{'cmd':'a'},{'cmd':'b'},{'cmd':'d'}],
                        'endpoints':[{'1':'a'},{'5':'d'}]}
            self.assertDictEqual(expected, output)


if __name__ == '__main__':
    unittest.main()
