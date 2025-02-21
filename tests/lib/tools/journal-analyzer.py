#!/usr/bin/env python3

import argparse
import json
import re
import subprocess


class All:
    regex = re.compile(r"(.*)")
    name = "all"
    parent = "all"    

    @staticmethod
    def compose_dict(re_search):
        return {All.name: re_search.group(1)}


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


def _get_timestamp(msg, log_dir):
    '''
    Searches the journal for the msg in input and returns that msg's timestamp

    :param msg: string to search for in the journal
    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :return: timestamp of the msg
    :raises: RuntimeError if the msg is not found
    :raises: ValueError if the timestamp found is not in the expected format
    '''
    boot_list = _get_boot_list(log_dir)
    for i, boot in enumerate(boot_list):
        cmd = ['journalctl', '--no-pager', '--output', 'short-iso', '-b', boot]
        if log_dir:
            cmd.extend(['--directory', log_dir])
        process = subprocess.Popen(
            cmd, stdout=subprocess.PIPE, universal_newlines=True)

        for line in process.stdout:
            if msg in line:
                timestamp = _parse_timestamp(line)
                process.terminate()
                return timestamp, boot_list[i:]

        process.terminate()
    raise RuntimeError("Error: %s not found in logs" % msg)


def _get_snapd_entries_after_timestamp(boot_list, log_dir, timestamp):
    '''
    Searches the journal for snapd entries in the given boot numbers

    :param boot_list: list of boot numbers to search for snapd entries
    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :param timestamp: (optional) timestamp from when to begin the search for snapd entries. If None, will return all snapd entries
    :return: iterator of snapd entries
    '''
    for boot in boot_list:
        cmd = ['journalctl', '-u', 'snapd', '--no-pager', '-b', boot]
        if log_dir:
            cmd.extend(['--directory', log_dir])
        if timestamp:
            cmd.extend(['--since', timestamp])

        process = subprocess.Popen(
            cmd, stdout=subprocess.PIPE, universal_newlines=True)
        for line in process.stdout:
            yield line
        process.terminate()


def get_snapd_entries(log_dir=None, beginning_tag=None):
    '''
    Returns snapd entries in the journal after the (optional) beginning_tag.

    :param log_dir: (optional) directory containing journal files. If None, will search the system journal
    :param beginning_tag: (optional) tag from where to begin searching the journal
    :return: iterator of snapd entries
    :raises: RuntimeError if the beginning_tag is provided but not found
    '''
    if beginning_tag:
        beginning, boot_list = _get_timestamp(beginning_tag, log_dir)
    else:
        beginning = None
        boot_list = _get_boot_list(log_dir)
    return _get_snapd_entries_after_timestamp(boot_list, log_dir, beginning)


def get_feature_dictionary(log_lines, feature_list):
    '''
    Extracts features from the journal entries and places them in a dictionary.

    :param log_lines: iterator of journal entries
    :param feature_list: comma-separated list of feature names to extract
    :return: dictionary of features
    :raises: ValueError if an invalid feature name is provided
    '''
    feature_dict = {}
    feature_classes = [cls for cls in globals().values()
                       if isinstance(cls, type) and
                       hasattr(cls, 'name') and
                       cls.name in feature_list.split(",")]
    if len(feature_classes) != len(feature_list.split(",")):
        raise ValueError(
            "Error: Invalid feature name in feature list %s" % feature_list)

    for line in log_lines:
        for feature_class in feature_classes:
            search = feature_class.regex.search(line)
            if search:
                feature_entry = feature_class.compose_dict(search)
                if feature_class.parent not in feature_dict:
                    feature_dict[feature_class.parent] = []
                feature_dict[feature_class.parent].append(feature_entry)
                break
    return feature_dict


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="""Given a set of features and optionally a tag from where to begin the search, 
        this script will search the journal and extract the features. Those features will be saved 
        in a dictionary and written to the indicated file in output.""")
    parser.add_argument(
        '-d', '--directory', help='Directory containing journal files', required=False)
    parser.add_argument(
        '-f', '--features', help='Features to extract from journal in a comma-separated list {all}', required=True)
    parser.add_argument('-o', '--output', help='Output file', required=True)
    parser.add_argument(
        '-t', '--tag', help='Tag from where to begin searching the journal', required=False)
    args = parser.parse_args()

    snapd_journal = get_snapd_entries(args.directory, args.tag)
    feature_dictionary = get_feature_dictionary(snapd_journal, args.features)
    json.dump(feature_dictionary, open(args.output, "w"), indent=4)
