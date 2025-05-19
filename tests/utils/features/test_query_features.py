
import json
import os
from pathlib import Path
import sys
import tempfile
from typing import Iterable
import unittest
# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import query_features
from features import SystemFeatures, TaskFeatures, Cmd, Endpoint, Change


class DictRetriever(query_features.Retriever):
    def __init__(self, data):
        self.data = data

    def get_sorted_timestamps_and_systems(self) -> list[dict[str, list[str]]]:
        pass

    def get_single_json(self, timestamp: str, system: str) -> SystemFeatures:
        return self.data[timestamp][system]

    def get_systems(self, timestamp: str, systems: list[str]) -> Iterable[SystemFeatures]:
        if systems:
            return [self.data[timestamp][system] for system in systems]
        else:
            return [data for _, data in self.data[timestamp].items()]


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
        j = {"tests":[
            {"task_name":"task1",
             "cmds": [{"cmd":"snap list --all"},{"cmd":"snap ack file"},],
             "ensures": [{"manager":"SnapManager","functions":[]}]},
            {"task_name":"task2",
             "cmds": [{"cmd":"snap do things"},{"cmd":"snap list --all"}],
             "ensures": [{"manager":"SnapManager","functions":["ensureThings"]}]
            }
        ]}
        c = query_features.consolidate_system_features(j)
        self.assertEqual(len(c), 2)
        self.assertTrue("cmds" in c)
        self.assertEqual(len(c["cmds"]), 3)
        self.assertTrue({"cmd":"snap list --all"} in c["cmds"])
        self.assertTrue({"cmd":"snap ack file"} in c["cmds"])
        self.assertTrue({"cmd":"snap do things"} in c["cmds"])
        self.assertTrue("ensures" in c)
        self.assertEqual(len(c["ensures"]), 2)
        self.assertTrue({"manager":"SnapManager","functions":[]} in c["ensures"])
        self.assertTrue({"manager":"SnapManager","functions":["ensureThings"]} in c["ensures"])


    def test_consolidate_features_exclude_task(self):
        j = {"tests":[
            {"suite":"suite","task_name":"task1","variant":"a",
             "cmds": [{"cmd":"snap list --all"},{"cmd":"snap ack file"},],
             "ensures": [{"manager":"SnapManager","functions":[]}]},
            {"suite":"suite","task_name":"task2","variant":"",
             "cmds": [{"cmd":"snap do things"},{"cmd":"snap list --all"}],
             "ensures": [{"manager":"SnapManager","functions":["ensureThings"]}]
            }
        ]}
        c = query_features.consolidate_system_features(j, exclude_tasks=[query_features.TaskId(suite='suite',task_name="task1")])
        self.assertEqual(len(c), 2)
        self.assertTrue("cmds" in c)
        self.assertEqual(len(c["cmds"]), 2)
        self.assertTrue({"cmd":"snap list --all"} in c["cmds"])
        self.assertTrue({"cmd":"snap do things"} in c["cmds"])
        self.assertTrue("ensures" in c)
        self.assertEqual(len(c["ensures"]), 1)
        self.assertTrue({"manager":"SnapManager","functions":["ensureThings"]} in c["ensures"])


    def test_consolidate_features_include_task(self):
        j = {"tests":[
            {"suite":"suite","task_name":"task1","variant":"a",
             "cmds": [{"cmd":"snap list --all"},{"cmd":"snap ack file"},],
             "ensures": [{"manager":"SnapManager","functions":[]}]},
            {"suite":"suite","task_name":"task2","variant":"",
             "cmds": [{"cmd":"snap do things"},{"cmd":"snap list --all"}],
             "ensures": [{"manager":"SnapManager","functions":["ensureThings"]}]
            }
        ]}
        c = query_features.consolidate_system_features(j, include_tasks=[query_features.TaskId(suite='suite',task_name="task2")])
        self.assertEqual(len(c), 2)
        self.assertTrue("cmds" in c)
        self.assertEqual(len(c["cmds"]), 2)
        self.assertTrue({"cmd":"snap list --all"} in c["cmds"])
        self.assertTrue({"cmd":"snap do things"} in c["cmds"])
        self.assertTrue("ensures" in c)
        self.assertEqual(len(c["ensures"]), 1)
        self.assertTrue({"manager":"SnapManager","functions":["ensureThings"]} in c["ensures"])


    def test_features_minus(self):
        j = {"cmds": [{"cmd":"snap list --all"},{"cmd":"snap ack file"},],
             "ensures": [{"manager":"SnapManager","functions":[]}],
            }
        k = {"cmds": [{"cmd":"snap list --all"}],
             "ensures": [{"manager":"SnapManager","functions":["ensureFunc"]}],
            }
        minus = query_features.minus(j, k)
        self.assertEqual(len(minus), 2)
        self.assertTrue("cmds" in minus)
        self.assertTrue("ensures" in minus)
        self.assertEqual(len(minus["cmds"]), 1)
        self.assertTrue({"cmd":"snap ack file"} in minus["cmds"])
        self.assertEqual(len(minus["ensures"]), 1)
        self.assertTrue({"manager":"SnapManager","functions":[]} in minus["ensures"])


    def test_list_tasks(self):
        sys_json = SystemFeatures(tests=[
            TaskFeatures(success=True,task_name='task1',variant='variant1',suite='suite1'),
            TaskFeatures(success=False,task_name='task2',variant='variant2',suite='suite2'),
            ])
        tasks_all = query_features.list_tasks(sys_json, False)
        tasks_success = query_features.list_tasks(sys_json, True)
        self.assertSetEqual({query_features.TaskIdVariant('suite1','task1','variant1'), 
                             query_features.TaskIdVariant('suite2','task2','variant2')}, tasks_all)
        self.assertSetEqual({query_features.TaskIdVariant('suite1','task1','variant1')}, tasks_success)


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
        data = {'timestamp1':{'system1':{'tests':[
                                    TaskFeatures(suite='suite', task_name='task1', success=True, variant='',
                                                 cmds=[Cmd(cmd="snap list --all"),Cmd(cmd="snap ack file")],
                                                 endpoints=[Endpoint(method="GET", path="/v2/snaps")],
                                                 changes=[Change(kind="install-snap", snap_types=["app"])]),
                                    TaskFeatures(suite='suite', task_name='task2', success=True, variant='v1',
                                                 cmds=[Cmd(cmd="snap pack file"),Cmd(cmd="snap debug api")],
                                                 endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")]),
                                    TaskFeatures(suite='suite', task_name='task3', success=False, variant='v2',
                                                 cmds=[Cmd(cmd="snap pack file")],
                                                 endpoints=[Endpoint(method="GET", path="/v2/snaps")]),]}}}
        retriever = DictRetriever(data)
        dup = query_features.dup(retriever, 'timestamp1','system1', False)
        self.assertListEqual([query_features.TaskIdVariant(suite='suite',task_name='task3',variant='v2')], dup)
        dup = query_features.dup(retriever, 'timestamp1','system1', True)
        self.assertListEqual([], dup)


    def test_dup_variants(self):
        data = {'timestamp1':{'system1':{'tests':[
                                    TaskFeatures(suite='suite', task_name='task1', success=True, variant='a',
                                                 cmds=[Cmd(cmd="snap list --all"),Cmd(cmd="snap ack file")],
                                                 endpoints=[Endpoint(method="GET", path="/v2/snaps")],
                                                 changes=[Change(kind="install-snap", snap_types=["app"])]),
                                    TaskFeatures(suite='suite', task_name='task1', success=True, variant='b',
                                                 cmds=[Cmd(cmd="snap list --all"),Cmd(cmd="snap ack file")],
                                                 endpoints=[Endpoint(method="GET", path="/v2/snaps")],
                                                 changes=[Change(kind="install-snap", snap_types=["app"])]),
                                    TaskFeatures(suite='suite', task_name='task3', success=False, variant='v2',
                                                 endpoints=[Endpoint(method="GET", path="/v2/snaps")]),]}}}
        retriever = DictRetriever(data)
        dup = query_features.dup(retriever, 'timestamp1','system1', False)
        # Features from variants of the same test should not influence
        # duplicate calculation. The only duplicate task should be task3
        self.assertListEqual([query_features.TaskIdVariant(suite='suite',task_name='task3',variant='v2')], dup)
        dup = query_features.dup(retriever, 'timestamp1','system1', True)
        self.assertListEqual([], dup)

    
    def test_export(self):
        t1s1_dict = SystemFeatures(system='system1', tests=[
                                    TaskFeatures(suite='suite', task_name='task1', success=True, variant=''),
                                    TaskFeatures(suite='suite', task_name='task2', success=True, variant='v1')])
        t2s1_dict = SystemFeatures(system='system1', tests=[
                                    TaskFeatures(suite='suite', task_name='task1', success=False, variant='')])
        s2_dict = SystemFeatures(system='system2')
        data = {'timestamp1':{'system1': t1s1_dict,
                              'system2': s2_dict},
                'timestamp2':{'system1': t2s1_dict,
                              'system2': s2_dict,}}
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
        data = {'timestamp1':{'system1':{'tests':[
                                    TaskFeatures(suite='suite', task_name='task1', success=True, variant='',
                                                 cmds=[Cmd(cmd="snap list --all"),Cmd(cmd="snap ack file")],
                                                 endpoints=[Endpoint(method="GET", path="/v2/snaps")],
                                                 changes=[Change(kind="install-snap", snap_types=["app"])]),
                                    TaskFeatures(suite='suite', task_name='task2', success=True, variant='v1',
                                                 cmds=[Cmd(cmd="snap pack file"),Cmd(cmd="snap debug api")],
                                                 endpoints=[Endpoint(method="POST", path="/v2/snaps/{name}", action="remove")])]},
                              'system2':{'tests':[]}},
                'timestamp2':{'system1':{'tests':[
                                    TaskFeatures(suite='suite', task_name='task1', success=False, variant='',
                                                 cmds=[Cmd(cmd="snap list --all")],
                                                 endpoints=[Endpoint(method="GET", path="/v2/changes/{id}"),Endpoint(method="GET", path="/v2/snaps")],
                                                 changes=[Change(kind="install-snap", snap_types=["app"])])]},
                              'system2':{'tests':[]}}}
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
        
        

if __name__ == '__main__':
    unittest.main()
