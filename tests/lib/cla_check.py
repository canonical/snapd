#!/usr/bin/env python

# XXX copied from https://github.com/kyrofa/cla-check
import collections
import os
import sys
from subprocess import check_call, check_output


try:
    from launchpadlib.launchpad import Launchpad
except ImportError:
    sys.exit("Install launchpadlib: sudo apt install python-launchpadlib")

Commit = collections.namedtuple("Commit", ["email", "hash"])


def get_commits_for_range(range):
    output = check_output(["git", "log", range, "--pretty=%aE|%H"])
    split_output = (i.split("|") for i in output.split("\n") if i != "")
    commits = [Commit(email=i[0], hash=i[1]) for i in split_output]
    return commits


def get_emails_from_commits(commits):
    emails = set()
    for c in commits:
        output = check_output(["git", "branch", "--contains", c.hash])
        in_master = any(["master" == b.strip() for b in output.split("\n")])
        if in_master:
            print(
                'Commit {} from {} not in "master", found in '
                "{!r}".format(c.hash, c.email, output)
            )
            emails.add(c.email)
        else:  # just for debug
            print("Skipping {}, found in:\n{}".format(c.hash, output))
    return emails


def main():
    # This is just to have information in case things go wrong
    print("Remotes:")
    sys.stdout.flush()
    check_call(["git", "remote", "-v"])
    print("Branches:")
    sys.stdout.flush()
    check_call(["git", "branch", "-v"])

    travis_commit_range = os.getenv("TRAVIS_COMMIT_RANGE", "")
    commits = get_commits_for_range(travis_commit_range)
    emails = get_emails_from_commits(commits)
    if not emails:
        print("No emails to verify")
        return

    print("Logging into Launchpad...")
    lp = Launchpad.login_anonymously("check CLA", "production")
    cla_folks = lp.people["contributor-agreement-canonical"].participants

    print("Amount of emails to check the CLA for {}".format(len(emails)))
    for email in emails:
        print("Checking the CLA for {!r}".format(email))
        if email.endswith("@canonical.com"):
            print("Skipping @canonical.com account {}".format(email))
            continue
        contributor = lp.people.getByEmail(email=email)
        if not contributor:
            sys.exit("The contributor does not have a Launchpad account.")

        print("Contributor account for {}: {}".format(email, contributor))
        if contributor in cla_folks:
            print("The contributor has signed the CLA.")
        else:
            sys.exit("The contributor {} has not signed the CLA.".format(email))


if __name__ == "__main__":
    main()
