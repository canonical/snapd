#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from __future__ import print_function

# XXX copied from https://github.com/kyrofa/cla-check
# (and then heavily modified)
import os
import re
import sys
import argparse
from subprocess import check_call, check_output


try:
    from launchpadlib.launchpad import Launchpad
except ImportError:
    sys.exit(
        "Install launchpadlib: sudo apt install python-launchpadlib python3-launchpadlib"
    )

shortlog_email_rx = re.compile(r"^\s*\d+\s+.*<(\S+)>$", re.M)


is_travis = os.getenv("TRAVIS", "") == "true"
is_github_actions = os.getenv("GITHUB_ACTIONS", "") == "true"


def get_emails_for_range(r):
    output = check_output(["git", "shortlog", "-se", r]).decode("utf-8")
    return set(m.group(1) for m in shortlog_email_rx.finditer(output))


if sys.stdout.isatty() or is_travis or is_github_actions:
    green = "\033[32;1m"
    red = "\033[31;1m"
    yellow = "\033[33;1m"
    reset = "\033[0m"
    clear = "\033[0K"
else:
    green = ""
    red = ""
    yellow = ""
    reset = ""
    clear = ""

if is_travis:
    fold_start = "travis_fold:start:{{tag}}\r{}{}{{message}}{}".format(
        clear, yellow, reset
    )
    fold_end = "travis_fold:end:{{tag}}\r{}".format(clear)
elif is_github_actions:
    fold_start = "::group::{message}"
    fold_end = "::endgroup::"
else:
    fold_start = "{}{{message}}{}".format(yellow, reset)
    fold_end = ""


def static_email_check(email, master_emails, width):
    if email in master_emails:
        print("{}✓{} {:<{}} already on master".format(green, reset, email, width))
        return True
    if email.endswith("@canonical.com"):
        print("{}✓{} {:<{}} @canonical.com account".format(green, reset, email, width))
        return True
    if email.endswith("@mozilla.com"):
        print(
            "{}✓{} {:<{}} @mozilla.com account (mozilla corp has signed the corp CLA)".format(
                green, reset, email, width
            )
        )
        return True
    if email.endswith("@users.noreply.github.com"):
        print(
            "{}‽{} {:<{}} privacy-enabled github web edit email address".format(
                yellow, reset, email, width
            )
        )
        return False
    return False


def lp_email_check(email, lp, cla_folks, width):
    contributor = lp.people.getByEmail(email=email)
    if not contributor:
        print("{}🛇{} {:<{}} has no Launchpad account".format(red, reset, email, width))
        return False

    if contributor in cla_folks:
        print(
            "{}✓{} {:<{}} ({}) has signed the CLA".format(
                green, reset, email, width, contributor
            )
        )
        return True
    else:
        print(
            "{}🛇{} {:<{}} ({}) has NOT signed the CLA".format(
                red, reset, email, width, contributor
            )
        )
        return False


def print_checkout_info(travis_commit_range):
    # This is just to have information in case things go wrong
    print(fold_start.format(tag="checkout_info", message="Debug information"))
    print("Commit range:", travis_commit_range)
    print("Remotes:")
    sys.stdout.flush()
    check_call(["git", "remote", "-v"])
    print("Branches:")
    sys.stdout.flush()
    check_call(["git", "branch", "-v"])
    sys.stdout.flush()
    print(fold_end.format(tag="checkout_info"))


def main():
    parser = argparse.ArgumentParser(description="")
    parser.add_argument(
        "commit_range", help="Commit range in format <upstream-head>..<fork-head>"
    )
    opts = parser.parse_args()
    master, _ = opts.commit_range.split("..")
    print_checkout_info(opts.commit_range)
    emails = get_emails_for_range(opts.commit_range)
    if len(emails) == 0:
        sys.exit("No emails found in in the given commit range.")

    master_emails = get_emails_for_range(master)

    width = max(map(len, emails))
    lp = None
    failed = False
    print("Need to check {} emails:".format(len(emails)))
    for email in emails:
        if static_email_check(email, master_emails, width):
            continue
        # in the normal case this isn't reached
        if lp is None:
            print("Logging into Launchpad...")
            lp = Launchpad.login_anonymously("check CLA", "production")
            cla_folks = lp.people["contributor-agreement-canonical"].participants
        if not lp_email_check(email, lp, cla_folks, width):
            failed = True

    if failed:
        sys.exit(1)


if __name__ == "__main__":
    main()
