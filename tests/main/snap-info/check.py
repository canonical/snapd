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
        if op == maybe:
            d[k] = d.get(k, "")
        if k not in d:
            die("in %s expected to have a key %r" % (name, k))
        op(name + "." + k, d[k], *args)
        ka.add(k)
    kd = set(d)
    if ka < kd:
        die("in %s: extra keys: %r" % (name, kd - ka))


def exists(name, d):
    pass


def maybe(name, d):
    pass


verNotesRx = re.compile(r"^\w\S*\s+-$")


def verRevNotesRx(s):
    return re.compile(r"^\w\S*\s+\(\d+\)\s+[1-9][0-9]*\w+\s+" + s + "$")


def verRelRevNotesRx(s):
    return re.compile(
        r"^\w\S*\s+\d{4}-\d{2}-\d{2}\s+\(\d+\)\s+[1-9][0-9]*\w+\s+" + s + "$"
    )


if os.environ.get("SNAPPY_USE_STAGING_STORE", "") == "1":
    snap_ids = {
        "test-snapd-tools": "02AHdOomTzby7gTaiLX3M3SGMmXDfLJp",
        "test-snapd-devmode": "FcHyKyMiQh71liP8P82SsyMXtZI5mvVj",
        "test-snapd-python-webserver": "uHjTANBWSXSiYzNOUXZNDnOSH3POSqWS",
    }
else:
    snap_ids = {
        "test-snapd-tools": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
        "test-snapd-devmode": "821MII7GAzoRnPvTEb8R51Z1s9e0XmK5",
        "test-snapd-python-webserver": "Wcs8QL2iRQMjsPYQ4qz4V1uOlElZ1ZOb",
    }

res = list(yaml.safe_load_all(sys.stdin))

equals("number of entries", len(res), 7)

check(
    "basic",
    res[0],
    ("name", equals, "basic"),
    ("summary", equals, "Basic snap"),
    ("path", matches, r"^basic_[0-9.]+_all\.snap$"),
    ("version", matches, verNotesRx),
    ("license", equals, "unset"),
    ("description", equals, "A basic buildable snap\n"),
    ("build-date", exists),
)

check(
    "basic-desktop",
    res[1],
    ("name", equals, "basic-desktop"),
    ("path", matches, "snaps/basic-desktop/$"),  # note the trailing slash
    ("summary", equals, ""),
    ("version", matches, verNotesRx),
    ("description", equals, "A basic snap with desktop apps\n"),
    ("license", equals, "GPL-3.0"),
    ("commands", exists),
)

check(
    "test-snapd-tools",
    res[2],
    ("name", equals, "test-snapd-tools"),
    ("publisher", matches, r"(Canonical✓|canonical)"),
    ("store-url", equals, "https://snapcraft.io/test-snapd-tools"),
    ("contact", equals, "snaps@canonical.com"),
    ("summary", equals, "Tools for testing the snapd application"),
    ("description", equals, "A tool to test snapd\n"),
    ("commands", exists),
    ("tracking", equals, "latest/stable"),
    ("installed", matches, verRevNotesRx("-")),
    ("refresh-date", exists),
    (
        "channels",
        check,
        ("latest/stable", matches, verRelRevNotesRx("-")),
        ("latest/candidate", equals, "↑"),
        ("latest/beta", equals, "↑"),
        ("latest/edge", matches, verRelRevNotesRx("-")),
        ("2.0/stable", exists),
        ("2.0/candidate", exists),
        ("2.0/beta", exists),
        ("2.0/edge", exists),
    ),
    ("snap-id", equals, snap_ids["test-snapd-tools"]),
    (
        "license",
        matches,
        r"(unknown|unset)",
    ),  # TODO: update once snap.yaml contains the right license
)

check(
    "test-snapd-devmode",
    res[3],
    ("name", equals, "test-snapd-devmode"),
    ("publisher", matches, r"(Canonical✓|canonical)"),
    ("store-url", equals, "https://snapcraft.io/test-snapd-devmode"),
    ("contact", equals, "snaps@canonical.com"),
    ("summary", equals, "Basic snap with devmode confinement"),
    (
        "description",
        equals,
        "A basic buildable snap that asks for devmode confinement\n",
    ),
    ("tracking", equals, "latest/beta"),
    ("installed", matches, verRevNotesRx("devmode")),
    ("refresh-date", exists),
    (
        "channels",
        check,
        ("latest/stable", equals, "–"),
        ("latest/candidate", equals, "–"),
        ("latest/beta", matches, verRelRevNotesRx("devmode")),
        ("latest/edge", matches, verRelRevNotesRx("devmode")),
    ),
    ("snap-id", equals, snap_ids["test-snapd-devmode"]),
    (
        "license",
        matches,
        r"(unknown|unset)",
    ),  # TODO: update once snap.yaml contains the right license
)

check(
    "core",
    res[4],
    ("name", equals, "core"),
    ("type", equals, "core"),  # attenti al cane
    ("publisher", exists),
    ("store-url", exists),
    ("summary", exists),
    ("description", exists),
    # tracking not there for local snaps
    ("tracking", maybe),
    ("installed", exists),
    ("refresh-date", exists),
    ("channels", exists),
    # contacts is set on classic but not on Ubuntu Core where we
    # sideload "core"
    ("contact", maybe),
    ("snap-id", maybe),
    (
        "license",
        matches,
        r"(unknown|unset)",
    ),  # TODO: update once snap.yaml contains the right license
)

check(
    "error", res[5], ("warning", equals, 'no snap found for "/etc/passwd"'),
)

# not installed snaps have "contact" information
check(
    "test-snapd-python-webserver",
    res[6],
    ("name", equals, "test-snapd-python-webserver"),
    ("publisher", matches, r"(Canonical✓|canonical)"),
    ("store-url", equals, "https://snapcraft.io/test-snapd-python-webserver"),
    ("contact", equals, "snaps@canonical.com"),
    ("summary", exists),
    ("description", exists),
    ("channels", exists),
    ("snap-id", equals, snap_ids["test-snapd-python-webserver"]),
    ("license", equals, "Other Open Source"),
)

