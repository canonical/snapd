#!/usr/bin/python3

import argparse
import re
import urllib.request

from html.parser import HTMLParser


class InvalidPRTitle(Exception):
    def __init__(self, invalid_title):
        self.invalid_title = invalid_title


class GithubTitleParser(HTMLParser):
    def __init__(self):
        HTMLParser.__init__(self)
        self._cur_tag = ""
        self.title = ""

    def handle_starttag(self, tag, attributes):
        self._cur_tag = tag

    def handle_endtag(self, tag):
        self._cur_tag = ""

    def handle_data(self, data):
        if self._cur_tag == "title":
            self.title = data


def check_pr_title(project_url: str, pr_number: int):
    # ideally we would use the github API - however we can't because:
    # a) its rate limiting and travis IPs hit the API a lot so we regularly
    #    get errors
    # b) using a API token is tricky because travis will not allow the secure
    #    vars for forks
    # so instead we just scrape the html title which is unlikely to change
    # radically
    parser = GithubTitleParser()
    with urllib.request.urlopen(
        "{}/pull/{}".format(project_url, pr_number)
    ) as f:
        parser.feed(f.read().decode("utf-8"))
    # the title has the format:
    #  "Added api endpoint for downloading snaps by glower · Pull Request #6958 · snapcore/snapd · GitHub"
    # so we rsplit() once to get the title (rsplit to not get confused by
    # possible "by" words in the real title)
    title = parser.title.rsplit(" by ", maxsplit=1)[0]
    print(title)
    # cover most common cases:
    # package: foo
    # package, otherpackage/subpackage: this is a title
    # tests/regression/lp-12341234: foo
    # [RFC] foo: bar
    if not re.match(r"[a-zA-Z0-9_\-\*/,. \[\]{}]+: .*", title):
        raise InvalidPRTitle(title)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "project_url", metavar="Project url", help="the github project url to check"
    )
    parser.add_argument(
        "pr_number", metavar="PR number", help="the github PR number to check"
    )
    args = parser.parse_args()
    try:
        check_pr_title(args.project_url, args.pr_number)
    except InvalidPRTitle as e:
        print('Invalid PR title: "{}"\n'.format(e.invalid_title))
        print("Please provide a title in the following format:")
        print("module: short description")
        print("E.g.:")
        print("daemon: fix frobinator bug")
        raise SystemExit(1)


if __name__ == "__main__":
    main()
