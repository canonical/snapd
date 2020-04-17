#!/usr/bin/env python
# -*- coding: utf-8 -*-
from __future__ import print_function

# XXX copied from https://github.com/kyrofa/cla-check
# (and then heavily modified)
import os
import re
import sys
from subprocess import check_call, check_output


try:
    from launchpadlib.launchpad import Launchpad
except ImportError:
    sys.exit("Install launchpadlib: sudo apt install python-launchpadlib")

shortlog_email_rx = re.compile("^\s*\d+\s+.*<(\S+)>$", re.M)


is_travis = os.getenv("TRAVIS", "") == "true"
is_github_actions = os.getenv("GITHUB_ACTIONS", "") == "true"


def get_commit_range():
    # Sanity check to ensure we are running from a pull request check job
    if is_travis:
        if os.getenv("TRAVIS_PULL_REQUEST", "false") == "false":
            raise RuntimeError("called from a non-pull request Travis job")
    elif is_github_actions:
        if os.getenv("GITHUB_EVENT_NAME", "") != "pull_request":
            raise RuntimeError("called from a non-pull request Github Actions job")
    else:
        raise RuntimeError("unknown CI system.")

    # The head revision is a synthesised merge commit, merging the
    # proposed branch into the destination branch.  So the first
    # parent is our destination, and the second is our proposal.
    lines = check_output(["git", "cat-file", "-p", "@"]).splitlines()
    parents = [line[len("parent "):].strip() for line in lines
               if line.startswith("parent ")]
    if len(parents) != 2:
        raise RuntimeError("expected two parents, but got {}".format(parents))
    dest, proposed = parents

    return dest, proposed


def get_emails_for_range(r):
    output = check_output(["git", "shortlog", "-se", r])
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
    fold_start = 'travis_fold:start:{{tag}}\r{}{}{{message}}{}'.format(
        clear, yellow, reset)
    fold_end = 'travis_fold:end:{{tag}}\r{}'.format(clear)
elif is_github_actions:
    fold_start = '::group::{message}'
    fold_end = '::endgroup::'
else:
    fold_start = '{}{{message}}{}'.format(yellow, reset)
    fold_end = ''


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
        return True
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
    try:
        master, proposed = get_commit_range()
    except Exception as exc:
        sys.exit("Could not determine commit range: {}".format(exc))
    commit_range = "{}..{}".format(master, proposed)
    print_checkout_info(commit_range)

    emails = get_emails_for_range(commit_range)
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
