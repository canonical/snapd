#!/usr/bin/python3

import argparse
import re


class InvalidPRTitle(Exception):
    def __init__(self, invalid_title):
        self.invalid_title = invalid_title


def check_pr_title(pr_title: str):
    print(pr_title)
    # cover most common cases:
    # package: foo
    # package, otherpackage/subpackage: this is a title
    # tests/regression/lp-12341234: foo
    # [RFC] foo: bar
    if not re.match(r"[a-zA-Z0-9_\-\*/,. \[\](){}]+: .*", pr_title):
        raise InvalidPRTitle(pr_title)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "pr_title", metavar="PR title", help="the github title to check"
    )
    args = parser.parse_args()
    try:
        check_pr_title(args.pr_title)
    except InvalidPRTitle as e:
        print('Invalid PR title: "{}"\n'.format(e.invalid_title))
        print("Please provide a title in the following format:")
        print("module: short description")
        print("E.g.:")
        print("daemon: fix frobinator bug")
        raise SystemExit(1)


if __name__ == "__main__":
    main()
