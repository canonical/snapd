import json
import os
import tempfile
import unittest

import sys
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import runtimeadder


def _make_results_json(items: list) -> dict:
    return {'items': items}


def _make_task_item(backend: str, system: str, name: str, variant: str,
                    start: str, end: str, verb: str = 'executing') -> dict:
    return {
        'level': 'task',
        'verb': verb,
        'backend': backend,
        'system': system,
        'name': name,
        'variant': variant,
        'start': start,
        'end': end,
        'success': True,
    }


class TestParseAttemptFromDirname(unittest.TestCase):

    def test_valid_name(self):
        self.assertEqual(1, runtimeadder._parse_attempt_from_dirname(
            'spread-results-26594493301-1-ubuntu-focal'))

    def test_valid_name_higher_attempt(self):
        self.assertEqual(3, runtimeadder._parse_attempt_from_dirname(
            'spread-results-26594493301-3-ubuntu-core-24'))

    def test_invalid_name(self):
        self.assertIsNone(runtimeadder._parse_attempt_from_dirname(
            'spread-artifacts-26594493301-1-ubuntu-focal'))

    def test_no_match(self):
        self.assertIsNone(runtimeadder._parse_attempt_from_dirname('something-else'))


class TestParseRuntimeSeconds(unittest.TestCase):

    def test_basic(self):
        runtime = runtimeadder._parse_runtime_seconds(
            '2026-01-01T00:00:00Z', '2026-01-01T00:01:30Z')
        self.assertAlmostEqual(90.0, runtime)

    def test_sub_second(self):
        runtime = runtimeadder._parse_runtime_seconds(
            '2026-01-01T00:00:00.500Z', '2026-01-01T00:00:01.000Z')
        self.assertAlmostEqual(0.5, runtime, places=2)


class TestBuildRuntimeLookup(unittest.TestCase):

    def test_single_attempt(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            artifact_dir = os.path.join(tmpdir, 'spread-results-12345-1-focal')
            os.makedirs(artifact_dir)
            items = [
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/install', '',
                                '2026-01-01T00:00:00Z', '2026-01-01T00:00:30Z', 'preparing'),
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/install', '',
                                '2026-01-01T00:00:30Z', '2026-01-01T00:01:00Z', 'executing'),
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/install', '',
                                '2026-01-01T00:01:00Z', '2026-01-01T00:01:10Z', 'restoring'),
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/remove', 'v1',
                                '2026-01-01T00:01:10Z', '2026-01-01T00:01:20Z', 'preparing'),
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/remove', 'v1',
                                '2026-01-01T00:01:20Z', '2026-01-01T00:01:30Z', 'executing'),
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/remove', 'v1',
                                '2026-01-01T00:01:30Z', '2026-01-01T00:01:45Z', 'restoring'),
                # Non-target verbs should be ignored.
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/install', '',
                                '2026-01-01T00:00:00Z', '2026-01-01T00:10:00Z', 'checking'),
            ]
            with open(os.path.join(artifact_dir, 'results.json'), 'w') as f:
                json.dump(_make_results_json(items), f)

            lookup = runtimeadder.build_runtime_lookup(tmpdir)

            self.assertAlmostEqual(70.0, lookup[('google:ubuntu-24.04-64', 'tests/smoke/install', '')])
            self.assertAlmostEqual(35.0, lookup[('google:ubuntu-24.04-64', 'tests/smoke/remove', 'v1')])
            self.assertEqual(2, len(lookup))

    def test_latest_attempt_wins(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            for attempt, phase_end in [
                (1, ('00:00:30Z', '00:01:30Z', '00:02:00Z')),
                (2, ('00:01:00Z', '00:02:30Z', '00:03:00Z')),
            ]:
                artifact_dir = os.path.join(
                    tmpdir, f'spread-results-12345-{attempt}-focal')
                os.makedirs(artifact_dir)
                items = [
                    _make_task_item('google', 'ubuntu-24.04-64',
                                    'tests/smoke/install', '',
                                    '2026-01-01T00:00:00Z',
                                    f'2026-01-01T{phase_end[0]}', 'preparing'),
                    _make_task_item('google', 'ubuntu-24.04-64',
                                    'tests/smoke/install', '',
                                    f'2026-01-01T{phase_end[0]}',
                                    f'2026-01-01T{phase_end[1]}', 'executing'),
                    _make_task_item('google', 'ubuntu-24.04-64',
                                    'tests/smoke/install', '',
                                    f'2026-01-01T{phase_end[1]}',
                                    f'2026-01-01T{phase_end[2]}', 'restoring'),
                ]
                with open(os.path.join(artifact_dir, 'results.json'), 'w') as f:
                    json.dump(_make_results_json(items), f)

            lookup = runtimeadder.build_runtime_lookup(tmpdir)
            # Attempt 1 total = 120s, attempt 2 total = 180s.
            self.assertAlmostEqual(180.0, lookup[('google:ubuntu-24.04-64', 'tests/smoke/install', '')])

    def test_ignores_non_matching_dirs(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            # a dir that doesn't match the naming pattern
            other_dir = os.path.join(tmpdir, 'spread-artifacts-12345-1-focal')
            os.makedirs(other_dir)
            items = [_make_task_item('google', 'ubuntu-24.04-64',
                                     'tests/smoke/install', '',
                                     '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z')]
            with open(os.path.join(other_dir, 'results.json'), 'w') as f:
                json.dump(_make_results_json(items), f)

            lookup = runtimeadder.build_runtime_lookup(tmpdir)
            self.assertEqual(0, len(lookup))

    def test_empty_dir(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            self.assertEqual({}, runtimeadder.build_runtime_lookup(tmpdir))

    def test_partial_phases_are_summed(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            artifact_dir = os.path.join(tmpdir, 'spread-results-12345-1-focal')
            os.makedirs(artifact_dir)
            items = [
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/install', '',
                                '2026-01-01T00:00:00Z', '2026-01-01T00:00:20Z', 'preparing'),
                _make_task_item('google', 'ubuntu-24.04-64', 'tests/smoke/install', '',
                                '2026-01-01T00:00:20Z', '2026-01-01T00:01:00Z', 'executing'),
            ]
            with open(os.path.join(artifact_dir, 'results.json'), 'w') as f:
                json.dump(_make_results_json(items), f)

            lookup = runtimeadder.build_runtime_lookup(tmpdir)

            self.assertAlmostEqual(60.0, lookup[('google:ubuntu-24.04-64', 'tests/smoke/install', '')])
            self.assertEqual(1, len(lookup))


class TestAddRuntimeToFeatures(unittest.TestCase):

    def _make_system_features(self, system: str, tests: list) -> dict:
        return {
            'schema_version': '0.0.0',
            'system': system,
            'scenarios': [],
            'env_variables': [],
            'tests': tests,
        }

    def test_adds_runtime(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            system = 'google:ubuntu-24.04-64'
            features = self._make_system_features(system, [
                {'suite': 'tests/smoke', 'task_name': 'install', 'variant': '',
                 'success': True, 'cmds': []},
                {'suite': 'tests/smoke', 'task_name': 'remove', 'variant': 'v1',
                 'success': True, 'cmds': []},
            ])
            with open(os.path.join(tmpdir, 'google:ubuntu-24.04-64.json'), 'w') as f:
                json.dump(features, f)

            lookup = {
                ('google:ubuntu-24.04-64', 'tests/smoke/install', ''): 60.0,
                ('google:ubuntu-24.04-64', 'tests/smoke/remove', 'v1'): 30.0,
            }
            runtimeadder.add_runtime_to_features(tmpdir, lookup)

            with open(os.path.join(tmpdir, 'google:ubuntu-24.04-64.json')) as f:
                result = json.load(f)

            tests = {t['task_name']: t for t in result['tests']}
            self.assertAlmostEqual(60.0, tests['install']['runtime'])
            self.assertAlmostEqual(30.0, tests['remove']['runtime'])

    def test_runtime_null_when_not_found(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            system = 'google:ubuntu-24.04-64'
            features = self._make_system_features(system, [
                {'suite': 'tests/smoke', 'task_name': 'missing', 'variant': '',
                 'success': True, 'cmds': []},
            ])
            with open(os.path.join(tmpdir, 'google:ubuntu-24.04-64.json'), 'w') as f:
                json.dump(features, f)

            runtimeadder.add_runtime_to_features(tmpdir, {})

            with open(os.path.join(tmpdir, 'google:ubuntu-24.04-64.json')) as f:
                result = json.load(f)
            self.assertIsNone(result['tests'][0]['runtime'])

    def test_skips_non_json_files(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            with open(os.path.join(tmpdir, 'not-a-feature.txt'), 'w') as f:
                f.write('hello')
            # Should not raise
            runtimeadder.add_runtime_to_features(tmpdir, {})


if __name__ == '__main__':
    unittest.main()
