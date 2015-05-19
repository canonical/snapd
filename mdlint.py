#!/usr/bin/python
#
# see http://daringfireball.net/projects/markdown/syntax
# for the "canonical" reference
#
# We support django-markdown which uses python-markdown, see:
# http://pythonhosted.org/Markdown/

import sys


def lint_li(fname, text):
    """Ensure that the list-items are multiplies of 4"""
    is_clean = True
    for i, line in enumerate(text.splitlines()):
        if line.lstrip().startswith("*") and line.index("*") % 4 != 0:
            print("%s: line %i list has non-4 spaces indent" % (fname, i))
            is_clean = False
    return is_clean


def lint(md_files):
    """lint all md files"""
    all_clean = True
    for md in md_files:
        with open(md) as f:
            buf = f.read()
            for fname, func in globals().items():
                if fname.startswith("lint_"):
                    all_clean &= func(md, buf)
    return all_clean


if __name__ == "__main__":
    if not lint(sys.argv):
        sys.exit(1)
