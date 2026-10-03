#!/bin/bash

set -eu

# This check verifies that interfaces that grant access to /proc/self/mountinfo
# (directly or not) also have a prioritized override (see basePrioritizedSnippets
# in interfaces/apparmor/template.go for why this is necessary). We do this
# by looking for a "AddPrioritizedSnippet(*, apparmor.MountInfoKey, ...)" call
# for any such interfaces.

missing_override=""

for f in interfaces/builtin/*.go; do
    if [[ "$f" == *"_test.go" ]]; then
        continue
    fi

    out=$(awk '
            # if a prioritized override is present, we can stop early
            /AddPrioritizedSnippet\(.* apparmor\.MountInfoKey/ { m=""; exit }

            # We look for the following types of rules:
            #   * Explicit mountinfo rules. For example, any combination of:
            #      owner /proc/self/mountinfo r,
            #      @{PROC}/@{pid} rw,
            #      /proc/1234/mountinfo
            #   * References to the "mountInfoSnippet" variable which contains
            #      the same rules as the previous item
            #   * Broad /proc grants:
            #      owner @{PROC}/** r,
            #      /proc/** rw,
            #   * Relevant "allow" rules:
            #      allow file
            #      allow all

            /^[[:space:]]*(owner[[:space:]]+)?(\/proc|@\{PROC\})\/(self|@\{pid\}|[0-9]+)\/mountinfo[[:space:]][a-z]*r[a-z]*,/ ||
            /^[[:space:]]*(owner[[:space:]]+)?(\/proc|@\{PROC\})\/\*\*[[:space:]][a-z]*r[a-z]*,/ ||
            /^[[:space:]]*allow[[:space:]]+(file|all),[[:space:]]*$/ ||
            /mountInfoSnippet/ { m = m "      " NR ":" $0 ORS }
            END { if (m != "") printf "  - %s:\n%s", FILENAME, m }
        ' "$f")
    [ -n "$out" ] && missing_override+="${out}"$'\n'
done

if [ -n "$missing_override" ]; then
    echo "" >&2
    echo "These interfaces allow access to /proc/*/mountinfo (specifically or not)" >&2
    echo "without adding the allow rule as a prioritized snippet" >&2
    echo "(see basePrioritizedSnippets in interfaces/apparmor/template.go):" >&2
    printf "%s" "$missing_override" >&2
    exit 1
fi
