#!/usr/bin/env python3

import argparse
import json
import os
import shutil


def _parse_file_name(file_name: str) -> tuple[str, str, str, str]:
    '''
    Given a file name in the format with inverted slashes <backend>:<system>:suite\\path\\test:variant,
    it returns the original name, the suite name, the test name and the variant name.
    So in the example, it returns:
    - original_name = <backend>:<system>:suite/path/test:variant
    - suite_name = suite/path
    - test_name = test
    - variant_name = variant

    :param file_name: The file name to parse
    :returns: A tuple with the original name, the suite name, the test name and the variant name. If variant is not present, it returns None.
    '''
    original_name = file_name.replace('\\', '/')
    task = ":".join(original_name.split(':')[2:])
    suite_name = "/".join(task.split('/')[:-1])
    test_name = task.split('/')[-1]
    variant_name = None
    if test_name.count(':') == 1:
        variant_name = test_name.split(':')[1]
        test_name = test_name.split(':')[0]
    return original_name, suite_name, test_name, variant_name


def _compose_test(dir: str, file: str, failed_tests: str) -> dict:
    '''
    Creates a dictionary with the features of a test and test information.
    The features are read from the file and the test information is extracted from the file name.

    :param dir: The directory where the file is located
    :param file: The file name
    :param failed_tests: A list of failed tests
    :returns: A dictionary with test information and features
    '''
    with open(os.path.join(dir, file), 'r') as f:
        original, suite_name, test_name, variant_name = _parse_file_name(file)
        features = {}
        features['suite'] = suite_name
        features['task-name'] = test_name
        features['variant'] = variant_name
        features['success'] = original not in failed_tests
        features.update(json.loads(f.read()))
        return features


def _compose_env_variables(env_variables: str) -> list[dict]:
    '''
    Given environment variables in the form of a comma-separated list of key=value,
    it creates a list of dictionaries of [{"name": <env1-name>, "value": <env1-value>}...]

    :param env_variables: a comma-seprated list of key=value environment variables
    :returns: A list of dictionaries
    '''
    composed = []
    for env in env_variables.split(',') if env_variables else []:
        name, value = env.split('=')
        composed.append({"name": name, "value": value})
    return composed


def compose_system(dir: str, system: str, failed_tests: str = "", env_variables: str = None, scenarios: str = None) -> dict:
    '''
    Given a containing directory, a system-identifying string, and other information
    about failed tests, environment variables, and scenarios, it creates a dictionary 
    containing the feature information found in the files contained in the directory 
    for that system.

    :param dir: Directory that contains feature-tagging files
    :param system: Identifying string to select only files with that string
    :param failed_tests: String containing the names of failing tests
    :param env_variables: Comma-separated string of key=value environment variables
    :param scenarios: Comma-separated string of scenario names
    :returns: Dictionary containing all tests and tests information for the system
    '''
    files = [file for file in os.listdir(
        dir) if system in file and file.count(':') >= 2]
    system_dict = {
        'schema-version': '0.0.0',
        'system': files[0].split(':')[1] if len(files) > 0 else "",
        'scenarios': scenarios.split(',') if scenarios else [],
        'env-variables': _compose_env_variables(env_variables),
        'tests': [_compose_test(dir, file, failed_tests) for file in files],
    }
    return system_dict


def get_system_list(dir: str) -> set:
    '''
    Constructs a list of all systems from the filenames in the specified directory

    :param dir: Directory containing feature-tagging information for tests
    :returns: Set of identifying strings for systems
    '''
    files = [f for f in os.listdir(
        dir) if os.path.isfile(os.path.join(dir, f))]
    systems = [":".join(file.split(':')[:2])
               for file in files if file.count(':') >= 2]
    return set(systems)


def _replace_tests(old_json_file, new_json_file):
    '''
    The new_json_file contains a subset of the tests found in the old_json_file.
    This function leaves not-rerun tests untouched, while replacing old test
    runs with their rerun counterparts found in new_json_file. The resulting
    json in output therefore contains a mix of tests that were not rerun and
    the latest version of tests that were rerun.

    :param old_json_file: file path of first run of composed features
    :param new_json_file: file path of rerun of composed features
    :returns: dictionary that contains the first run data with rerun tests 
    replaced by the rerun data from the new_json_file
    '''
    with open(old_json_file, 'r') as f:
        old_json = json.load(f)
    with open(new_json_file, 'r') as f:
        new_json = json.load(f)
    for test in new_json['tests']:
        for old_test in old_json['tests']:
            if old_test['task-name'] == test['task-name'] and old_test['suite'] == test['suite'] and old_test['variant'] == test['variant']:
                old_test.clear()
                for key, value in test.items():
                    old_test[key] = value
                break
    return old_json


def replace_old_runs(dir, output_dir):
    '''
    Given the directory in input (dir) that contains a set of files of original
    run data together with rerun data, this populates the specified output_dir
    with a consolidated set of composed features, one per system. An original
    composed features file is a file that ends in _1.json. A rerun composed
    features file is a file that ends in _<num>.json where <num> is greater 
    than 1. The numbering is automatically generated when the compose features
    script was called with the --run-attempt


    :param dir: directory containing composed feature files with varying run 
    attempt numbers
    :param output_dir: directory where to write the consolidated composed features
    '''
    os.makedirs(output_dir)
    filenames_no_ext = [os.path.splitext(f)[0] for f in os.listdir(
        dir) if os.path.isfile(os.path.join(dir, f))]
    reruns_no_ext = [
        file for file in filenames_no_ext if not file.endswith('_1')]
    originals_no_ext = [file for file in filenames_no_ext if file.endswith(
        '_1') and any(rerun for rerun in reruns_no_ext if rerun.startswith(file[:-2]))]
    reruns_no_ext.sort(key=lambda x: int(x.split('_')[-1]))
    for rerun in reruns_no_ext:
        beginning = '_'.join(rerun.split('_')[:-1])
        original = list(
            filter(lambda x: x.startswith(beginning), originals_no_ext))
        if len(original) != 1:
            raise RuntimeError(
                "The rerun %s does not have a corresponding original run" % rerun)
        tests = _replace_tests(os.path.join(
            dir, original[0] + ".json"), os.path.join(dir, rerun + ".json"))
        with open(os.path.join(output_dir, beginning + ".json"), 'w') as f:
            f.write(json.dumps(tests))
    for file in filenames_no_ext:
        if file not in originals_no_ext:
            shutil.copyfile(os.path.join(dir, file + ".json"),
                            os.path.join(output_dir, '_'.join(file.split('_')[:-1]) + '.json'))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description="""
                                     Can be run in two modes: composed feature generation or composed feature consolidation

                                     Composed feature generation mode

                                     Given a directory containing files with outputs of journal-analzyer.py with filenames
                                     of format <backend>:<system>:suite\\path\\<test>:<variant>, it will construct a json
                                     file for each <backend>:<system> with feature-tagging information, accompanied with
                                     additional test information.

                                     Composed feature consolidation mode

                                     Given a directory containing files of pre-composed feature information with filenames like
                                     <system>_<run-attempt>.json, it writes the consolidated feature information in a new
                                     directory (specified with the --output flag) where the latest rerun data replaces the old.
                                     So if a file contains one test that was later rerun, the new consolidated file will contain
                                     unaltered content from the original run except for the one test rerun that will replace
                                     the old.
                                     """)
    parser.add_argument('-d', '--dir', type=str, required=True,
                        help='Path to the folder containing json files')
    parser.add_argument('-o', '--output', type=str,
                        help='Output directory', required=True)
    parser.add_argument('-s', '--scenarios', type=str,
                        help='Comma-separated list of scenarios', default="")
    parser.add_argument('-e', '--env-variables', type=str,
                        help='Comma-separated list of environment variables as key=value', default="")
    parser.add_argument('-f', '--failed-tests', type=str,
                        help='List of failed tests', default="")
    parser.add_argument('--run-attempt', type=int, help="""Run attempt number of the json files contained in the folder [1,). 
                        Only needed when rerunning spread for failed tests. When specified, will append the run attempt 
                        number on the filename, which will then be used when running this script with the --replace-old-runs
                        flag to determine replacement order""")
    parser.add_argument('-r', '--replace-old-runs', action="store_true",
                        help='When set, will process pre-composed runs and consolidate them into the output dir')
    args = parser.parse_args()

    if args.replace_old_runs:
        replace_old_runs(args.dir, args.output)
        exit(0)

    attempt = ""
    if args.run_attempt:
        if args.run_attempt == 0:
            raise RuntimeError(
                "The first run attempt must be 1. 0 is not allowed")
        attempt = "_%s" % args.run_attempt
    os.makedirs(args.output, exist_ok=True)
    systems = get_system_list(args.dir)
    for system in systems:
        composed = compose_system(dir=args.dir, system=system,
                                  failed_tests=args.failed_tests, env_variables=args.env_variables)
        system = "_".join(system.split(':'))
        with open(os.path.join(args.output, system + attempt + '.json'), 'w') as f:
            f.write(json.dumps(composed))
