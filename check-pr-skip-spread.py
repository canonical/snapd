#!/usr/bin/python3

import argparse
import urllib.request
import logging

from html.parser import HTMLParser

# PR label indicating that spread job should be skipped
LABEL_SKIP_SPREAD_JOB = "Skip spread"


class GithubLabelsParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.in_labels = False
        self.deep = 0
        self.labels = []

    def handle_starttag(self, tag, attributes):
        logging.debug(attributes)

        if not self.in_labels:
            attr_class = [attr[1] for attr in attributes if attr[0] == "class"]
            if len(attr_class) == 0:
                return
            # labels are in separate div defined like:
            # <div class=".. labels .." .. >
            elems = attr_class[0].split(" ")
            if "labels" in elems:
                self.in_labels = True
                self.deep = 1
                logging.debug("labels start")
        else:
            # nesting counter
            self.deep += 1

            # inside labels
            # label entry has
            # <a class=".." data-name="<label name>" />
            attr_data_name = [attr[1] for attr in attributes if attr[0] == "data-name"]
            if len(attr_data_name) == 0:
                return
            data_name = attr_data_name[0]
            logging.debug("found label: %s", data_name)
            self.labels.append(data_name)

    def handle_endtag(self, tag):
        if self.in_labels:
            self.deep -= 1
            if self.deep < 1:
                logging.debug("labels end")
                self.in_labels = False

    def handle_data(self, data):
        if self.in_labels:
            logging.debug("data: %s", data)


def grab_pr_labels(pr_number: int):
    # ideally we would use the github API - however we can't because:
    # a) its rate limiting and travis IPs hit the API a lot so we regularly
    #    get errors
    # b) using a API token is tricky because travis will not allow the secure
    #    vars for forks
    # so instead we just scrape the html title which is unlikely to change
    # radically
    parser = GithubLabelsParser()
    with urllib.request.urlopen(
        "https://github.com/snapcore/snapd/pull/{}".format(pr_number)
    ) as f:
        parser.feed(f.read().decode("utf-8"))
    return parser.labels


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "pr_number", metavar="PR number", help="the github PR number to check"
    )
    parser.add_argument(
        "-d", "--debug", help="enable debug logging", action="store_true"
    )
    args = parser.parse_args()

    lvl = logging.INFO
    if args.debug:
        lvl = logging.DEBUG
    logging.basicConfig(level=lvl)

    labels = grab_pr_labels(args.pr_number)
    print("labels:", labels)

    if LABEL_SKIP_SPREAD_JOB not in labels:
        raise SystemExit(1)

    print("requested to skip the spread job")


if __name__ == "__main__":
    main()
