import os
import re
import sys
import yaml

def die(s):
    print(s, file=sys.stderr)
    sys.exit(1)

def equals(name, s1, s2):
    if s1 != s2:
        die("in %s expected %r, got %r" % (name, s2, s1))

def matches(name, s, r):
    if not re.search(r, s):
        die("in %s expected to match %s, got %r" % (name, r, s))

def check(name, d, *a):
    ka = set()
    for k, op, *args in a:
        if k not in d:
            die("in %s expected to have a key %r" % (name, k))
        op(name+"."+k, d[k], *args)
        ka.add(k)
    kd = set(d)
    if ka < kd:
        die("in %s: extra keys: %r" % (name, kd-ka))

def exists(name, d):
    pass

verNotesRx = re.compile(r"^\w\S*\s+-$")
def verRevNotesRx(s):
    return re.compile(r"^\w\S*\s+\(\d+\)\s+[1-9][0-9]*\w+\s+" + s + "$")

res = list(yaml.load_all(sys.stdin))

equals("number of entries", len(res), 6)

check("basic", res[0],
   ("name", equals, "basic"),
   ("summary", equals, "Basic snap"),
   ("path", matches, r"^basic_[0-9.]+_all\.snap$"),
   ("version", matches, verNotesRx),
)

check("basic-desktop", res[1],
   ("name", equals, "basic-desktop"),
   ("path", matches, "snaps/basic-desktop/$"), # note the trailing slash
   ("summary", equals, ""),
   ("version", matches, verNotesRx),
)

check("test-snapd-tools", res[2],
   ("name", equals, "test-snapd-tools"),
   ("publisher", equals, "canonical"),
   ("summary", equals, "Tools for testing the snapd application"),
   ("description", equals, "A tool to test snapd\n"),
   ("commands", exists),
   ("tracking", equals, "stable"),
   ("installed", matches, verRevNotesRx("-")),
   ("refreshed", exists),
   ("channels", check,
    ("stable", matches, verRevNotesRx("-")),
    ("candidate", matches, verRevNotesRx("-")),
    ("beta", matches, verRevNotesRx("-")),
    ("edge", matches, verRevNotesRx("-")),
   ),
)

check("test-snapd-devmode", res[3],
   ("name", equals, "test-snapd-devmode"),
   ("publisher", equals, "canonical"),
   ("summary", equals, "Basic snap with devmode confinement"),
   ("description", equals, "A basic buildable snap that asks for devmode confinement\n"),
   ("tracking", equals, "beta"),
   ("installed", matches, verRevNotesRx("devmode")),
   ("refreshed", exists),
   ("channels", check,
    ("beta", matches, verRevNotesRx("devmode")),
    ("edge", matches, verRevNotesRx("devmode")),
   ),
)

check("core", res[4],
      ("name", equals, "core"),
      ("type", equals, "core"), # attenti al cane
      ("publisher", exists),
      ("summary", exists),
      ("description", exists),
      ("tracking", exists),
      ("installed", exists),
      ("refreshed", exists),
      ("channels", exists),
)

check("error", res[5],
   ("argument", equals, "/etc/passwd"),
   ("warning", equals, "not a valid snap"),
)
