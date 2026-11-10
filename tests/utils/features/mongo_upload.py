#!/usr/bin/env python3

import argparse
import datetime
import json
import os
from pymongo import ASCENDING, MongoClient, InsertOne

HOST = 'MONGO_HOST'
PORT = 'MONGO_PORT'
USER = 'MONGO_USER'
PASSWORD = 'MONGO_PASSWORD'

# Feature that is stored in its own collection, one document per test, because
# keeping it inline makes system documents exceed mongodb's 16MB document limit.
COVERAGES = 'coverages'


def split_coverages(system_json, timestamp):
    '''
    Removes the coverages from each test in the system json and returns them
    as a list of documents, one per test, suitable for the coverages collection.
    '''
    coverage_docs = []
    for test in system_json.get('tests', []):
        if COVERAGES not in test:
            continue
        coverage_docs.append({
            'timestamp': timestamp,
            'system': system_json['system'],
            'suite': test.get('suite'),
            'task_name': test.get('task_name'),
            'variant': test.get('variant'),
            COVERAGES: test.pop(COVERAGES),
        })
    return coverage_docs


def connect():
    '''
    Returns a MongoClient built from the MONGO_* environment variables.
    '''
    if HOST not in os.environ.keys():
        raise RuntimeError(
            "the {} environment variable must be set and contain the host data".format(HOST))
    if PORT not in os.environ.keys():
        raise RuntimeError(
            "the {} environment variable must be set and contain the port data".format(PORT))
    if USER not in os.environ.keys():
        raise RuntimeError(
            "the {} environment variable must be set and contain the username".format(USER))
    if PASSWORD not in os.environ.keys():
        raise RuntimeError(
            "the {} environment variable must be set and contain the password".format(PASSWORD))
    if not os.environ[PORT].isdigit():
        raise RuntimeError(
            "the {} environment variable must contain a valid port number".format(PORT))

    return MongoClient(host=os.environ[HOST], port=int(os.environ[PORT]), username=os.environ[USER], password=os.environ[PASSWORD])


def upload_documents(folder, verbose):
    with connect() as client:
        db = client.snapd
        collection = db.features
        coverage_collection = db.coverages
        coverage_collection.create_index(
            [('timestamp', ASCENDING), ('system', ASCENDING)])

        requesting = []
        coverage_requesting = []
        timestamp = datetime.datetime.now(datetime.timezone.utc)
        for file in os.listdir(folder):
            if file.endswith(".json"):
                with open(os.path.join(folder, file), 'r', encoding='utf-8') as f:
                    j = json.load(f)
                    j['timestamp'] = timestamp
                    if file == 'all-features.json':
                        j['all_features'] = True
                    else:
                        coverage_requesting.extend(
                            InsertOne(doc) for doc in split_coverages(j, timestamp))
                    requesting.append(InsertOne(j))

        result = collection.bulk_write(requesting)
        if verbose:
            print("inserted {} new documents".format(result.inserted_count))
        if coverage_requesting:
            result = coverage_collection.bulk_write(coverage_requesting)
            if verbose:
                print("inserted {} new coverage documents".format(result.inserted_count))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        description="mongodb document uploader. It assumes the following environment variables are set: {}, {}, {}, and {}".format(HOST, PORT, USER, PASSWORD))
    parser.add_argument(
        '--dir', help='directory containing json files', required=True, type=str)
    parser.add_argument(
        '--verbose', help='print upload statement', action='store_true')
    args = parser.parse_args()

    if not os.path.isdir(args.dir):
        raise RuntimeError(
            "the indicated directory {} does not exist.".format(args.dir))

    upload_documents(args.dir, args.verbose)
