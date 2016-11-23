import os
import re
import sys
import yaml

def e(s):
    print(s, file=sys.stderr)
    sys.exit(1)

def eq(name, s1, s2):
    if s1 != s2:
        e("in %s expected %r, got %r" % (name, s2, s1))

def rx(name, s, r):
    if not re.search(r, s):
        e("in %s expected to match %s, got %r" % (name, r, s))

def ck(name, d, *a):
    ka = set()
    for k, op, *args in a:
        if k not in d:
            e("in %s expected to have a key %r" % (name, k))
        op(name+"."+k, d[k], *args)
        ka.add(k)
    kd = set(d)
    if ka < kd:
        e("in %s: extra keys: %r" % (name, kd-ka))

verNotesRx = re.compile(r"^\w\S*\s+-$")
def verRevNotesRx(s):
    return re.compile(r"^\w\S*\s+\(\d+\)\s+" + s + "$")

res = list(yaml.load_all(sys.stdin))

eq("number of entries", len(res), 5)

ck("basic", res[0],
   ("name", eq, "basic"),
   ("summary", eq, "Basic snap"),
   ("path", rx, r"^basic_[0-9.]+_all\.snap$"),
   ("version", rx, verNotesRx),
)

ck("basic-desktop", res[1],
   ("name", eq, "basic-desktop"),
   ("path", rx, "snaps/basic-desktop/$"), # note the trailing slash
   ("summary", eq, ""),
   ("version", rx, verNotesRx),
)

ck("test-snapd-tools", res[2],
   ("name", eq, "test-snapd-tools"),
   ("publisher", eq, "canonical"),
   ("summary", eq, "Tools for testing the snapd application"),
   ("tracking", eq, "stable"),
   ("installed", rx, verRevNotesRx("-")),
   ("channels", ck,
    ("stable", rx, verRevNotesRx("-")),
    ("candidate", rx, verRevNotesRx("-")),
    ("beta", rx, verRevNotesRx("-")),
    ("edge", rx, verRevNotesRx("-")),
   ),
)

ck("test-snapd-devmode", res[3],
   ("name", eq, "test-snapd-devmode"),
   ("publisher", eq, "canonical"),
   ("summary", eq, "Basic snap with devmode confinement"),
   ("tracking", eq, "beta"),
   ("installed", rx, verRevNotesRx("devmode")),
)

ck("error", res[4],
   ("path", eq, "/etc/passwd"),
   ("warning", eq, "not a valid snap"),
)
