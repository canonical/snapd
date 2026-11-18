
import os
import sys
import unittest
# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import mongo_upload


class TestMongoUpload(unittest.TestCase):

    def test_split_coverages(self):
        system_json = {'system': 'system', 'tests': [
            {'suite': 'tests/main', 'task_name': 'a', 'variant': '', 'cmds': [{'cmd': 'a'}],
             'coverages': [{'file': 'a.go', 'func': 'A'}]},
            {'suite': 'tests/main', 'task_name': 'b', 'variant': 'v1', 'cmds': [{'cmd': 'b'}]},
        ]}
        docs = mongo_upload.split_coverages(system_json, 'ts')
        self.assertEqual(docs, [
            {'timestamp': 'ts', 'system': 'system', 'suite': 'tests/main', 'task_name': 'a', 'variant': '',
             'coverages': [{'file': 'a.go', 'func': 'A'}]},
        ])
        self.assertEqual(system_json['tests'], [
            {'suite': 'tests/main', 'task_name': 'a', 'variant': '', 'cmds': [{'cmd': 'a'}]},
            {'suite': 'tests/main', 'task_name': 'b', 'variant': 'v1', 'cmds': [{'cmd': 'b'}]},
        ])
