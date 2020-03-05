#!/usr/bin/env python3

import apt
import re

best_golang = None
for p in apt.Cache():
    if re.match(r"golang-([0-9.]+)$", p.name):
        if (
            best_golang is None
            or apt.apt_pkg.version_compare(
                best_golang.candidate.version, p.candidate.version
            )
            < 0
        ):
            best_golang = p

print(best_golang.name)
