#!/usr/bin/python3

import argparse
import base64
import json
import os
import re
import sys
import urllib.request

class InvalidPRTitle(Exception):
    def __init__(self, invalid_title):
        self.invalid_title = invalid_title


def check_pr_title(pr_number: int):
    req = urllib.request.Request('https://api.github.com/repos/snapcore/snapd/pulls/{}'.format(pr_number))
    api_key=os.environ.get("GITHUB_API_KEY")
    api_user=os.environ.get("GITHUB_API_USER")
    if api_key:
        # TODO: replace with a snapcore RO api key?
        credentials = ('%s:%s' % (api_user, api_key))
        encoded_credentials = base64.b64encode(credentials.encode('ascii'))
        req.add_header('Authorization', 'Basic %s' % encoded_credentials.decode("ascii"))
    with urllib.request.urlopen(req) as f:
        data=json.loads(f.read().decode("utf-8"))
    title = data["title"]
    # cover most common cases:
    # package: foo
    # package, otherpackage/subpackage: this is a title
    # tests/regression/lp-12341234: foo
    # [RFC] foo: bar
    if not re.match(r'[a-zA-Z0-9_\-/,. \[\]]+: .*', title):
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
