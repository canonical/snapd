#!/usr/bin/python
#
# see http://daringfireball.net/projects/markdown/syntax
# for the "canonical" reference

import sys


def lint_li(fname, text):
    """Ensure that the list-items are multiplies of 4"""
    for i, line in enumerate(text.splitlines()):
        if line.lstrip().startswith("*") and line.index("*") % 4 != 0:
            print("%s: line %i list has non-4 spaces indent" % (fname, i))


def lint(md_files):
    """lint all md files"""
    for md in md_files:
        with open(md) as f:
            buf = f.read()
            for fname, func in globals().items():
                if fname.startswith("lint_"):
                    func(md, buf)
        

if __name__ == "__main__":
    lint(sys.argv)
