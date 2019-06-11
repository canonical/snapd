#!/usr/bin/python3

import argparse
import json
import os
import sys
import urllib.request

class InvalidPRTitle(Exception):
    def __init__(self, invalid_title):
        self.invalid_title = invalid_title


def check_pr_title(pr_number: int):
    with urllib.request.urlopen('https://api.github.com/repos/snapcore/snapd/pulls/{}'.format(pr_number)) as f:
        data=json.loads(f.read())
    title = data["title"]
    # TODO: be a bit smarter here - but this will catch ~95% of the bad ones
    if not ":" in title:
        raise InvalidPRTitle(title)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        'pr_number', metavar='PR number', help='the github PR number to check')
    args = parser.parse_args()
    try:
        check_pr_title(args.pr_number)
    except InvalidPRTitle as e:
        print("Invalid PR title: \"{}\"\n".format(e.invalid_title))
        print("Please provide a title in the following format:")
        print("module: short description")
        print("E.g.:")
        print("daemon: fix frobinator bug")
        sys.exit(1)


if __name__ == "__main__":
    main()
