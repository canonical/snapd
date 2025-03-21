#!/usr/bin/env python3

import argparse
from collections import defaultdict
from features import FEATURE_LIST
import json
import subprocess


def _get_boot_list(log_dir=None):
    cmd = ['journalctl', '--list-boots']
    if log_dir:
        cmd.extend(['--directory', log_dir])
    output = subprocess.check_output(cmd, universal_newlines=True).splitlines()
    output = [line.split()[0] for line in output]
    output = [line for line in output if line.strip('-').isdigit()]
    return output


def _find_tag(cmd, tag, log_dir=None):
    '''
    Given the base command to search the journal, adds the log_dir if provided and
    searches for the tag in the journal, returning the cursor of the tag if found.

    :param cmd: base command to search the journal
    :param tag: string to search for in the journal
    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :return: tag cursor
    '''
    if log_dir:
        cmd.extend(['--directory', log_dir])
    process = subprocess.Popen(
        cmd, stdout=subprocess.PIPE, universal_newlines=True)
    for line in process.stdout:
        if tag in line:
            process.terminate()
            return json.loads(line)['__CURSOR']
    process.terminate()
    

def _get_msg_cursor(msg, log_dir, cursor=None):
    '''
    Searches the journal for the msg in input and returns that msg's cursor

    :param msg: string to search for in the journal
    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :param cursor: (optional) cursor from where to begin searching the journal for the msg
    :return: cursor of the msg
    :raises: RuntimeError if the msg is not found
    '''
    if cursor:
        cmd = ['journalctl', '--no-pager', '--output', 'json', '-c', cursor]
        msg_cursor = _find_tag(cmd, msg, log_dir)
        if msg_cursor:
            return msg_cursor
    else:
        boot_list = _get_boot_list(log_dir)
        for boot in boot_list:
            cmd = ['journalctl', '--no-pager', '--output', 'json', '-b', boot]
            msg_cursor = _find_tag(cmd, msg, log_dir)
            if msg_cursor:
                return msg_cursor
    raise RuntimeError("Error: %s not found in logs" % msg)


def _get_snapd_entries_after_cursor(log_dir, cursor):
    '''
    Searches the journal for snapd entries from the given cursor

    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :param cursor: cursor from when to begin the search for snapd entries.
    :return: iterator of snapd entries
    '''
    cmd = ['journalctl', '-u', 'snapd', '--no-pager', '--output', 'json', '-c', cursor] # This will be changed to --output cat once structural logging is added
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
    msg_cursor = _get_msg_cursor(beginning_tag, log_dir, cursor)
    return _get_snapd_entries_after_cursor(log_dir, msg_cursor)


def get_feature_dictionary(log_lines, feature_list):
    '''
    Extracts features from the journal entries and places them in a dictionary.

    :param log_lines: iterator of journal entries
    :param feature_list: comma-separated list of feature names to extract
    :return: dictionary of features
    :raises: ValueError if an invalid feature name is provided
    '''
    feature_list = feature_list.split(",")

    feature_dict = defaultdict(list)
    feature_classes = [cls for cls in FEATURE_LIST
                       if cls.name in feature_list]
    if len(feature_classes) != len(feature_list):
        raise ValueError(
            "Error: Invalid feature name in feature list {}".format(feature_list))

    for line in log_lines:
        try:
            line_json = json.loads(line)
            for feature_class in feature_classes:
                feature_class.maybe_add_feature(feature_dict, line_json)
        except ValueError:
            pass
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
