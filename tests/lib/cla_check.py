#!/usr/bin/env python
# -*- coding: utf-8 -*-
from __future__ import print_function

# XXX copied from https://github.com/kyrofa/cla-check
# (and then heavily modified)
import collections
import os
import re
import sys
from subprocess import check_call, check_output


try:
    from launchpadlib.launchpad import Launchpad
except ImportError:
    sys.exit("Install launchpadlib: sudo apt install python-launchpadlib")

shortlog_email_rx = re.compile("^\s*\d+\s+.*<(\S+)>$", re.M)

def get_emails_for_range(r):
    output = check_output(["git", "shortlog", "-se", r])
    return set(m.group(1) for m in shortlog_email_rx.finditer(output))

if sys.stdout.isatty():
    green="\033[32;1m"
    red="\033[31;1m"
    yellow="\033[33;1m"
    reset="\033[0m"
    clear="\033[0K"
else:
    green=""
    red=""
    yellow=""
    reset=""
    clear=""

def main():
    travis_commit_range = os.getenv("TRAVIS_COMMIT_RANGE", "")

    # This is just to have information in case things go wrong
    if clear:
        print("travis_fold:start:checkout_info\r"+clear, end="")
    print("{}Debug information{}".format(yellow, reset))
    print("Commit range:", travis_commit_range)
    print("Remotes:")
    sys.stdout.flush()
    check_call(["git", "remote", "-v"])
    print("Branches:")
    sys.stdout.flush()
    check_call(["git", "branch", "-v"])
    sys.stdout.flush()
    if clear:
        print("travis_fold:end:checkout_info\r"+clear)

    if travis_commit_range == "":
        sys.exit("No TRAVIS_COMMIT_RANGE set.")

    emails = get_emails_for_range(travis_commit_range)
    if len(emails) == 0:
        sys.exit("No emails found in in the given commit range.")

    master_emails = get_emails_for_range("master")

    width = max(map(len, emails))
    lp = None
    failed = False
    print("Need to check {} emails:".format(len(emails)))
    for email in emails:
        if email in master_emails:
            print("{}✓{} {:<{}} already on master".format(green, reset, email, width))
            continue
        if email.endswith("@canonical.com"):
            print("{}✓{} {:<{}} @canonical.com account".format(green, reset, email, width))
            continue
        if email.endswith("@users.noreply.github.com"):
            print("{}‽{} {:<{}} privacy-enabled github web edit email address".format(yellow, reset, email, width))
            continue
        if lp is None:
            print("Logging into Launchpad...")
            lp = Launchpad.login_anonymously("check CLA", "production")
            cla_folks = lp.people["contributor-agreement-canonical"].participants
        contributor = lp.people.getByEmail(email=email)
        if not contributor:
            failed = True
            print("{}🛇{} {:<{}} has no Launchpad account".format(red, reset, email, width))
            continue

        if contributor in cla_folks:
            print("{}✓{} {:<{}} ({}) has signed the CLA".format(green, reset, email, width, contributor))
        else:
            failed = True
            print("{}🛇{} {:<{}} ({}) has NOT signed the CLA".format(red, reset, email, width, contributor))
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    main()
