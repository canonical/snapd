#!/usr/bin/env python3

import argparse
from collections import defaultdict
import json
from typing import TextIO


# This will be removed
class AllFeature:
    name = "all"
    parent = "all"
    
    @staticmethod
    def maybe_add_feature(feature_dict: dict, json_entry: dict, state_json: dict):
        feature_dict[AllFeature.parent].append({AllFeature.name: json_entry})
    

FEATURE_LIST = [AllFeature]


def get_feature_dictionary(log_file: TextIO, feature_list: list[str], state_json: dict):
    '''
    Extracts features from the journal entries and places them in a dictionary.

    :param log_file: iterator of journal entries
    :param feature_list: list of feature names to extract
    :param state_json: dictionary of a state.json
    :return: dictionary of features
    :raises: ValueError if an invalid feature name is provided
    :raises: RuntimeError if a line could not be parsed as json
    '''
    
    feature_dict = defaultdict(list)
    feature_classes = [cls for cls in FEATURE_LIST
                       if cls.name in feature_list]
    if len(feature_classes) != len(feature_list):
        raise ValueError(
            "Error: Invalid feature name in feature list {}".format(feature_list))

    for line in log_file:
        try:
            line_json = json.loads(line)
            for feature_class in feature_classes:
                feature_class.maybe_add_feature(feature_dict, line_json, state_json)
        except json.JSONDecodeError:
            raise RuntimeError("Could not parse line as json: {}".format(line))
    return feature_dict


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="""Given a set of features with journal entries, each in json format, and a 
        state.json, this script will search the text file and extract the features. Those 
        features will be saved in a dictionary and written to the indicated file in output.""")
    parser.add_argument('-o', '--output', help='Output file', required=True)
    parser.add_argument(
        '-f', '--features', help='Features to extract from journal {all}', nargs='+')
    parser.add_argument(
        '-j', '--journal', help='Text file containing journal entries', required=True, type=argparse.FileType('r'))
    parser.add_argument(
        '-s', '--state', help='state.json', required=True, type=argparse.FileType('r'))
    args = parser.parse_args()

    try:
        state_json = json.load(args.state)
        feature_dictionary = get_feature_dictionary(args.journal, args.features, state_json)
        json.dump(feature_dictionary, open(args.output, "w"))
    except json.JSONDecodeError:
        raise RuntimeError("The state.json is not valid json")
