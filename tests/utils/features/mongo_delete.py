#!/usr/bin/env python3

import argparse
import datetime

from mongo_upload import HOST, PORT, USER, PASSWORD, connect


def get_sorted_timestamps(db):
    '''
    Returns all timestamps present in the features collection, oldest first.
    '''
    return sorted(db.features.distinct('timestamp'))


def delete_timestamp(db, timestamp, dry_run=False):
    '''
    Deletes all documents with the given timestamp from the features and
    coverages collections. Returns the number of documents deleted (or that
    would be deleted, when dry_run is set) per collection.
    '''
    counts = {}
    for collection in (db.features, db.coverages):
        if dry_run:
            counts[collection.name] = collection.count_documents(
                {'timestamp': timestamp})
        else:
            counts[collection.name] = collection.delete_many(
                {'timestamp': timestamp}).deleted_count
    return counts


def delete_oldest(db, dry_run=False):
    '''
    Looks up all timestamps and deletes every document with the oldest one.
    Returns the deleted timestamp and the counts from delete_timestamp.
    '''
    timestamps = get_sorted_timestamps(db)
    if not timestamps:
        raise RuntimeError("no timestamps found in the features collection")
    oldest = timestamps[0]
    return oldest, delete_timestamp(db, oldest, dry_run)


def print_counts(timestamp, counts, dry_run):
    action = "would delete" if dry_run else "deleted"
    for name, count in counts.items():
        print("{} {} documents from {} at timestamp {}".format(
            action, count, name, timestamp.isoformat()))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        description="mongodb document remover. It assumes the following environment variables are set: {}, {}, {}, and {}".format(HOST, PORT, USER, PASSWORD))
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument(
        '--timestamp', help='delete all documents at this timestamp, in UTC, as listed by query_features.py (e.g. 2026-10-17T09:14:03.123000)',
        type=datetime.datetime.fromisoformat)
    group.add_argument(
        '--oldest', help='delete all documents at the oldest timestamp', action='store_true')
    parser.add_argument(
        '--dry-run', help='only print how many documents would be deleted', action='store_true')
    args = parser.parse_args()

    with connect() as client:
        db = client.snapd
        if args.oldest:
            timestamp, counts = delete_oldest(db, args.dry_run)
        else:
            timestamp = args.timestamp
            counts = delete_timestamp(db, timestamp, args.dry_run)
        print_counts(timestamp, counts, args.dry_run)
