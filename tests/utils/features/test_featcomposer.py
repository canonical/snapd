
import argparse
from io import StringIO
import json
import os
import tempfile
from typing import Any
import unittest
from unittest.mock import Mock, patch
import sys

# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import featcomposer
from features import *


class TestCompose(unittest.TestCase):
    @staticmethod
    def get_features(msg: str) -> dict[str, Any]:
        features = {}
        features['cmds'] = [Cmd(cmd=msg), Cmd(cmd=f'msg msg')]
        features['endpoints'] = [Endpoint(method='POST', path=msg)]
        features['interfaces'] = [Interface(name=msg)]
        features['tasks'] = [
            Task(kind=msg, snap_type='snap', last_status=Status.done)]
        features['changes'] = [Change(kind=msg, snap_type='snap')]
        return features

    @staticmethod
    def get_json(suite: str, task: str, variant: str, success: str, msg: str) -> TaskFeatures:
        features = TestCompose.get_features(msg)
        return TaskFeatures(
            suite=suite,
            task_name=task,
            variant=variant,
            success=success,
            cmds=features['cmds'],
            endpoints=features['endpoints'],
            interfaces=features['interfaces'],
            tasks=features['tasks'],
            changes=features['changes']
        )

    @staticmethod
    def write_task(filepath: str, msg: str) -> None:
        with open(filepath, 'w') as f:
            json.dump(TestCompose.get_features(msg), f)

    def test_compose(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            task1variant1 = os.path.join(
                tmpdir, 'backend:system.version:path--to--task1:variant1.json')
            task2 = os.path.join(tmpdir, 'backend:system.version:path--to--task2')
            TestCompose.write_task(task1variant1, 'task1variant1')
            TestCompose.write_task(task2, 'task2')
            systems = featcomposer.get_system_list(tmpdir)
            self.assertEqual(1, len(systems))
            composed = featcomposer.compose_system(tmpdir, systems.pop(),
                                                       'backend:system.version:path/to/task1:variant1 backend:system.version:another/task2',
                                                       ['e = 1 ', 'f = 2 '], ['1 ', ' 2', ' 3'])
            assert len(composed) == 5
            assert 'schema_version' in composed and composed['schema_version'] == '0.0.0'
            assert 'system' in composed and composed['system'] == 'backend:system.version'
            assert 'scenarios' in composed and len(composed['scenarios']) == 3
            assert '1' in composed['scenarios']
            assert '2' in composed['scenarios']
            assert '3' in composed['scenarios']
            assert 'env_variables' in composed and len(composed['env_variables']) == 2
            assert {'name': 'e', 'value': '1'} in composed['env_variables']
            assert {'name': 'f', 'value': '2'} in composed['env_variables']
            assert 'tests' in composed and len(composed['tests']) == 2
            assert TestCompose.get_json('path/to', 'task1', 'variant1', False, 'task1variant1') in composed['tests']
            assert TestCompose.get_json('path/to', 'task2', '', True, 'task2') in composed['tests']

    @patch('argparse.ArgumentParser.parse_args')
    def test_compose_features(self, parse_args_mock: Mock):
        with tempfile.TemporaryDirectory() as tmpdir:
            res = os.path.join(tmpdir, 'res')
            os.mkdir(res)
            sys1test1 = TestCompose.get_features('sys1test1')
            sys1test2 = TestCompose.get_features('sys1test2')
            sys2test1 = TestCompose.get_features('sys2test1')
            sys2test3 = TestCompose.get_features('sys2test3')

            with open(os.path.join(res, 'backend:system1:tests--test1'), mode='w', encoding='utf-8') as f:
                json.dump(sys1test1, f)
            with open(os.path.join(res, 'backend:system1:tests--test2:variant1'), mode='w', encoding='utf-8') as f:
                json.dump(sys1test2, f)
            with open(os.path.join(res, 'backend:system2:tests--test1'), mode='w', encoding='utf-8') as f:
                json.dump(sys2test1, f)
            with open(os.path.join(res, 'backend:system2:tests--test3'), mode='w', encoding='utf-8') as f:
                json.dump(sys2test3, f)
            out = os.path.join(tmpdir, 'out')
            os.mkdir(out)
            failed=StringIO('backend:system1:tests/test1 backend:system1:tests/test2:variant1 backend:system2:tests/test3')
            parse_args_mock.return_value = argparse.Namespace(
                    dir=res,
                    output=out,
                    failed_tests=failed,
                    run_attempt=1,
                    replace_old_runs=False,
                    env_variables='',
                    scenarios=''
                )
            featcomposer.main()

            def check_test_equal(expected, actual, succeeded):
                assert succeeded == actual['success']
                for k in expected.keys():
                    assert k in actual
                    assert actual[k] == expected[k]

            with open(os.path.join(out, 'backend:system1_1.json'), mode='r', encoding='utf-8') as f:
                sys1 = json.load(f)
                for test in sys1['tests']:
                    assert 'test1' in test['task_name'] or 'test2' in test['task_name']
                    if test['task_name'] == 'test1':
                        check_test_equal(sys1test1, test, False)
                    if test['task_name'] == 'test2':
                        check_test_equal(sys1test2, test, False)
            with open(os.path.join(out, 'backend:system2_1.json'), mode='r', encoding='utf-8') as f:
                sys1 = json.load(f)
                for test in sys1['tests']:
                    assert 'test1' in test['task_name'] or 'test3' in test['task_name']
                    if test['task_name'] == 'test1':
                        check_test_equal(sys2test1, test, True)
                    if test['task_name'] == 'test3':
                        check_test_equal(sys2test3, test, False)


class TestReplace(unittest.TestCase):

    def test_replace(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            original = os.path.join(tmpdir, 'my:system.version_1')
            rerun = os.path.join(tmpdir, 'my:system.version_2.json')
            run_once = os.path.join(tmpdir, 'my:other-system.version_1.json')
            original_json = {'system.version': 'my:system.version', 'tests': [{'task_name': 'task1', 'suite': 'my/suite', 'variant': '', 'success': False, 'cmds': [{'cmd': 'original run'}]},
                                                              {'task_name': 'task2', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'original run'}]}]}
            rerun_json = {'system.version': 'my:system.version', 'tests': [
                {'task_name': 'task1', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'rerun 1'}, {'cmd': 'another'}]}]}
            run_once_json = {'system.version': 'my:other-system.version', 'tests': [
                {'task_name': 'task', 'suite': 'my/suite', 'variant': 'v1', 'success': True}]}
            with open(original, 'w') as f:
                json.dump(original_json, f)
            with open(rerun, 'w') as f:
                json.dump(rerun_json, f)
            with open(run_once, 'w') as f:
                json.dump(run_once_json, f)
            output_dir = 'replaced'
            os.makedirs(os.path.join(tmpdir, output_dir), exist_ok=True)
            featcomposer.replace_old_runs(
                tmpdir, os.path.join(tmpdir, output_dir))
            self.assertEqual(
                2, len(os.listdir(os.path.join(tmpdir, output_dir))))
            with open(os.path.join(tmpdir, output_dir, 'my:system.version.json'), 'r') as f:
                actual = json.load(f)
                assert len(actual) == 2
                assert 'system.version' in actual and actual['system.version'] == 'my:system.version'
                assert 'tests' in actual and len(actual['tests']) == 2
                assert {'task_name': 'task1', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'rerun 1'}, {'cmd': 'another'}]} in actual['tests']
                assert {'task_name': 'task2', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'original run'}]} in actual['tests']
            with open(os.path.join(tmpdir, output_dir, 'my:other-system.version.json'), 'r') as f:
                actual = json.load(f)
                assert len(actual) == 2
                assert 'system.version' in actual and actual['system.version'] == run_once_json['system.version']
                assert 'tests' in actual and actual['tests'] == run_once_json['tests']


if __name__ == '__main__':
    unittest.main()
