#!/usr/bin/env python3

import argparse
from features import FEATURE_LIST
import json
import re
import subprocess


def _get_boot_list(log_dir=None):
    cmd = ['journalctl', '--list-boots']
    if log_dir:
        cmd.extend(['--directory', log_dir])
    output = subprocess.check_output(cmd, universal_newlines=True).splitlines()
    output = [line.split()[0] for line in output]
    output = [line for line in output if line.strip('-').isdigit()]
    return output


def _parse_timestamp(line):
    '''
    Given a timestamp in the format of "YYYY-MM-DDTHH:MM:SS" in the input string, 
    returns the timestamp in the format of "YYYY-MM-DD HH:MM:SS"

    :param line: line that contains a timestamp in the format of "YYYY-MM-DDTHH:MM:SS"
    :return: timestamp in the format of "YYYY-MM-DD HH:MM:SS"
    :raises: ValueError if the timestamp is not in the expected format
    '''
    timestamp_pattern = r'(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})'
    search = re.search(timestamp_pattern, line)
    if search:
        return "%s %s" % (search.group(1), search.group(2))
    raise ValueError(
        "Error: Invalid timestamp format. Expected YYYY-MM-DDTHH:MM:SS not found in %s" % line)


def _find_tag(cmd, tag, log_dir=None):
    '''
    Given the base command to search the journal, adds the log_dir if provided and
    searches for the tag in the journal, returning the timestamp of the tag if found.

    :param cmd: base command to search the journal
    :param tag: string to search for in the journal
    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :return: timestamp of the tag
    '''
    if log_dir:
        cmd.extend(['--directory', log_dir])
    process = subprocess.Popen(
        cmd, stdout=subprocess.PIPE, universal_newlines=True)
    for line in process.stdout:
        if tag in line:
            timestamp = _parse_timestamp(line)
            process.terminate()
            return timestamp
    process.terminate()
    

def _get_timestamp(msg, log_dir, cursor=None):
    '''
    Searches the journal for the msg in input and returns that msg's timestamp

    :param msg: string to search for in the journal
    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :param cursor: (optional) cursor from where to begin searching the journal for the msg
    :return: timestamp of the msg
    :raises: RuntimeError if the msg is not found
    :raises: ValueError if the timestamp found is not in the expected format
    '''
    if cursor:
        cmd = ['journalctl', '--no-pager', '--output', 'short-iso', '-c', cursor]
        timestamp = _find_tag(cmd, msg, log_dir)
        if timestamp:
            return timestamp
    else:
        boot_list = _get_boot_list(log_dir)
        for boot in boot_list:
            cmd = ['journalctl', '--no-pager', '--output', 'short-iso', '-b', boot]
            timestamp = _find_tag(cmd, msg, log_dir)
            if timestamp:
                return timestamp
    raise RuntimeError("Error: %s not found in logs" % msg)


def _get_snapd_entries_after_timestamp(log_dir, timestamp):
    '''
    Searches the journal for snapd entries from the given timestamp

    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :param timestamp: timestamp from when to begin the search for snapd entries. If None, will return all snapd entries
    :return: iterator of snapd entries
    '''
    cmd = ['journalctl', '-u', 'snapd', '--no-pager', '--output', 'cat', '--since', timestamp]
    if log_dir:
        cmd.extend(['--directory', log_dir])

    process = subprocess.Popen(
        cmd, stdout=subprocess.PIPE, universal_newlines=True)
    for line in process.stdout:
        yield line
    process.terminate()


def get_snapd_entries(beginning_tag, log_dir=None, cursor=None):
    '''
    Returns snapd entries in the journal after the beginning_tag.

    :param beginning_tag: tag from where to begin searching the journal
    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :param cursor: (optional) cursor from where to begin searching the journal for the beginning_tag
    :return: iterator of snapd entries
    :raises: RuntimeError if the beginning_tag is not found
    '''
    beginning = _get_timestamp(beginning_tag, log_dir, cursor)
    return _get_snapd_entries_after_timestamp(log_dir, beginning)


def get_feature_dictionary(log_lines, feature_list):
    '''
    Extracts features from the journal entries and places them in a dictionary.

    :param log_lines: iterator of journal entries
    :param feature_list: comma-separated list of feature names to extract
    :return: dictionary of features
    :raises: ValueError if an invalid feature name is provided
    '''
    feature_list = feature_list.split(",")

    feature_dict = {}
    feature_classes = [cls for cls in FEATURE_LIST
                       if cls.name in feature_list]
    if len(feature_classes) != len(feature_list):
        raise ValueError(
            "Error: Invalid feature name in feature list {}".format(feature_list))

    for line in log_lines:
        try:
            line_json = json.loads(line)
            for feature_class in feature_classes:
                feature_entry = feature_class.extract_feature(line_json)
                if feature_entry:
                    if feature_class.parent not in feature_dict:
                        feature_dict[feature_class.parent] = []
                    feature_dict[feature_class.parent].append(feature_entry)
        except ValueError:
            print("Error parsing json: " + line)
    return feature_dict


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="""Given a set of features and a tag from where to begin the search, this
        script will search the journal and extract the features. Those features will be saved 
        in a dictionary and written to the indicated file in output.""")
    parser.add_argument(
        '-f', '--features', help='Features to extract from journal in a comma-separated list {all}', required=True)
    parser.add_argument('-o', '--output', help='Output file', required=True)
    parser.add_argument(
        '-t', '--tag', help='Tag from where to begin searching the journal', required=True)
    parser.add_argument(
        '-d', '--directory', help='Directory containing journal files', required=False)
    parser.add_argument(
        '-c', '--cursor', help='Cursor from where to begin searching the journal', required=False)
    args = parser.parse_args()

    snapd_journal = get_snapd_entries(args.tag, args.directory, args.cursor)
    feature_dictionary = get_feature_dictionary(snapd_journal, args.features)
    json.dump(feature_dictionary, open(args.output, "w"))
