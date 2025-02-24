#!/usr/bin/env python3

import argparse
import json
import os


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


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description="""
                                     Given a directory containing files with outputs of journal-analzyer.py with filenames
                                     of format <backend>:<system>:suite\\path\\<test>:<variant>, it will construct a json
                                     file for each <backend>:<system> with feature-tagging information, accompanied with
                                     additional test information.
                                     """)
    parser.add_argument('-d', '--dir', type=str,
                        help='Path to the feature-tags folder')
    parser.add_argument('-o', '--output', type=str, help='Output directory')
    parser.add_argument('-s', '--scenarios', type=str,
                        help='Comma-separated list of scenarios', default="")
    parser.add_argument('-e', '--env-variables', type=str,
                        help='Comma-separated list of environment variables as key=value', default="")
    parser.add_argument('-f', '--failed-tests', type=str,
                        help='List of failed tests', default="")
    args = parser.parse_args()
    os.makedirs(args.output, exist_ok=True)
    systems = get_system_list(args.dir)
    for system in systems:
        composed = compose_system(dir=args.dir, system=system,
                                  failed_tests=args.failed_tests, env_variables=args.env_variables)
        system = "_".join(system.split(':'))
        with open(os.path.join(args.output, system + '.json'), 'w') as f:
            f.write(json.dumps(composed))
