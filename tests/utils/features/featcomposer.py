#!/usr/bin/env python3

import argparse
from collections import namedtuple
import json
import os
import shutil
from typing import Any

import features


SpreadTaskNames = namedtuple(
    'SpreadTaskNames', ['original', 'suite', 'task', 'variant'])


def _remove_json_extension(file_name: str) -> str:
    return os.path.splitext(file_name)[0] if file_name.endswith('.json') else file_name


def _parse_file_name(file_name: str) -> SpreadTaskNames:
    '''
    Given a file name in the format with double slashes <backend>:<system>:suite--path--task:variant
    and optionally a json extension, it returns the original name, the suite name, the task name, 
    and the variant name. So in the example, it returns:
    - original_name = <backend>:<system>:suite/path/task:variant
    - suite_name = suite/path
    - task_name = task
    - variant_name = variant

    :param file_name: The file name to parse
    :returns: A namedtuple with the original name, the suite name, the task name and the variant name. If variant is not present, it's value is an empty string.
    '''
    file_name = _remove_json_extension(file_name)
    original_name = file_name.replace('--', '/')
    task = ':'.join(original_name.split(':')[2:])
    suite_name = '/'.join(task.split('/')[:-1])
    task_name = task.split('/')[-1]
    variant_name = ''
    if task_name.count(':') == 1:
        variant_name = task_name.split(':')[1]
        task_name = task_name.split(':')[0]
    return SpreadTaskNames(original_name, suite_name, task_name, variant_name)


def _compose_test(dir: str, file: str, failed_tests: set[str]) -> features.TaskFeatures:
    '''
    Creates a dictionary with the features of a test and test information.
    The features are read from the file and the test information is extracted from the file name.

    :param dir: The directory where the file is located
    :param file: The file name
    :param failed_tests: String containing the names of failing tests
    :returns: A dictionary with test information and features
    '''
    with open(os.path.join(dir, file), 'r', encoding='utf-8') as f:
        original, suite_name, result_name, variant_name = _parse_file_name(
            file)
        task_features = features.TaskFeatures(
            suite=suite_name,
            task_name=result_name,
            variant=variant_name,
            success=original not in failed_tests
        )
        task_features.update(json.loads(f.read()))
        return task_features


def _compose_env_variables(env_variables: list[str]) -> list[features.EnvVariables]:
    '''
    Given environment variables as a list of strings key=value, it creates
    a list of dictionaries of [{"name": <env1-name>, "value": <env1-value>}...]

    :param env_variables: a list of strings with key=value environment variables
    :returns: A list of dictionaries
    '''
    composed = []
    for env in env_variables:
        name, sep, value = env.partition('=')
        if sep != '=':
            raise ValueError("Not a key=value pair {}".format(env))
        composed.append(features.EnvVariables(
            name=name.strip(), value=value.strip()))
    return composed


def compose_system(dir: str, system: str, failed_tests: set[str], env_variables: list[str], scenarios: list[str]) -> features.SystemFeatures:
    '''
    Given a containing directory, a system-identifying string, and other information
    about failed tests, environment variables, and scenarios, it creates a dictionary 
    containing the feature information found in the files contained in the directory 
    for that system.

    :param dir: Directory that contains feature-tagging files
    :param system: Identifying string to select only files with that string
    :param failed_tests: String containing the names of failing tests
    :param env_variables: List of strings with key=value environment variables
    :param scenarios: List of strings with scenario names
    :returns: Dictionary containing all tests and tests information for the system
    '''
    files = [file for file in os.listdir(
        dir) if system in file and file.count(':') >= 2]
    return features.SystemFeatures(
        schema_version='0.0.0',
        system=system,
        scenarios=[scenario.strip()
                   for scenario in scenarios] if scenarios else [],
        env_variables=_compose_env_variables(env_variables),
        tests=[_compose_test(dir, file, failed_tests) for file in files]
    )


def get_system_list(dir: str) -> set[str]:
    '''
    Constructs a list of all systems from the filenames in the specified directory

    :param dir: Directory containing feature-tagging information for tests
    :returns: Set of identifying strings for systems
    '''
    files = [f for f in os.listdir(dir)
             if os.path.isfile(os.path.join(dir, f))]
    return {':'.join(file.split(':')[:2])
            for file in files if file.count(':') >= 2}


def _replace_tests(old_json_file: str, new_json_file: str) -> features.SystemFeatures:
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
    with open(old_json_file, 'r', encoding='utf-8') as f:
        old_json = json.load(f)
    with open(new_json_file, 'r', encoding='utf-8') as f:
        new_json = json.load(f)
    for test in new_json['tests']:
        for old_test in old_json['tests']:
            if old_test['task_name'] == test['task_name'] and old_test['suite'] == test['suite'] and old_test['variant'] == test['variant']:
                old_test.clear()
                for key, value in test.items():
                    old_test[key] = value
                break
    return old_json


def _get_original_and_rerun_list(filenames: list[str]) -> tuple[list[str], list[str]]:
    '''
    Given a list of filenames, gets two lists of rerun information: 
    the first list contains the first run (of systems that were rerun) 
    while the second list contains all reruns, sorted from earliest to latest.

    Note: the list of first runs ONLY contains the first run of reruns;
    it does not contain systems that had no rerun.

    :param filenames: a list of filenames
    :returns: the list of first runs and the list of all reruns
    '''
    reruns = [file for file in filenames if not _remove_json_extension(
        file).endswith('_1')]
    originals = [file for file in filenames
                 if _remove_json_extension(file).endswith('_1') and
                 any(rerun for rerun in reruns if rerun.startswith(_remove_json_extension(file)[:-2]))]
    reruns.sort(key=lambda x: int(_remove_json_extension(x).split('_')[-1]))

    # If something went drastically wrong during testing, there might not be an original run.
    # ( e.g. we have my-system_2.json and my-system_3.json but not my-system_1.json)
    # Search for any reruns that don't have an original and remove them from the rerun list.
    # Then, if there is another rerun present, add that instance to the originals.
    no_originals = []
    has_rerun = set()
    for rerun in reruns:
        bare_filename = _get_name_without_run_number(rerun)
        if any(o for o in no_originals if o.startswith(bare_filename)):
            has_rerun.add(bare_filename)
            continue
        if not any(original for original in originals if original.startswith(bare_filename)):
            no_originals.append(rerun)
    for no_original in no_originals:
        reruns.remove(no_original)
        if _get_name_without_run_number(no_original) in has_rerun:
            originals.append(no_original)
    
    return originals, reruns


def _get_name_without_run_number(test: str) -> str:
    '''
    Given a name like <some-name>_<some-number> (optionally with extension), 
    returns <some-name>. If the name doesn't end with _<some-number>, then 
    it will return the original name without extension.
    '''
    test_split = _remove_json_extension(test).split('_')
    if test_split[-1].isdigit():
        return '_'.join(test_split[:-1])
    return _remove_json_extension(test)


def replace_old_runs(dir: str, output_dir: str) -> None:
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
    filenames = [f for f in os.listdir(
        dir) if os.path.isfile(os.path.join(dir, f))]
    originals, reruns = _get_original_and_rerun_list(filenames)
    for rerun in reruns:
        result_name = _get_name_without_run_number(rerun)
        original = list(
            filter(lambda x: x.startswith(result_name), originals))
        if len(original) != 1:
            raise RuntimeError(
                f'The rerun {rerun} does not have a corresponding original run')
        tests = _replace_tests(os.path.join(
            dir, original[0]), os.path.join(dir, rerun))
        with open(os.path.join(output_dir, result_name + '.json'), 'w', encoding='utf-8') as f:
            f.write(json.dumps(tests))

    # Search for system test results that had no reruns and
    # simply copy their result file to the output folder
    for file in filenames:
        if file not in originals and file not in reruns:
            shutil.copyfile(os.path.join(dir, file),
                            os.path.join(output_dir, _get_name_without_run_number(file) + '.json'))


def main():
    description = '''
    Can be run in two modes: composed feature generation or composed feature consolidation

    Composed feature generation mode

    Given a directory containing files with outputs of journal-analzyer.py with filenames
    of format <backend>:<system>:suite--path--<test>:<variant>, it will construct a json
    file for each <backend>:<system> with feature-tagging information, accompanied with
    additional test information.

    Composed feature consolidation mode

    Given a directory containing files of pre-composed feature information with filenames like
    <backend>:<system>_<run-attempt>.json, it writes the consolidated feature information in a
    new directory (specified with the --output flag) where the latest rerun data replaces the old.
    So if a file contains one test that was later rerun, the new consolidated file will contain
    unaltered content from the original run except for the one test rerun that will replace
    the old.
    '''
    parser = argparse.ArgumentParser(
        description=description, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('-d', '--dir', type=str, required=True,
                        help='Path to the folder containing json files')
    parser.add_argument('-o', '--output', type=str,
                        help='Output directory', required=True)
    parser.add_argument('-s', '--scenarios', type=str, nargs='*',
                        help='List of useful metadata tags to describe the testing scenario', default='')
    parser.add_argument('-e', '--env-variables', type=str, nargs='*',
                        help='List of environment variables as key=value', default='')
    parser.add_argument('-f', '--failed-tests', type=argparse.FileType('r'),
                        help='File containing the space-separated names of failed tests')
    parser.add_argument('--run-attempt', type=int, choices=range(1, 10), help='''
                        Run attempt number of the json files contained in the folder [1,10). 
                        Only needed when rerunning spread for failed tests. When specified, will append the run attempt 
                        number on the filename, which will then be used when running this script with the --replace-old-runs
                        flag to determine replacement order''')
    parser.add_argument('-r', '--replace-old-runs', action='store_true',
                        help='When set, will process pre-composed runs and consolidate them into the output dir')
    args = parser.parse_args()

    os.makedirs(args.output, exist_ok=True)

    if args.replace_old_runs:
        replace_old_runs(args.dir, args.output)
        exit(0)

    failed_tests = set()
    if args.failed_tests:
        for failed_test in args.failed_tests:
            failed_tests.update(failed_test.split())

    attempt = ''
    if args.run_attempt:
        attempt = '_%s' % args.run_attempt
    systems = get_system_list(args.dir)
    for system in systems:
        composed = compose_system(dir=args.dir, 
                                  system=system,
                                  failed_tests=failed_tests,
                                  env_variables=args.env_variables, 
                                  scenarios=args.scenarios)
        with open(os.path.join(args.output, system + attempt + '.json'), 'w', encoding='utf-8') as f:
            json.dump(composed, f)


if __name__ == '__main__':
    main()
