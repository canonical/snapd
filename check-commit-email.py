#!/usr/bin/python3

# This script checks that the committer and author email addresses of commits
# are valid - this is important because we need to be able to import the git
# repo to launchpad and launchpad does not like git commits that are formatted
# badly.
# This can be run either on a merge commit in which case the commits it
# evaluates is just limited to the "other" branch (the one being merged into the
# destination) like we do in CI workflows. Or it can be run on a normal commit
# like a developer would do locally in which case this ends up checking all
# commits locally - some day that may grow to be unwieldy but for now seems ok.

import os
import subprocess
from email.utils import parseaddr


def get_commit_range():
    # For CI, the head revision is a synthesised merge commit,
    # merging the proposed branch into the destination branch.
    # So the first parent is our destination, and the second is
    # our proposal.
    lines = subprocess.check_output(
        ["git", "cat-file", "-p", "@"], text=True
    ).splitlines()
    parents = [
        line[len("parent ") :].strip() for line in lines if line.startswith("parent ")
    ]
    if len(parents) == 1:
        # not a merge commit, so return nothing to use default git log behavior
        # and check all commits
        return ""
    elif len(parents) == 2:
        # merge commit so use "foo..bar" syntax to just check the proposed
        # commits
        dest, proposed = parents
        return "{}..{}".format(dest, proposed)
    else:
        raise RuntimeError("expected two parents, but got {}".format(parents))


if __name__ == "__main__":
    if not os.path.exists('.git'):
        exit(0)
    commitrange = get_commit_range()
    args = ["git", "log", "--format=format:%h,%ce%n%h,%ae"]
    if commitrange != "":
        args.append(commitrange)
    for line in subprocess.check_output(args, text=True).split("\n"):
        parsed = line.split(",", 1)
        commithash = parsed[0]
        potentialemail = parsed[1]
        if potentialemail == "":
            continue
        name, addr = parseaddr(potentialemail)
        if addr == "":
            print(
                "Found invalid email %s for commmit %s" % (potentialemail, commithash)
            )
            exit(1)
        if not addr.isascii():
            print(
                "Found invalid non-ascii email %s for commmit %s"
                % (potentialemail, commithash)
            )
            exit(1)
