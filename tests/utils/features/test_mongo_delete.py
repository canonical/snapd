
from datetime import datetime
import os
import sys
import unittest
# To ensure the unit test can be run from any point in the filesystem,
# add parent folder to path to permit relative imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

import mongo_delete


class FakeDeleteResult:
    def __init__(self, deleted_count):
        self.deleted_count = deleted_count


class FakeCollection:
    def __init__(self, name, docs):
        self.name = name
        self.docs = docs

    def distinct(self, key):
        return list({doc[key] for doc in self.docs})

    def count_documents(self, query):
        return len([doc for doc in self.docs if doc['timestamp'] == query['timestamp']])

    def delete_many(self, query):
        remaining = [doc for doc in self.docs if doc['timestamp'] != query['timestamp']]
        deleted = len(self.docs) - len(remaining)
        self.docs = remaining
        return FakeDeleteResult(deleted)


class FakeDb:
    def __init__(self, features, coverages):
        self.features = FakeCollection('features', features)
        self.coverages = FakeCollection('coverages', coverages)


OLD = datetime(2026, 10, 3, 9, 0, 0)
MID = datetime(2026, 10, 10, 9, 0, 0)
NEW = datetime(2026, 10, 17, 9, 0, 0)


def make_db():
    return FakeDb(
        features=[
            {'timestamp': MID, 'system': 'a'},
            {'timestamp': OLD, 'system': 'a'},
            {'timestamp': OLD, 'system': 'b'},
            {'timestamp': OLD, 'all_features': True},
            {'timestamp': NEW, 'system': 'a'},
        ],
        coverages=[
            {'timestamp': OLD, 'system': 'a'},
            {'timestamp': MID, 'system': 'a'},
            {'timestamp': NEW, 'system': 'a'},
        ])


class TestMongoDelete(unittest.TestCase):

    def test_get_sorted_timestamps(self):
        self.assertEqual(mongo_delete.get_sorted_timestamps(make_db()), [OLD, MID, NEW])

    def test_delete_timestamp(self):
        db = make_db()
        counts = mongo_delete.delete_timestamp(db, MID)
        self.assertEqual(counts, {'features': 1, 'coverages': 1})
        self.assertEqual(mongo_delete.get_sorted_timestamps(db), [OLD, NEW])
        self.assertEqual([doc['timestamp'] for doc in db.coverages.docs], [OLD, NEW])

    def test_delete_timestamp_dry_run(self):
        db = make_db()
        counts = mongo_delete.delete_timestamp(db, OLD, dry_run=True)
        self.assertEqual(counts, {'features': 3, 'coverages': 1})
        self.assertEqual(len(db.features.docs), 5)
        self.assertEqual(len(db.coverages.docs), 3)

    def test_delete_oldest(self):
        db = make_db()
        timestamp, counts = mongo_delete.delete_oldest(db)
        self.assertEqual(timestamp, OLD)
        self.assertEqual(counts, {'features': 3, 'coverages': 1})
        self.assertEqual(mongo_delete.get_sorted_timestamps(db), [MID, NEW])

    def test_delete_oldest_empty(self):
        with self.assertRaises(RuntimeError):
            mongo_delete.delete_oldest(FakeDb([], []))
