
import os
import sys
# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import compose_features
from feature_dict import *
import json
import tempfile
import unittest


class TestCompose(unittest.TestCase):
    @staticmethod
    def get_features(msg):
        features = {}
        features['cmds'] = [Cmd(cmd=msg), Cmd(cmd=f'msg msg')]
        features['endpoints'] = [Endpoint(method='POST', path=msg)]
        features['interfaces'] = [Interface(name=msg)]
        features['tasks'] = [
            Task(kind=msg, snap_type='snap', last_status=Status.done)]
        features['changes'] = [Change(kind=msg, snap_type='snap')]
        return features

    @staticmethod
    def get_json(suite, task, variant, success, msg):
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
    def write_task(filepath, msg):
        with open(filepath, 'w') as f:
            json.dump(TestCompose.get_features(msg), f)

    def test_compose(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            task1variant1 = os.path.join(
                tmpdir, 'backend:system:path--to--task1:variant1.json')
            task2 = os.path.join(tmpdir, 'backend:system:path--to--task2')
            TestCompose.write_task(task1variant1, 'task1variant1')
            TestCompose.write_task(task2, 'task2')
            systems = compose_features.get_system_list(tmpdir)
            self.assertEqual(1, len(systems))
            composed = compose_features.compose_system(tmpdir, systems.pop(),
                                                       'backend:system:path/to/task1:variant1 backend:system:another/task2',
                                                       'e=1, f=2', '1, 2, 3')
            expected = SystemFeatures(schema_version='0.0.0',
                                      system='backend:system',
                                      scenarios=['1', '2', '3'],
                                      env_variables=[{'name': 'e', 'value': '1'},
                                                     {'name': 'f', 'value': '2'}],
                                      tests=[TestCompose.get_json('path/to', 'task1', 'variant1', False, 'task1variant1'),
                                             TestCompose.get_json('path/to', 'task2', '', True, 'task2')])
            self.assertDictEqual(expected, composed)


class TestReplace(unittest.TestCase):

    def test_replace(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            original = os.path.join(tmpdir, 'my:system_1')
            rerun = os.path.join(tmpdir, 'my:system_2.json')
            run_once = os.path.join(tmpdir, 'my:other-system_1.json')
            original_json = {'system': 'my:system', 'tests': [{'task_name': 'task1', 'suite': 'my/suite', 'variant': '', 'success': False, 'cmds': [{'cmd': 'original run'}]},
                                                              {'task_name': 'task2', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'original run'}]}]}
            rerun_json = {'system': 'my:system', 'tests': [
                {'task_name': 'task1', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'rerun 1'}, {'cmd': 'another'}]}]}
            run_once_json = {'system': 'my:other-system', 'tests': [
                {'task_name': 'task', 'suite': 'my/suite', 'variant': 'v1', 'success': True}]}
            with open(original, 'w') as f:
                json.dump(original_json, f)
            with open(rerun, 'w') as f:
                json.dump(rerun_json, f)
            with open(run_once, 'w') as f:
                json.dump(run_once_json, f)
            output_dir = 'replaced'
            compose_features.replace_old_runs(
                tmpdir, os.path.join(tmpdir, output_dir))
            self.assertEqual(
                2, len(os.listdir(os.path.join(tmpdir, output_dir))))
            with open(os.path.join(tmpdir, output_dir, 'my:system.json'), 'r') as f:
                actual = json.load(f)
                expected = {'system': 'my:system', 'tests': [{'task_name': 'task1', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'rerun 1'}, {'cmd': 'another'}]},
                                                             {'task_name': 'task2', 'suite': 'my/suite', 'variant': '', 'success': True, 'cmds': [{'cmd': 'original run'}]}]}
                self.assertDictEqual(expected, actual)
            with open(os.path.join(tmpdir, output_dir, 'my:other-system.json'), 'r') as f:
                actual = json.load(f)
                self.assertDictEqual(run_once_json, actual)


if __name__ == '__main__':
    unittest.main()
