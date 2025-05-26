#!/usr/bin/env python3

import argparse
import datetime
import json
import os
from pymongo import MongoClient, InsertOne

HOST = 'MONGO_HOST'
PORT = 'MONGO_PORT'
USER = 'MONGO_USER'
PASSWORD = 'MONGO_PASSWORD'


def upload_documents(folder, verbose):
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

    with MongoClient(host=os.environ[HOST], port=int(os.environ[PORT]), username=os.environ[USER], password=os.environ[PASSWORD]) as client:
        db = client.snapd
        collection = db.features

        requesting = []
        for file in os.listdir(folder):
            if file.endswith(".json"):
                with open(os.path.join(folder, file), 'r', encoding='utf-8') as f:
                    j = json.load(f)
                    j['timestamp'] = datetime.datetime.now(
                        datetime.timezone.utc)
                    requesting.append(InsertOne(j))

        result = collection.bulk_write(requesting)
        if verbose:
            print("inserted {} new documents".format(result.inserted_count))


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
