#!/usr/bin/env python3

import argparse
import datetime
import json
import os
from pymongo import MongoClient, InsertOne


def upload_documents(host, port, user, password, folder):
    with MongoClient(host=host, port=port, username=user, password=password) as client:
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
        print("inserted {} new documents".format(result.inserted_count))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        description="mongodb document uploader")
    parser.add_argument('--host', help='Host for mongodb',
                        required=True, type=str)
    parser.add_argument('--port', help='Port for mongodb',
                        required=True, type=int, choices=(range(1, 65535)))
    parser.add_argument('--user', help='mongodb user', required=True, type=str)
    parser.add_argument('--password', help='mongodb password',
                        required=True, type=str)
    parser.add_argument(
        '--folder', help='folder containing json files', required=True, type=str)
    args = parser.parse_args()

    if not os.path.isdir(args.folder):
        raise RuntimeError(
            "the indicated folder {} does not exist.".format(args.folder))

    upload_documents(args.host, args.port, args.user,
                     args.password, args.folder)
