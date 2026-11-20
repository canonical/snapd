#!/bin/bash

set -eu

regexp='GOPATH(?!%%:\*)(?!:)[^= ]*/'
if grep -qPr --exclude HACKING.md --exclude 'Makefile.*' --exclude '*.qcow2' --exclude-dir .git --exclude-dir vendor "$regexp"; then
    echo "Using GOPATH as if it were a single entry and not a list:" >&2
    grep -PHrn -C1 --color=auto --exclude HACKING.md --exclude 'Makefile.*' --exclude '*.qcow2' --exclude-dir .git --exclude-dir vendor "$regexp"
    echo "Use GOHOME, or {GOPATH%%:*}, instead." >&2
    exit 1
fi
